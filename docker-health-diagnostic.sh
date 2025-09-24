#!/bin/bash

echo "🐳 Docker Health Check Diagnostic"
echo "================================="

echo ""
echo "📋 Checking Docker Compose Configuration..."

# Check if docker-compose file exists
if [ -f "docker-compose.yml" ]; then
    echo "✅ docker-compose.yml found"
else
    echo "❌ docker-compose.yml not found"
    exit 1
fi

# Check nginx configuration
echo ""
echo "🔧 Checking Nginx Configuration..."
if [ -f "nginx/default.conf" ]; then
    echo "✅ nginx/default.conf found"
    
    # Check if health endpoint exists on HTTP
    if grep -q "location /health" nginx/default.conf; then
        echo "✅ Health endpoint configured"
        
        # Check if it's in the HTTP server block (port 80)
        echo "🔍 Checking health endpoint placement..."
        
        # Extract the HTTP server block and check if health is there
        if awk '/server {/,/}/' nginx/default.conf | awk '/listen 80/,/}/' | grep -q "location /health"; then
            echo "✅ Health endpoint available on HTTP (port 80)"
        else
            echo "⚠️  Health endpoint might only be on HTTPS"
        fi
    else
        echo "❌ Health endpoint not found"
    fi
else
    echo "❌ nginx/default.conf not found"
fi

# Check frontend build configuration
echo ""
echo "🏗️  Checking Frontend Build Configuration..."
if [ -f "frontend/Dockerfile.build-only" ]; then
    echo "✅ Frontend build Dockerfile found"
else
    echo "❌ Frontend build Dockerfile not found"
fi

if [ -f "frontend/.env.local" ]; then
    echo "✅ Frontend environment file found"
    
    # Check for conflicting environment settings
    if grep -q "NODE_ENV.*production" frontend/.env.local; then
        echo "✅ Production mode set in frontend env"
    elif grep -q "VITE_DEV_MODE.*true" frontend/.env.local; then
        echo "⚠️  Development mode set in frontend env"
        echo "   This might conflict with Docker production build"
    fi
else
    echo "⚠️  No frontend environment file found"
fi

echo ""
echo "🔧 Recommended Fix Applied:"
echo "========================="
echo "1. ✅ Modified nginx/default.conf to allow /health on HTTP"
echo "2. ✅ Updated docker-compose.yml health check"
echo ""
echo "🚀 Next Steps:"
echo "1. Stop current containers: docker-compose down"
echo "2. Rebuild and restart: docker-compose up --build"
echo ""
echo "If issues persist, check:"
echo "- Frontend build logs: docker-compose logs frontend-builder"
echo "- Nginx logs: docker-compose logs frontend"
echo "- Backend logs: docker-compose logs backend"
