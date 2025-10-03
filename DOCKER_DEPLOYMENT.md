# Docker Deployment Guide for Sphere of Influence

This guide provides complete instructions for deploying Sphere of Influence using Docker with full automation.

## 🚀 Quick Start

### Development Deployment
```bash
./docker-deploy.sh localhost admin@example.com development
```

### Production Deployment
```bash
./docker-deploy.sh yourdomain.com your-email@domain.com production
```

## 📦 What's Included

The Docker Compose setup includes all necessary dependencies:

### Core Services
- **Backend**: Go application with HTTPS/WSS support
- **Frontend**: React app built and served via Nginx
- **Nginx**: Reverse proxy with SSL termination and static file serving

### Optional Services (Production Profiles)
- **Database**: PostgreSQL for persistent data storage
- **Redis**: Caching and session management
- **Certbot**: Automatic SSL certificate management
- **Prometheus**: Application monitoring

## 🏗️ Architecture

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Internet  │───▶│    Nginx    │───▶│   Backend   │
│             │    │ (SSL Term.) │    │ (Go Server) │
└─────────────┘    └─────────────┘    └─────────────┘
                          │
                          ▼
                   ┌─────────────┐
                   │  Frontend   │
                   │ (React SPA) │
                   └─────────────┘
```

## 🔧 Configuration Files

### Core Configuration
- `docker-compose.yml` - Main orchestration file
- `.env.docker` - Environment variables template
- `nginx/nginx.conf` - Nginx main configuration
- `nginx/default.conf` - Site-specific Nginx config

### SSL & Security
- `backend/certs/` - SSL certificates directory
- `ssl/` - Docker volume for SSL certificates
- Auto-generated Let's Encrypt certificates for production

### Build Configuration
- `backend/Dockerfile` - Backend container build
- `frontend/Dockerfile.build` - Frontend build container

## 🚀 Deployment Options

### 1. Development Mode
```bash
# Uses self-signed certificates
./docker-deploy.sh localhost admin@example.com development

# Or manually:
docker-compose up -d
```

**Features:**
- Self-signed SSL certificates
- Development-friendly error messages
- Hot reload disabled (production build)
- Basic services only

### 2. Production Mode
```bash
# Uses Let's Encrypt certificates
./docker-deploy.sh yourdomain.com admin@domain.com production

# Or manually with profiles:
docker-compose --profile database --profile cache --profile monitoring up -d
```

**Features:**
- Real SSL certificates via Let's Encrypt
- Full service stack (DB, Redis, Monitoring)
- Optimized for performance
- Automatic certificate renewal

## 📋 Prerequisites

### Development
- Docker & Docker Compose
- Domain pointing to your server (for production)
- Ports 80, 443, 8443 available

### Production
- VPS/Cloud server with public IP
- Domain name with DNS configured
- Firewall configured for HTTP/HTTPS traffic

## 🔒 SSL Certificate Management

### Development (Localhost)
```bash
# Self-signed certificates are automatically copied from backend/certs/
# No additional setup required
```

### Production (Real Domain)
```bash
# Automatic Let's Encrypt certificates
docker-compose --profile ssl-setup run --rm certbot

# Manual certificate renewal
docker-compose exec certbot certbot renew
```

## 🛠️ Service Management

### Start Services
```bash
# Basic services
docker-compose up -d

# With database
docker-compose --profile database up -d

# Full production stack
docker-compose --profile database --profile cache --profile monitoring up -d
```

### Stop Services
```bash
docker-compose down

# Remove volumes (WARNING: Deletes data)
docker-compose down -v
```

### View Logs
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f backend
docker-compose logs -f frontend
```

### Health Checks
```bash
# Check service status
docker-compose ps

# Test endpoints
curl -k https://localhost/health
curl -k https://localhost/rooms
```

## 🔍 Troubleshooting

### Common Issues

#### Certificate Errors
```bash
# Check SSL certificate
openssl x509 -in ssl/fullchain.pem -text -noout

# Regenerate development certificates
cd backend/scripts && ./generate-certs.sh
```

#### Service Won't Start
```bash
# Check logs
docker-compose logs backend
docker-compose logs frontend

# Check resource usage
docker stats

# Restart specific service
docker-compose restart backend
```

#### Network Issues
```bash
# Check network connectivity
docker-compose exec backend wget -O- https://localhost:8443/rooms
docker-compose exec frontend curl -I http://localhost/health
```

### Database Issues (Production)
```bash
# Connect to database
docker-compose exec database psql -U spacetrader -d spacetrader

# Check database logs
docker-compose logs database

# Reset database (WARNING: Deletes all data)
docker-compose down
docker volume rm spacetrader_db_data
docker-compose --profile database up -d
```

## 📊 Monitoring (Production)

### Prometheus Metrics
- Access: http://yourdomain.com:9090
- Metrics endpoint: https://yourdomain.com/metrics

### Log Management
```bash
# Centralized logging
docker-compose logs -f

# Export logs
docker-compose logs --no-color > app.log
```

## 🔄 Updates & Maintenance

### Update Application
```bash
# Pull latest code
git pull

# Rebuild and restart
./docker-deploy.sh yourdomain.com your-email@domain.com production
```

### Certificate Renewal
```bash
# Auto-renewal is set up via cron in certbot container
# Manual renewal:
docker-compose exec certbot certbot renew --dry-run
```

### Backup
```bash
# Backup volumes
docker run --rm -v spacetrader_db_data:/data -v $(pwd):/backup ubuntu tar czf /backup/db_backup.tar.gz /data

# Backup SSL certificates
tar czf ssl_backup.tar.gz ssl/
```

## 🌐 Access URLs

### Development
- Frontend: https://localhost
- Backend API: https://localhost/api/
- WebSocket: wss://localhost/ws
- Health Check: https://localhost/health

### Production
- Frontend: https://yourdomain.com
- Backend API: https://yourdomain.com/api/
- WebSocket: wss://yourdomain.com/ws
- Monitoring: http://yourdomain.com:9090

## 📞 Support

### Debug Commands
```bash
# Container shell access
docker-compose exec backend sh
docker-compose exec frontend sh

# Network inspection
docker network ls
docker network inspect spacetrader_app-network

# Volume inspection
docker volume ls
docker volume inspect spacetrader_ssl_certs
```

Your Sphere of Influence application is now ready for production deployment with full automation! 🎉
