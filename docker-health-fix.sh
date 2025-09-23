#!/bin/bash

echo "🩺 Docker Health Check Fix"
echo "========================="

echo ""
echo "❌ Problem Identified:"
echo "   Backend health check was trying to access /rooms (requires auth)"
echo "   Should use /health (no auth required)"
echo ""
echo "✅ Fix Applied:"
echo "   Updated backend health check to use http://localhost:8080/health"
echo ""

echo "🛑 Stopping containers..."
docker-compose down

echo ""
echo "🧹 Cleaning up..."
docker system prune -f

echo ""
echo "🏗️  Rebuilding containers..."
docker-compose build --no-cache

echo ""
echo "🚀 Starting containers..."
docker-compose up -d

echo ""
echo "⏳ Waiting for health checks..."
sleep 30

echo ""
echo "📊 Container Status:"
docker-compose ps

echo ""
echo "🔍 Health Check Tests:"
echo ""

# Test backend health directly
echo "Testing backend health endpoint..."
if docker-compose exec -T backend wget -qO- http://localhost:8080/health >/dev/null 2>&1; then
    echo "✅ Backend health check: PASS"
else
    echo "❌ Backend health check: FAIL"
    echo "Backend logs:"
    docker-compose logs --tail=5 backend
fi

echo ""
# Test frontend health directly  
echo "Testing frontend health endpoint..."
if docker-compose exec -T frontend wget -qO- http://localhost/health >/dev/null 2>&1; then
    echo "✅ Frontend health check: PASS"
else
    echo "❌ Frontend health check: FAIL"
    echo "Frontend logs:"
    docker-compose logs --tail=5 frontend
fi

echo ""
echo "📋 Summary:"
echo "=========="

# Check if all containers are healthy
BACKEND_HEALTHY=$(docker-compose ps backend | grep -c "healthy")
FRONTEND_HEALTHY=$(docker-compose ps frontend | grep -c "healthy")

if [ "$BACKEND_HEALTHY" -gt 0 ] && [ "$FRONTEND_HEALTHY" -gt 0 ]; then
    echo "🎉 SUCCESS: All containers are healthy!"
    echo ""
    echo "🌐 Your application should now be accessible:"
    echo "   - Frontend: https://space-trader.click"
    echo "   - Backend API: https://space-trader.click/api/"
    echo "   - WebSocket: wss://space-trader.click/ws"
    echo ""
    echo "🔐 Authentication Status:"
    echo "   - ✅ Backend has Cognito auth middleware ready"
    echo "   - ✅ Frontend configured for AWS Cognito"
    echo "   - ⚠️  You still need to deploy Terraform for real AWS auth"
    echo "   - 🔧 Current setup uses test/mock Cognito values"
else
    echo "⚠️  Some containers are still unhealthy"
    echo "Check logs with: docker-compose logs [service-name]"
fi

echo ""
echo "🔧 Next Steps:"
echo "1. If healthy: Your app is ready!"
echo "2. For real auth: Deploy Terraform infrastructure"
echo "3. For troubleshooting: Check individual container logs"
