# Sphere of Influence

Server-authoritative multiplayer sphere dominance sim with HTTPS/WSS support and Docker deployment.

## Stack
- Backend: Go, Gorilla WebSocket, Gorilla Mux with TLS 1.2+
- Frontend: React + Vite with TypeScript
- Deployment: Docker Compose with Nginx reverse proxy
- SSL: Let's Encrypt certificates with auto-renewal

## Features
- Rooms: create/join from a lobby; per-room server state
- Server turns: 60-second turns; ends early if all humans Ready
- Ready flow: bots are always ready; Start is disabled until all humans are Ready
- Trading: buy/sell with inventory average cost; sell anywhere at local prices
- Planets: per-turn production; travel resolves at turn end; limited goods visibility (current planet)
- Visuals: player color dots, destination arrows, Players dropdown with Ready status
- Persistence: leaving room preserves player state for rejoin
- End Game: vote to end game and close room when all players agree
- Mobile Support: responsive design with touch-friendly interface
- WebSocket Reliability: automatic reconnection with exponential backoff
- Analytics: wealth tracking with interactive pie charts and historical graphs

## Production Deployment

### Prerequisites
Install Docker and Docker Compose on your server:

**Ubuntu/Debian:**
```bash
# Update package index
sudo apt update

# Install required packages
sudo apt install -y apt-transport-https ca-certificates curl gnupg lsb-release

# Add Docker's official GPG key
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg

# Add Docker repository
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Start and enable Docker
sudo systemctl start docker
sudo systemctl enable docker

# Add your user to docker group (optional, to run without sudo)
sudo usermod -aG docker $USER
```

**CentOS/RHEL/Rocky Linux:**
```bash
# Install required packages
sudo yum install -y yum-utils

# Add Docker repository
sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo

# Install Docker
sudo yum install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Start and enable Docker
sudo systemctl start docker
sudo systemctl enable docker

# Add your user to docker group (optional)
sudo usermod -aG docker $USER
```

### One-Command Production Deployment
Replace `sphereofinfluence.click` with your domain and `admin@sphereofinfluence.click` with your email:

```bash
cd && sudo rm -rf Game-SpaceTradingSim ; sudo ./cleanupDocker.sh ; git clone https://github.com/johnlacomba/Game-SpaceTradingSim.git && cd Game-SpaceTradingSim/ && cp cleanupDocker.sh ~/cleanupDocker.sh && sudo ./setup-ssl.sh && sudo ./quick-deploy.sh sphereofinfluence.click admin@sphereofinfluence.click production
```

This command will:
1. Clean up any existing installation
2. Clone the latest code from GitHub
3. Set up SSL certificates with Let's Encrypt
4. Deploy the application with proper SSL configuration
5. Start all services with health checks

### Manual Deployment Steps
If you prefer step-by-step deployment:

1. **Clone the repository:**
```bash
git clone https://github.com/johnlacomba/Game-SpaceTradingSim.git
cd Game-SpaceTradingSim
```

2. **Set up SSL certificates:**
```bash
sudo ./setup-ssl.sh
```

3. **Deploy with your domain:**
```bash
sudo ./quick-deploy.sh your-domain.com your-email@domain.com production
```

### Environment Configuration
The deployment scripts will automatically configure:
- SSL certificates via Let's Encrypt
- Nginx reverse proxy with HTTPS termination
- WebSocket Secure (WSS) connections
- Docker health checks and auto-restart
- Frontend build optimization for production

## Development Setup
For local development without Docker:

**Backend (Go):**
```bash
cd backend
go mod tidy
go run ./cmd/server
```

**Frontend (Vite):**
```bash
cd frontend
npm install
npm run dev
```

Open http://localhost:5173 and click Connect. The client autodetects the backend at ws://<page-host>:8080/ws.

**Local HTTPS Development:**
```bash
# Generate self-signed certificates
cd backend && ./scripts/generate-certs.sh

# Run with HTTPS
go run ./cmd/server -https-port=8443
```

## Architecture
- **Backend**: Secure WebSocket server with ping/pong heartbeat
- **Frontend**: React SPA with automatic reconnection and mobile support
- **Proxy**: Nginx handles SSL termination and static file serving
- **Certificates**: Let's Encrypt with automatic renewal
- **Networking**: All services communicate via Docker internal network

## Monitoring
- Health checks for all services
- Automatic container restart on failure
- SSL certificate auto-renewal
- Connection state monitoring with reconnection

LAN: both servers bind to all interfaces; you can connect from other devices on your network using your machine's IP.
