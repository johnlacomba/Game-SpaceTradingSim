#!/bin/bash

# Script to generate SSL certificates for development and production

CERT_DIR="../certs"
DAYS=365

# Create certs directory if it doesn't exist
mkdir -p "$CERT_DIR"

echo "Generating SSL certificates..."

# Generate private key
openssl genrsa -out "$CERT_DIR/server.key" 2048

# Generate certificate signing request
openssl req -new -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.csr" -subj "/C=US/ST=CA/L=San Francisco/O=SpaceTrader/CN=localhost"

# Generate self-signed certificate
openssl x509 -req -in "$CERT_DIR/server.csr" -signkey "$CERT_DIR/server.key" -out "$CERT_DIR/server.crt" -days $DAYS

# Generate certificate for additional domains (for production)
cat > "$CERT_DIR/cert.conf" << EOF
[req]
default_bits = 2048
prompt = no
distinguished_name = dn
req_extensions = v3_req

[dn]
C=US
ST=CA
L=San Francisco
O=SpaceTrader
CN=localhost

[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = 127.0.0.1
DNS.3 = ::1
EOF

# Generate key and certificate with SAN (Subject Alternative Names)
openssl req -new -newkey rsa:2048 -nodes -keyout "$CERT_DIR/server-san.key" -out "$CERT_DIR/server-san.csr" -config "$CERT_DIR/cert.conf"
openssl x509 -req -in "$CERT_DIR/server-san.csr" -signkey "$CERT_DIR/server-san.key" -out "$CERT_DIR/server-san.crt" -days $DAYS -extensions v3_req -extfile "$CERT_DIR/cert.conf"

echo "SSL certificates generated successfully!"
echo "Files created:"
echo "  - $CERT_DIR/server.key (private key)"
echo "  - $CERT_DIR/server.crt (certificate)"
echo "  - $CERT_DIR/server-san.key (private key with SAN)"
echo "  - $CERT_DIR/server-san.crt (certificate with SAN)"

# Clean up temporary files
rm "$CERT_DIR/server.csr" "$CERT_DIR/server-san.csr" "$CERT_DIR/cert.conf"

echo ""
echo "Note: These are self-signed certificates for development."
echo "For production, use certificates from a trusted Certificate Authority (CA)."
