#!/bin/bash

# Production deployment script for Space Trader

set -e

echo "🚀 Space Trader Production Deployment"
echo "======================================"

# Configuration
DOMAIN=${1:-"your-domain.com"}
BACKEND_PORT=${2:-"8443"}
FRONTEND_PORT=${3:-"443"}

echo "Domain: $DOMAIN"
echo "Backend HTTPS Port: $BACKEND_PORT"
echo "Frontend HTTPS Port: $FRONTEND_PORT"

# Check if certificates exist
if [ ! -f "../backend/certs/server-san.crt" ] || [ ! -f "../backend/certs/server-san.key" ]; then
    echo "❌ SSL certificates not found!"
    echo "Please ensure you have proper SSL certificates for production."
    echo "For Let's Encrypt certificates, use certbot:"
    echo "  sudo certbot certonly --standalone -d $DOMAIN"
    echo ""
    echo "Then copy them to the certs directory:"
    echo "  cp /etc/letsencrypt/live/$DOMAIN/fullchain.pem ../backend/certs/server-san.crt"
    echo "  cp /etc/letsencrypt/live/$DOMAIN/privkey.pem ../backend/certs/server-san.key"
    exit 1
fi

# Update production environment
echo "📝 Updating production environment..."
cat > .env.production << EOF
VITE_API_BASE_URL=https://$DOMAIN:$BACKEND_PORT
VITE_WS_URL=wss://$DOMAIN:$BACKEND_PORT/ws
VITE_DEV_MODE=false
EOF

# Build frontend
echo "🏗️  Building frontend..."
npm run build

# Build backend
echo "🏗️  Building backend..."
cd ../backend
go build -o space-trader-server ./cmd/server

echo "✅ Build complete!"
echo ""
echo "🚀 To start the production servers:"
echo ""
echo "Backend (HTTPS):"
echo "  cd backend"
echo "  ./space-trader-server -https-port=$BACKEND_PORT -tls-only"
echo ""
echo "Frontend (serve the dist folder with a web server like nginx):"
echo "  # Example nginx configuration needed for the frontend"
echo ""
echo "📋 Next steps:"
echo "1. Configure your web server (nginx/apache) to serve the frontend dist folder"
echo "2. Start the backend server with the command above"
echo "3. Ensure your firewall allows traffic on ports $BACKEND_PORT and $FRONTEND_PORT"
echo "4. Point your domain's DNS to this server"
