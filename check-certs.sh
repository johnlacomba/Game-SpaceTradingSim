#!/bin/bash

# Test script to check where certificates are located

echo "🔍 Checking certificate locations after setup..."

# Check what's in the ssl_certs volume
echo "📁 Contents of ssl_certs volume:"
docker run --rm -v game-spacetradingsim_ssl_certs:/ssl alpine find /ssl -type f -name "*.pem" 2>/dev/null || echo "No .pem files found"

echo ""
echo "📁 Directory structure of ssl_certs volume:"
docker run --rm -v game-spacetradingsim_ssl_certs:/ssl alpine ls -la /ssl/ 2>/dev/null || echo "Volume not found or empty"

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
