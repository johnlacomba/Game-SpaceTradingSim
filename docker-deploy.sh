#!/bin/bash

# Space Trader Docker Deployment Script

set -e

echo "🚀 Space Trader Docker Deployment"
echo "=================================="

# Configuration
DOMAIN=${1:-"localhost"}
EMAIL=${2:-"admin@example.com"}
MODE=${3:-"development"}

echo "Domain: $DOMAIN"
echo "Email: $EMAIL"
echo "Mode: $MODE"

# Create necessary directories
echo "📁 Creating directories..."
mkdir -p nginx/logs
mkdir -p nginx/webroot
mkdir -p certbot/logs
mkdir -p database/init

# Copy environment file
echo "🔧 Setting up environment..."
cp .env.docker .env

# Update environment with provided values
sed -i "s/DOMAIN=localhost/DOMAIN=$DOMAIN/g" .env
sed -i "s/CERTBOT_EMAIL=admin@example.com/CERTBOT_EMAIL=$EMAIL/g" .env

if [ "$MODE" = "production" ]; then
    sed -i "s/DEPLOYMENT_MODE=development/DEPLOYMENT_MODE=production/g" .env
    sed -i "s/SSL_VERIFY=false/SSL_VERIFY=true/g" .env
    sed -i "s/TLS_ONLY=false/TLS_ONLY=true/g" .env
fi

# Run production setup
echo "🔧 Running production setup..."
export DOMAIN="$DOMAIN"
export DEPLOYMENT_MODE="$MODE"
./production-setup.sh

# Build frontend
echo "🏗️  Building frontend..."
docker-compose --profile build run --rm frontend-builder

# Generate or copy SSL certificates
if [ "$DOMAIN" = "localhost" ]; then
    echo "🔒 Using development certificates..."
    mkdir -p ssl
    cp backend/certs/server-san.crt ssl/fullchain.pem
    cp backend/certs/server-san.key ssl/privkey.pem
else
    echo "🔒 Setting up SSL certificates for $DOMAIN..."
    if [ "$MODE" = "production" ]; then
        # Run certbot to get real certificates
        docker-compose --profile ssl-setup run --rm certbot
    else
        echo "⚠️  Using self-signed certificates for staging"
        # Generate certificates for the domain
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout ssl/privkey.pem \
            -out ssl/fullchain.pem \
            -subj "/C=US/ST=CA/L=San Francisco/O=SpaceTrader/CN=$DOMAIN"
    fi
fi

# Start the application
echo "🚀 Starting Space Trader..."

if [ "$MODE" = "production" ]; then
    # Production deployment with all services
    docker-compose --profile database --profile cache up -d
else
    # Development deployment (basic services only)
    docker-compose up -d
fi

# Wait for services to be healthy
echo "⏳ Waiting for services to be ready..."
sleep 10

# Check health
echo "🏥 Checking service health..."
if docker-compose ps | grep -q "unhealthy"; then
    echo "❌ Some services are unhealthy:"
    docker-compose ps
    echo ""
    echo "📋 To debug, check logs:"
    echo "  docker-compose logs backend"
    echo "  docker-compose logs frontend"
    exit 1
else
    echo "✅ All services are healthy!"
fi

echo ""
echo "🎉 Space Trader is now running!"
echo ""
echo "📱 Access your application:"
if [ "$DOMAIN" = "localhost" ]; then
    echo "  Frontend: https://localhost (accept certificate warning)"
    echo "  Backend API: https://localhost/api/"
    echo "  WebSocket: wss://localhost/ws"
else
    echo "  Frontend: https://$DOMAIN"
    echo "  Backend API: https://$DOMAIN/api/"
    echo "  WebSocket: wss://$DOMAIN/ws"
fi

echo ""
echo "🛠️  Management commands:"
echo "  View logs: docker-compose logs -f [service]"
echo "  Stop: docker-compose down"
echo "  Restart: docker-compose restart [service]"
echo "  Update: ./docker-deploy.sh $DOMAIN $EMAIL $MODE"

if [ "$MODE" = "production" ]; then
    echo ""
    echo "📊 Monitoring (if enabled):"
    echo "  Prometheus: http://$DOMAIN:9090"
fi
