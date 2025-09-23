# Production Deployment Guide

## 🚀 Quick Start

### For Development (localhost)
```bash
./quick-deploy.sh localhost admin@example.com development
```

### For Production (real domain)
```bash
# Option 1: Automatic SSL setup
./setup-ssl.sh yourdomain.com your-email@domain.com
./quick-deploy.sh yourdomain.com your-email@domain.com production

# Option 2: Manual deployment
./quick-deploy.sh yourdomain.com your-email@domain.com production
```

## 🔧 What the Deployment Fixes

The original Docker Compose issue was caused by:
1. **Go version mismatch**: Updated Dockerfile to use Go 1.23
2. **Certificate path issues**: Fixed certificate mounting and paths
3. **TLS-only mode**: Made TLS configurable instead of forced
4. **Certificate generation**: Separated SSL setup from deployment
5. **Infinite hanging**: Fixed certbot container hanging with cron

## 📋 Prerequisites

### Development
- Docker & Docker Compose
- No additional setup required

### Production
- Domain name pointing to your server
- Ports 80 and 443 open and accessible
- Docker & Docker Compose installed

## 🔒 SSL Certificate Options

### 1. Development (Localhost)
- Uses existing self-signed certificates from `backend/certs/`
- Automatically generated if missing
- Browser will show certificate warnings (expected)

### 2. Production (Real Domain)
- **Option A**: Let's Encrypt (recommended)
  ```bash
  ./setup-ssl.sh yourdomain.com your-email@domain.com
  ```
- **Option B**: Bring your own certificates
  ```bash
  # Place your certificates as:
  cp your-cert.pem ssl/fullchain.pem
  cp your-key.pem ssl/privkey.pem
  ```

## 🐳 Service Architecture

```
Internet → Nginx (443) → Backend (8443)
           ↓
        Frontend (React SPA)
```

### Core Services
- **Backend**: Go server with HTTPS/WSS
- **Frontend**: React app served by Nginx
- **Nginx**: Reverse proxy with SSL termination

### Optional Services (Production)
- **Database**: PostgreSQL (`--profile database`)
- **Cache**: Redis (`--profile cache`)
- **Monitoring**: Prometheus (`--profile monitoring`)

## 📊 Deployment Commands

### Basic Deployment
```bash
# Development
docker-compose up -d

# Production with database
docker-compose --profile database up -d

# Full production stack
docker-compose --profile database --profile cache --profile monitoring up -d
```

### SSL Certificate Management
```bash
# Get new certificates
./setup-ssl.sh yourdomain.com admin@domain.com

# Renew certificates (production)
docker-compose --profile ssl-renew up -d

# Manual renewal
docker-compose --profile ssl-setup run --rm certbot
```

### Troubleshooting
```bash
# Check service status
docker-compose ps

# View logs
docker-compose logs backend
docker-compose logs frontend

# Restart services
docker-compose restart backend

# Full rebuild
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

## 🔍 Health Checks

The deployment includes automatic health checks:
- **Backend**: `https://localhost:8443/rooms`
- **Frontend**: `http://localhost/health`
- **Database**: PostgreSQL connection test
- **Redis**: Redis ping test

## 🌐 Access URLs

### Development
- Frontend: https://localhost
- Backend API: https://localhost:8443
- WebSocket: wss://localhost:8443/ws

### Production
- Frontend: https://yourdomain.com
- Backend API: https://yourdomain.com/api/
- WebSocket: wss://yourdomain.com/ws
- Monitoring: http://yourdomain.com:9090 (if enabled)

## 🔄 Updates & Maintenance

### Application Updates
```bash
git pull
./quick-deploy.sh yourdomain.com your-email@domain.com production
```

### Certificate Renewal
Automatic renewal is set up via cron when using `--profile ssl-renew`

### Backup
```bash
# Backup data volumes
docker run --rm -v game-spacetradingsim_db_data:/data -v $(pwd):/backup ubuntu tar czf /backup/backup.tar.gz /data

# Backup SSL certificates
tar czf ssl-backup.tar.gz ssl/
```

## ⚠️ Important Notes

1. **Certificate Warnings**: Development mode uses self-signed certificates
2. **Port Access**: Production requires ports 80 and 443 to be publicly accessible
3. **Domain Setup**: Ensure DNS points to your server before SSL setup
4. **Firewall**: Configure firewall to allow HTTP/HTTPS traffic
5. **Resource Limits**: Default limits are set for small VPS instances

## 🆘 Common Issues

### "Certificate not found"
- Run `./setup-ssl.sh yourdomain.com` first
- Or place certificates in `ssl/` directory

### "Port already in use"
- Stop other web servers: `sudo systemctl stop apache2 nginx`
- Or change ports in docker-compose.yml

### "Domain not accessible"
- Check DNS configuration
- Verify firewall settings
- Ensure no other services on ports 80/443

Your Space Trader application is now production-ready! 🎉
