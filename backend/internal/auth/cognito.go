package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKSet represents a set of JSON Web Keys
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// CognitoConfig holds Cognito configuration
type CognitoConfig struct {
	Region       string
	UserPoolID   string
	ClientID     string
	JWKSEndpoint string
	jwkSet       *JWKSet
	lastFetch    time.Time
}

// UserClaims represents the claims from a Cognito JWT token
type UserClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	TokenUse      string `json:"token_use"`
	Scope         string `json:"scope"`
	AuthTime      int64  `json:"auth_time"`
	Iss           string `json:"iss"`
	Exp           int64  `json:"exp"`
	Iat           int64  `json:"iat"`
	ClientID      string `json:"client_id"`
	Username      string `json:"username"`
	jwt.RegisteredClaims
}

// NewCognitoConfig creates a new Cognito configuration
func NewCognitoConfig() *CognitoConfig {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	userPoolID := os.Getenv("COGNITO_USER_POOL_ID")
	clientID := os.Getenv("COGNITO_CLIENT_ID")

	jwksEndpoint := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s/.well-known/jwks.json", region, userPoolID)

	return &CognitoConfig{
		Region:       region,
		UserPoolID:   userPoolID,
		ClientID:     clientID,
		JWKSEndpoint: jwksEndpoint,
	}
}

// fetchJWKS fetches the JWKS from Cognito
func (c *CognitoConfig) fetchJWKS() error {
	// Only fetch if we haven't fetched recently (cache for 1 hour)
	if c.jwkSet != nil && time.Since(c.lastFetch) < time.Hour {
		return nil
	}

	resp, err := http.Get(c.JWKSEndpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch JWKS: status %d", resp.StatusCode)
	}

	var jwkSet JWKSet
	if err := json.NewDecoder(resp.Body).Decode(&jwkSet); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	c.jwkSet = &jwkSet
	c.lastFetch = time.Now()
	return nil
}

// getPublicKey converts JWK to RSA public key
func (c *CognitoConfig) getPublicKey(kid string) (*rsa.PublicKey, error) {
	if err := c.fetchJWKS(); err != nil {
		return nil, err
	}

	for _, key := range c.jwkSet.Keys {
		if key.Kid == kid && key.Kty == "RSA" {
			return c.jwkToRSAPublicKey(key)
		}
	}

	return nil, fmt.Errorf("key with kid %s not found", kid)
}

// jwkToRSAPublicKey converts a JWK to an RSA public key
func (c *CognitoConfig) jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode N: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode E: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// ValidateToken validates a Cognito JWT token
func (c *CognitoConfig) ValidateToken(tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get the key ID from the token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		// Get the public key for this kid
		return c.getPublicKey(kid)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token or claims")
	}

	// Validate issuer
	expectedIssuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", c.Region, c.UserPoolID)
	if claims.Iss != expectedIssuer {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Iss)
	}

	// Validate token use (should be "access" for API access)
	if claims.TokenUse != "access" {
		return nil, fmt.Errorf("invalid token use: %s", claims.TokenUse)
	}

	// Validate client ID
	if claims.ClientID != c.ClientID {
		return nil, fmt.Errorf("invalid client ID: %s", claims.ClientID)
	}

	return claims, nil
}

// AuthMiddleware creates a middleware for authenticating requests
func (c *CognitoConfig) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Remove "Bearer " prefix
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Bearer token required", http.StatusUnauthorized)
			return
		}

		// Validate the token
		claims, err := c.ValidateToken(tokenString)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		// Add user info to request context
		ctx := context.WithValue(r.Context(), "user", claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserFromContext extracts user claims from request context
func GetUserFromContext(ctx context.Context) (*UserClaims, bool) {
	user, ok := ctx.Value("user").(*UserClaims)
	return user, ok
}
