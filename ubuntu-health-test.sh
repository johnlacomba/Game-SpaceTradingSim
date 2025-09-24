#!/bin/bash

echo "🩺 Health Check Fix - Ubuntu Server Test"
echo "========================================"

echo ""
echo "Current issue: Health endpoint works but Docker still reports unhealthy"
echo "Fix: Simplified health check command for better reliability"

echo ""
echo "Step 1: Stop current containers"
sudo docker-compose down

echo ""
echo "Step 2: Start backend with new health check"
sudo docker-compose up -d backend

echo ""
echo "Step 3: Monitor health check progress..."
echo "Waiting 60 seconds for health checks to complete..."

for i in {1..12}; do
    sleep 5
    STATUS=$(sudo docker ps --format "table {{.Names}}\t{{.Status}}" | grep backend | awk '{print $2}')
    echo "Health check attempt $i: $STATUS"
    
    if [[ "$STATUS" == *"healthy"* ]]; then
        echo "✅ Backend is now healthy!"
        break
    fi
done

echo ""
echo "Step 4: Final status check"
sudo docker ps

echo ""
echo "Step 5: Test health endpoint manually"
echo "From host:"
curl -s http://localhost:8080/health && echo " ← Host can reach health endpoint"

echo ""
echo "From inside container:"
CONTAINER_ID=$(sudo docker ps | grep backend | awk '{print $1}')
if [ ! -z "$CONTAINER_ID" ]; then
    sudo docker exec $CONTAINER_ID wget -qO- http://localhost:8080/health && echo " ← Container can reach health endpoint"
fi

echo ""
echo "Step 6: Check Docker health details"
if [ ! -z "$CONTAINER_ID" ]; then
    echo "Docker health check details:"
    sudo docker inspect $CONTAINER_ID | grep -A 15 '"Health"' || echo "No health details found"
fi

echo ""
echo "✅ Test complete!"
echo ""
echo "If still unhealthy, the issue might be:"
echo "1. Docker taking longer than expected to register health"
echo "2. Network connectivity issues inside container"
echo "3. Need to use curl instead of wget"
