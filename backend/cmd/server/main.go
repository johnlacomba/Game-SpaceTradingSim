package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/example/space-trader/internal/auth"
	srv "github.com/example/space-trader/internal/server"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	var (
		httpPort  = flag.String("http-port", "8080", "HTTP port")
		httpsPort = flag.String("https-port", "8443", "HTTPS port")
		certFile  = flag.String("cert", "", "Path to certificate file")
		keyFile   = flag.String("key", "", "Path to private key file")
		tlsOnly   = flag.Bool("tls-only", false, "Only serve HTTPS")
	)
	flag.Parse()

	r := mux.NewRouter()

	// Initialize Cognito auth
	cognitoConfig := auth.NewCognitoConfig()

	gs := srv.NewGameServer()

	// Health check endpoint (no auth required)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	// Protected routes that require authentication
	protected := r.PathPrefix("/api").Subrouter()
	protected.Use(cognitoConfig.AuthMiddleware)

	// WebSocket endpoint (requires auth via query parameter or header)
	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		gs.HandleWS(w, r, cognitoConfig)
	})

	// Debug REST endpoints (protected)
	protected.HandleFunc("/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gs.HandleListRooms(w, r)
			return
		}
		if r.Method == http.MethodPost {
			gs.HandleCreateRoom(w, r)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// User profile endpoint
	protected.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		gs.HandleGetProfile(w, r)
	}).Methods("GET")

	// Add CORS headers for HTTPS
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Determine certificate paths
	var certPath, keyPath string
	if *certFile != "" && *keyFile != "" {
		certPath = *certFile
		keyPath = *keyFile
	} else {
		// Default to generated certificates relative to working directory
		certPath = "certs/server-san.crt"
		keyPath = "certs/server-san.key"
	}

	// Check if certificates exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		log.Printf("Certificate file not found at %s", certPath)
		if !*tlsOnly {
			log.Printf("Falling back to HTTP only on port %s", *httpPort)
			log.Fatal(http.ListenAndServe(":"+*httpPort, r))
		} else {
			log.Printf("TLS-only mode enabled but certificates not found")
			log.Printf("Please ensure certificates are mounted or available at %s and %s", certPath, keyPath)
			log.Fatal("Exiting due to missing certificates in TLS-only mode")
		}
	}

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		log.Printf("Private key file not found at %s", keyPath)
		if !*tlsOnly {
			log.Printf("Falling back to HTTP only on port %s", *httpPort)
			log.Fatal(http.ListenAndServe(":"+*httpPort, r))
		} else {
			log.Fatal("TLS-only mode enabled but private key not found")
		}
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	// Start HTTPS server
	go func() {
		httpsAddr := ":" + *httpsPort
		log.Printf("Space Trader backend (HTTPS) listening on %s", httpsAddr)

		server := &http.Server{
			Addr:      httpsAddr,
			Handler:   r,
			TLSConfig: tlsConfig,
		}

		if err := server.ListenAndServeTLS(certPath, keyPath); err != nil {
			log.Fatal("HTTPS server failed:", err)
		}
	}()

	// Start HTTP server (redirect to HTTPS) unless TLS-only mode
	if !*tlsOnly {
		httpAddr := ":" + *httpPort
		log.Printf("Space Trader backend (HTTP->HTTPS redirect) listening on %s", httpAddr)

		// HTTP server that redirects to HTTPS
		httpServer := &http.Server{
			Addr: httpAddr,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpsURL := "https://" + r.Host
				if *httpsPort != "443" {
					httpsURL += ":" + *httpsPort
				}
				httpsURL += r.RequestURI

				http.Redirect(w, r, httpsURL, http.StatusMovedPermanently)
			}),
		}

		log.Fatal(httpServer.ListenAndServe())
	} else {
		// Block forever
		select {}
	}
}
