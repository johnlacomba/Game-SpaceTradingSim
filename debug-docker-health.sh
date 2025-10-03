#!/bin/bash

echo "🔍 Docker Health Check Debug"
echo "============================"

echo ""
echo "Testing the exact Docker health check command inside the container..."

COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-sphereofinfluence}"
BACKEND_CONTAINER_PREFIX="${COMPOSE_PROJECT}_backend"

if sudo docker ps | grep -q "$BACKEND_CONTAINER_PREFIX"; then
    CONTAINER_ID=$(sudo docker ps | grep "$BACKEND_CONTAINER_PREFIX" | awk '{print $1}')
    echo "Found backend container: $CONTAINER_ID"
    
    echo ""
    echo "Test 1: Basic wget test"
    sudo docker exec $CONTAINER_ID wget -qO- http://localhost:8080/health
    
    echo ""
    echo "Test 2: Exact health check command (with verbose output)"
    sudo docker exec $CONTAINER_ID sh -c "wget --quiet --tries=1 --spider http://localhost:8080/health && echo 'HEALTH CHECK SUCCESS' || echo 'HEALTH CHECK FAILED'"
    
    echo ""
    echo "Test 3: Health check command without --quiet"
    sudo docker exec $CONTAINER_ID sh -c "wget --tries=1 --spider http://localhost:8080/health"
    
    echo ""
    echo "Test 4: Check wget exit code"
    sudo docker exec $CONTAINER_ID sh -c "wget --quiet --tries=1 --spider http://localhost:8080/health; echo 'Exit code:' \$?"
    
    echo ""
    echo "Test 5: Alternative with curl"
    sudo docker exec $CONTAINER_ID curl -f -s http://localhost:8080/health && echo " <- curl result"
    
    echo ""
    echo "Test 6: Check if port is actually listening"
    sudo docker exec $CONTAINER_ID netstat -ln | grep 8080
    
    echo ""
    echo "Test 7: Container logs (last 5 lines)"
    sudo docker logs --tail=5 $CONTAINER_ID
    
else
    echo "❌ Backend container not found or not running"
    echo "Current containers:"
    sudo docker ps
fi

echo ""
echo "🔍 Docker health check status:"
sudo docker inspect $(sudo docker ps | grep "$BACKEND_CONTAINER_PREFIX" | awk '{print $1}') | grep -A 20 '"Health"'
