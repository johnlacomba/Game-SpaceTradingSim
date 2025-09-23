#!/bin/bash

# Production initialization script for Space Trader

set -e

echo "🔧 Space Trader Production Setup"
echo "================================"

# Check for required environment variables
DOMAIN=${DOMAIN:-localhost}
MODE=${DEPLOYMENT_MODE:-development}

echo "Domain: $DOMAIN"
echo "Mode: $MODE"

# Ensure SSL certificates are available
if [ "$DOMAIN" = "localhost" ]; then
    echo "🔒 Setting up development certificates..."
    
    # Copy development certificates to ssl volume
    mkdir -p ssl
    if [ -f "backend/certs/server-san.crt" ] && [ -f "backend/certs/server-san.key" ]; then
        cp backend/certs/server-san.crt ssl/fullchain.pem
        cp backend/certs/server-san.key ssl/privkey.pem
        echo "✅ Development certificates copied"
    else
        echo "❌ Development certificates not found. Generating new ones..."
        cd backend/scripts
        ./generate-certs.sh
        cd ../..
        cp backend/certs/server-san.crt ssl/fullchain.pem
        cp backend/certs/server-san.key ssl/privkey.pem
        echo "✅ New development certificates generated"
    fi
else
    echo "🔒 Production mode: certificates should be obtained via Let's Encrypt"
    if [ ! -f "ssl/fullchain.pem" ] || [ ! -f "ssl/privkey.pem" ]; then
        echo "⚠️  No SSL certificates found for production domain: $DOMAIN"
        echo "   Run: docker-compose --profile ssl-setup run --rm certbot"
        echo "   Or place your certificates in:"
        echo "     ssl/fullchain.pem"
        echo "     ssl/privkey.pem"
        exit 1
    fi
fi

# Set environment variables
export TLS_ONLY=true
if [ "$MODE" = "development" ]; then
    export TLS_ONLY=false
fi

echo "TLS_ONLY: $TLS_ONLY"

# Create necessary directories
mkdir -p nginx/logs nginx/webroot certbot/logs database/init

echo "✅ Production setup complete!"
echo ""
echo "🚀 Ready to start services with:"
echo "   docker-compose up -d"
