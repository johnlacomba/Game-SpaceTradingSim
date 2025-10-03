#!/bin/bash

echo "🐳 Docker Container Fix Test"
echo "============================"

echo ""
echo "🛑 Stopping existing containers..."
docker-compose down

echo ""
echo "🧹 Cleaning up old containers and images..."
docker-compose rm -f
docker system prune -f

echo ""
echo "🏗️  Building fresh containers..."
docker-compose build --no-cache

echo ""
echo "🚀 Starting containers..."
docker-compose up -d

echo ""
echo "⏳ Waiting for containers to start..."
sleep 10

echo ""
echo "📊 Container Status:"
docker-compose ps

echo ""
echo "🔍 Health Check Status:"
echo "Backend Health:"
docker-compose exec -T backend wget --no-check-certificate -qO- https://localhost:8443/rooms || echo "Backend health check failed"

echo ""
echo "Frontend Health:"
docker-compose exec -T frontend wget -qO- http://localhost/health || echo "Frontend health check failed"

echo ""
echo "📋 Container Logs (last 10 lines each):"
echo ""
echo "=== Backend Logs ==="
docker-compose logs --tail=10 backend

echo ""
echo "=== Frontend Logs ==="
docker-compose logs --tail=10 frontend

echo ""
echo "=== Frontend Builder Logs ==="
docker-compose logs --tail=10 frontend-builder

echo ""
echo "🌐 Testing External Access:"
echo "Frontend should be available at: https://sphereofinfluence.click"
echo "Health check should work at: http://sphereofinfluence.click/health"

echo ""
echo "✅ Docker fix test complete!"
echo ""
echo "If containers are healthy, your authentication integration should work properly."
