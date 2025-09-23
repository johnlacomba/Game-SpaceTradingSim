#!/bin/bash

# Development setup script for Space Trader HTTPS

echo "🔧 Space Trader HTTPS Development Setup"
echo "======================================"

# Check if backend server is running
echo "📡 Checking backend server..."
if curl -k -s https://localhost:8443/rooms > /dev/null 2>&1; then
    echo "✅ Backend server is running on https://localhost:8443"
else
    echo "❌ Backend server is not running on https://localhost:8443"
    echo "   Start it with: cd backend && go run ./cmd/server -https-port=8443"
    exit 1
fi

# Check if frontend server is running
echo "🌐 Checking frontend server..."
if curl -k -s https://localhost:5173 > /dev/null 2>&1; then
    echo "✅ Frontend server is running on https://localhost:5173"
else
    echo "❌ Frontend server is not running on https://localhost:5173"
    echo "   Start it with: cd frontend && npm run dev"
    exit 1
fi

echo ""
echo "🎉 Both servers are running!"
echo ""
echo "📋 Next steps:"
echo "1. Open https://localhost:5173 in your browser"
echo "2. If you see certificate warnings, click 'Advanced' → 'Proceed to localhost'"
echo "3. For the WebSocket connection, you may also need to visit:"
echo "   https://localhost:8443/rooms"
echo "   and accept the certificate there as well"
echo ""
echo "🚀 Your Space Trader application is ready for secure development!"

# Optionally open the browser
if command -v open > /dev/null 2>&1; then
    echo ""
    read -p "Would you like to open the application in your browser? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🌐 Opening browser..."
        open https://localhost:5173
        sleep 2
        open https://localhost:8443/rooms
    fi
fi
