#!/bin/bash

# Quick test script for Space Trader Docker setup

echo "🧪 Testing Space Trader Docker Setup"
echo "===================================="

# Test basic docker-compose build
echo "📦 Testing Docker build..."
if docker-compose build --no-cache backend; then
    echo "✅ Backend build successful"
else
    echo "❌ Backend build failed"
    exit 1
fi

# Test certificate setup
echo "🔒 Testing certificate setup..."
if [ -f "backend/certs/server-san.crt" ] && [ -f "backend/certs/server-san.key" ]; then
    echo "✅ Certificates found"
else
    echo "⚠️  Certificates not found, generating..."
    cd backend/scripts && ./generate-certs.sh && cd ../..
fi

# Test environment setup
echo "🔧 Testing environment setup..."
if ./production-setup.sh; then
    echo "✅ Production setup successful"
else
    echo "❌ Production setup failed"
    exit 1
fi

# Test docker-compose config
echo "📋 Testing docker-compose configuration..."
if docker-compose config > /dev/null; then
    echo "✅ Docker-compose configuration valid"
else
    echo "❌ Docker-compose configuration invalid"
    exit 1
fi

echo ""
echo "🎉 All tests passed! Ready to deploy with:"
echo "   ./docker-deploy.sh localhost admin@example.com development"
echo "   # or"
echo "   docker-compose up -d"
