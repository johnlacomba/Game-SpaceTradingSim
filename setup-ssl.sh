#!/bin/bash

# SSL Certificate Setup for Production

set -e

DOMAIN="space-trader.click"
EMAIL="admin@space-trader.click"

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
if [ -f "ssl/fullchain.pem" ] && [ -f "ssl/privkey.pem" ]; then
    echo "✅ SSL certificates successfully obtained!"
    echo "📋 Certificate files:"
    echo "  ssl/fullchain.pem"
    echo "  ssl/privkey.pem"
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
