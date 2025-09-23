#!/bin/bash

# Simplified Docker deployment for Space Trader

set -e

echo "🚀 Space Trader Deployment"
echo "=========================="

# Parse arguments
DOMAIN=${1:-"localhost"}
EMAIL=${2:-"admin@example.com"}
MODE=${3:-"development"}

echo "Domain: $DOMAIN"
echo "Email: $EMAIL"
echo "Mode: $MODE"

# Create necessary directories
mkdir -p nginx/logs nginx/webroot certbot/logs ssl

# Set up environment
cp .env.docker .env
sed -i "s/DOMAIN=localhost/DOMAIN=$DOMAIN/g" .env
sed -i "s/CERTBOT_EMAIL=admin@example.com/CERTBOT_EMAIL=$EMAIL/g" .env

if [ "$MODE" = "production" ]; then
    sed -i "s/DEPLOYMENT_MODE=development/DEPLOYMENT_MODE=production/g" .env
    sed -i "s/TLS_ONLY=false/TLS_ONLY=true/g" .env
fi

# Handle SSL certificates
if [ "$DOMAIN" = "localhost" ]; then
    echo "🔒 Setting up development certificates..."
    
    # Ensure development certificates exist
    if [ ! -f "backend/certs/server-san.crt" ] || [ ! -f "backend/certs/server-san.key" ]; then
        echo "Generating development certificates..."
        cd backend/scripts && ./generate-certs.sh && cd ../..
    fi
    
    # Copy to ssl directory for nginx
    cp backend/certs/server-san.crt ssl/fullchain.pem
    cp backend/certs/server-san.key ssl/privkey.pem
    echo "✅ Development certificates ready"
    
else
    echo "🔒 Production mode: Setting up SSL certificates for $DOMAIN"
    
    # Check if certificates already exist
    if [ -f "ssl/fullchain.pem" ] && [ -f "ssl/privkey.pem" ]; then
        echo "✅ SSL certificates already exist"
    else
        echo "📋 To obtain SSL certificates for production:"
        echo "1. Ensure your domain $DOMAIN points to this server"
        echo "2. Make sure ports 80 and 443 are open"
        echo "3. Run: docker-compose --profile ssl-setup run --rm certbot"
        echo ""
        echo "Or place your own certificates as:"
        echo "  ssl/fullchain.pem"
        echo "  ssl/privkey.pem"
        echo ""
        read -p "Continue with existing certificates or self-signed? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Deployment cancelled. Please set up certificates first."
            exit 1
        fi
        
        # Use self-signed certificates as fallback
        echo "Using self-signed certificates for $DOMAIN..."
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout ssl/privkey.pem \
            -out ssl/fullchain.pem \
            -subj "/C=US/ST=CA/L=San Francisco/O=SpaceTrader/CN=$DOMAIN"
    fi
fi

# Build frontend if needed
echo "🏗️  Building frontend..."
if [ "$MODE" = "production" ]; then
    docker-compose --profile build run --rm frontend-builder
fi

# Start services
echo "🚀 Starting services..."
if [ "$MODE" = "production" ]; then
    # Production with all services
    docker-compose --profile database --profile cache up -d
else
    # Development - basic services only
    docker-compose up -d
fi

# Wait for services
echo "⏳ Waiting for services to start..."
sleep 15

# Check health
echo "🏥 Checking service health..."
docker-compose ps

# Test connectivity
echo "🔗 Testing connectivity..."
if [ "$DOMAIN" = "localhost" ]; then
    if curl -k -s https://localhost:8443/rooms > /dev/null; then
        echo "✅ Backend is responding"
    else
        echo "⚠️  Backend may not be ready yet"
    fi
else
    if curl -k -s https://$DOMAIN/rooms > /dev/null; then
        echo "✅ Backend is responding"
    else
        echo "⚠️  Backend may not be ready yet"
    fi
fi

echo ""
echo "🎉 Deployment completed!"
echo ""
if [ "$DOMAIN" = "localhost" ]; then
    echo "🌐 Access your application:"
    echo "  Frontend: https://localhost"
    echo "  Backend: https://localhost/api/"
    echo "  Direct Backend: https://localhost:8443"
    echo ""
    echo "⚠️  You may need to accept certificate warnings in your browser"
else
    echo "🌐 Access your application:"
    echo "  Frontend: https://$DOMAIN"
    echo "  Backend: https://$DOMAIN/api/"
fi

echo ""
echo "🛠️  Management commands:"
echo "  docker-compose logs -f [service]   # View logs"
echo "  docker-compose restart [service]   # Restart service"
echo "  docker-compose down                # Stop all services"
