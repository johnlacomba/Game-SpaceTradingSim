# HTTPS Setup Guide for Sphere of Influence

This guide explains how to set up HTTPS for the Sphere of Influence application for both development and production environments.

## 🔧 Quick Setup

### 1. Generate Development Certificates

```bash
cd backend/scripts
./generate-certs.sh
```

This creates self-signed certificates for local development.

### 2. Start Development Servers with HTTPS

**Backend:**
```bash
cd backend
go run ./cmd/server -https-port=8443
```

**Frontend:**
```bash
cd frontend
npm run dev:https
```

Your application will now be available at:
- Frontend: https://localhost:5173
- Backend API: https://localhost:8443

## 🚀 Production Deployment

### Option 1: Manual Deployment

1. **Get SSL Certificates**
   
   For production, use Let's Encrypt:
   ```bash
   sudo certbot certonly --standalone -d yourdomain.com
   ```

2. **Copy Certificates**
   ```bash
   cp /etc/letsencrypt/live/yourdomain.com/fullchain.pem backend/certs/server-san.crt
   cp /etc/letsencrypt/live/yourdomain.com/privkey.pem backend/certs/server-san.key
   ```

3. **Build and Deploy**
   ```bash
   cd frontend
   ./deploy.sh yourdomain.com
   ```

4. **Configure Nginx**
   ```bash
   cp ../nginx.conf.example /etc/nginx/sites-available/sphere-of-influence
   ln -s /etc/nginx/sites-available/sphere-of-influence /etc/nginx/sites-enabled/
   nginx -t && systemctl reload nginx
   ```

### Option 2: Docker Deployment

1. **Update docker-compose.yml** with your domain
2. **Build and start:**
   ```bash
   docker-compose up --build -d
   ```

## 🔒 Security Features

### Backend Security
- **TLS 1.2+ only** - Modern encryption standards
- **CORS headers** - Proper cross-origin resource sharing
- **Secure cipher suites** - Strong encryption algorithms
- **HTTPS redirect** - Automatic HTTP to HTTPS redirection

### Frontend Security
- **HTTPS-only mode** - All connections encrypted
- **Environment-based configuration** - Separate dev/prod settings
- **Secure WebSocket connections** - WSS protocol

## 🛠️ Development Commands

```bash
# Backend
go run ./cmd/server                           # HTTP only (fallback)
go run ./cmd/server -https-port=8443         # HTTPS + HTTP redirect
go run ./cmd/server -tls-only                # HTTPS only

# Frontend
npm run dev          # HTTPS development server
npm run build        # Production build
npm run preview      # Preview production build with HTTPS
```

## 📁 Certificate Files

```
backend/certs/
├── server.crt          # Basic certificate
├── server.key          # Basic private key
├── server-san.crt      # Certificate with Subject Alternative Names
└── server-san.key      # Private key for SAN certificate
```

## 🌐 Environment Configuration

### Development (.env)
```env
VITE_API_BASE_URL=https://localhost:8443
VITE_WS_URL=wss://localhost:8443/ws
VITE_DEV_MODE=true
```

### Production (.env.production)
```env
VITE_API_BASE_URL=https://yourdomain.com
VITE_WS_URL=wss://yourdomain.com/ws
VITE_DEV_MODE=false
```

## 🔍 Troubleshooting

### Certificate Errors
If you see certificate warnings in the browser:
1. **Development**: Click "Advanced" → "Proceed to localhost" (self-signed certs)
2. **Production**: Ensure certificates are from a trusted CA

### Connection Issues
- Check firewall settings (ports 80, 443, 8443)
- Verify DNS configuration
- Ensure certificates have correct permissions

### WebSocket Issues
- Confirm WSS (not WS) protocol in frontend
- Check proxy configuration for WebSocket upgrades

## 📋 Production Checklist

- [ ] SSL certificates installed and valid
- [ ] Domain pointing to server
- [ ] Firewall configured (ports 80, 443, 8443)
- [ ] Nginx configured and running
- [ ] Backend server running with `-tls-only`
- [ ] Frontend built for production
- [ ] Environment variables set correctly

## 🔄 Certificate Renewal

For Let's Encrypt certificates, set up automatic renewal:

```bash
# Add to crontab
0 12 * * * /usr/bin/certbot renew --quiet --post-hook "systemctl reload nginx"
```

## 📞 Support

For issues related to HTTPS setup:
1. Check server logs: `journalctl -u your-service`
2. Verify certificate validity: `openssl x509 -in cert.pem -text -noout`
3. Test SSL configuration: `curl -I https://yourdomain.com`
