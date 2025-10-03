#!/bin/bash

# SSL Certificate Setup for Production

set -e

DOMAIN="sphereofinfluence.click"
EMAIL="admin@sphereofinfluence.click"

# Determine docker compose project/volume naming (reuse .env/.env.docker if available)
if [ -z "$COMPOSE_PROJECT_NAME" ]; then
    if [ -f .env ] && grep -q '^COMPOSE_PROJECT_NAME=' .env; then
        COMPOSE_PROJECT_NAME=$(grep '^COMPOSE_PROJECT_NAME=' .env | tail -1 | cut -d'=' -f2-)
    elif [ -f .env.docker ] && grep -q '^COMPOSE_PROJECT_NAME=' .env.docker; then
        COMPOSE_PROJECT_NAME=$(grep '^COMPOSE_PROJECT_NAME=' .env.docker | tail -1 | cut -d'=' -f2-)
    fi
fi

DEFAULT_COMPOSE_PROJECT=$(basename "$PWD" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/-\{2,\}/-/g')
COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-$DEFAULT_COMPOSE_PROJECT}"
export COMPOSE_PROJECT_NAME="$COMPOSE_PROJECT"
SSL_VOLUME="${COMPOSE_PROJECT}_ssl_certs"

if [ -z "$DOMAIN" ]; then
    echo "Usage: $0 <domain> [email]"
    echo "Example: $0 myapp.com admin@myapp.com"
    exit 1
fi

echo "🔒 Setting up SSL certificates for $DOMAIN"
echo "Email: $EMAIL"

# Update environment
export DOMAIN="$DOMAIN"
export CERTBOT_EMAIL="$EMAIL"

# Create webroot directory
mkdir -p nginx/webroot

# Start a temporary nginx container for certificate verification
echo "🌐 Starting temporary web server for certificate verification..."
docker run -d --name temp-nginx \
    -p 80:80 \
    -v $(pwd)/nginx/webroot:/usr/share/nginx/html \
    nginx:alpine

# Wait a moment for nginx to start
sleep 3

# Run certbot
echo "📜 Requesting certificate from Let's Encrypt..."
docker-compose --profile ssl-setup run --rm certbot

# Stop temporary nginx
echo "🛑 Stopping temporary web server..."
docker stop temp-nginx && docker rm temp-nginx

# Check if certificates were created
echo "🔍 Checking certificate files..."

# Check in the Docker volume (where they actually are)
if docker run --rm -v "$SSL_VOLUME":/ssl alpine test -f /ssl/fullchain.pem && \
    docker run --rm -v "$SSL_VOLUME":/ssl alpine test -f /ssl/privkey.pem; then
    echo "✅ SSL certificates successfully obtained!"
    echo "📋 Certificate files stored in Docker volume:"
    echo "  ${SSL_VOLUME}:/fullchain.pem"
    echo "  ${SSL_VOLUME}:/privkey.pem"
    
    # Optional: Copy to local ssl/ directory for backup/inspection
    echo "📁 Creating local backup copies..."
    mkdir -p ssl
    docker run --rm -v "$SSL_VOLUME":/ssl -v $(pwd)/ssl:/backup alpine cp /ssl/fullchain.pem /backup/
    docker run --rm -v "$SSL_VOLUME":/ssl -v $(pwd)/ssl:/backup alpine cp /ssl/privkey.pem /backup/
    echo "  Local copies: ssl/fullchain.pem, ssl/privkey.pem"
    
    echo ""
    echo "🚀 You can now run:"
    echo "  ./quick-deploy.sh $DOMAIN $EMAIL production"
else
    echo "❌ Certificate generation failed"
    echo "Please check:"
    echo "  - Domain $DOMAIN points to this server"
    echo "  - Port 80 is accessible from the internet"
    echo "  - No other web server is running on port 80"
    exit 1
fi
