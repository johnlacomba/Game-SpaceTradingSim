#!/bin/bash

# Test script to check where certificates are located

echo "🔍 Checking certificate locations after setup..."

# Detect compose project name similarly to setup-ssl.sh
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

# Check what's in the ssl_certs volume
echo "📁 Contents of ssl_certs volume:"
docker run --rm -v "$SSL_VOLUME":/ssl alpine find /ssl -type f -name "*.pem" 2>/dev/null || echo "No .pem files found"

echo ""
echo "📁 Directory structure of ssl_certs volume:"
docker run --rm -v "$SSL_VOLUME":/ssl alpine ls -la /ssl/ 2>/dev/null || echo "Volume not found or empty"

echo ""
echo "🔍 Check if nginx can access the certificates:"
if docker-compose exec frontend test -f /etc/nginx/ssl/fullchain.pem 2>/dev/null; then
    echo "✅ Nginx can access fullchain.pem"
else
    echo "❌ Nginx cannot access fullchain.pem"
fi

if docker-compose exec frontend test -f /etc/nginx/ssl/privkey.pem 2>/dev/null; then
    echo "✅ Nginx can access privkey.pem"
else
    echo "❌ Nginx cannot access privkey.pem"
fi
