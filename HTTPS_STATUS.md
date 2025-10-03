# Sphere of Influence HTTPS Configuration

## Current Status
✅ **HTTPS Setup Complete!**

Your application is now configured for secure HTTPS communication:

### Development URLs (Self-signed certificates)
- **Frontend**: https://localhost:5173
- **Backend API**: https://localhost:8443
- **WebSocket**: wss://localhost:8443/ws

### Production Setup
For production deployment, replace the self-signed certificates with certificates from a trusted Certificate Authority.

## Files Modified

### Backend
- `cmd/server/main.go` - Added HTTPS server with TLS configuration
- `certs/` - Contains SSL certificates
- `scripts/generate-certs.sh` - Certificate generation script
- `Dockerfile` - Docker container configuration

### Frontend  
- `vite.config.ts` - HTTPS development server configuration
- `.env` - Development environment variables
- `.env.production` - Production environment variables
- `deploy.sh` - Production deployment script

### Infrastructure
- `nginx.conf.example` - Production nginx configuration
- `docker-compose.yml` - Container orchestration
- `HTTPS_SETUP.md` - Comprehensive setup guide

## Next Steps

1. **Development**: Access https://localhost:5173 (you may need to accept the self-signed certificate warning)

2. **Production**: 
   - Get proper SSL certificates (Let's Encrypt recommended)
   - Update environment variables with your domain
   - Use the provided nginx configuration
   - Run the deployment script

## Security Features Enabled

- 🔒 **TLS 1.2+** encryption
- 🛡️ **CORS headers** configured
- 🔄 **HTTP to HTTPS** automatic redirect
- 🔌 **Secure WebSocket** connections (WSS)
- 🚫 **HTTP Strict Transport Security** headers
- 🛡️ **Additional security headers** (XSS protection, etc.)

Your Sphere of Influence application is now ready for secure internet hosting!
