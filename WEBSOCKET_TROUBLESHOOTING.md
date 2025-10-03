# WebSocket Connection Troubleshooting Guide

If you're seeing "DOMException: The operation is insecure" errors when clicking Connect, here's how to fix them:

## 🔧 Quick Fix Steps

### 1. Accept Self-Signed Certificates

Since we're using self-signed certificates for development, your browser needs to accept them:

**For the Backend API:**
1. Open a new tab and go to: https://localhost:8443/rooms
2. You'll see a certificate warning
3. Click "Advanced" → "Proceed to localhost (unsafe)"
4. You should see a JSON response like: `{"rooms":[]}`

**For the Frontend:**
1. Your main app should be at: https://localhost:5173
2. If you see a certificate warning, accept it the same way

### 2. Check Both Servers Are Running

Run this command to verify:
```bash
./dev-setup.sh
```

Or manually check:
- Backend: https://localhost:8443/rooms
- Frontend: https://localhost:5173

### 3. Clear Browser Cache

Sometimes browsers cache certificate rejections:
1. Open Developer Tools (F12)
2. Right-click refresh button → "Empty Cache and Hard Reload"
3. Or use Incognito/Private mode

### 4. Check Browser Console

1. Open Developer Tools (F12)
2. Go to Console tab
3. Look for specific error messages about certificates or WebSocket connections

## 🛠️ Alternative Solutions

### Option 1: HTTP Fallback for Testing

If certificates are causing too much trouble, you can temporarily run in HTTP mode:

**Backend (HTTP only):**
```bash
cd backend
go run ./cmd/server -http-port=8080
```

**Frontend (HTTP only):**
Update `frontend/.env` to:
```
VITE_WS_URL=ws://localhost:8080/ws
```

Then restart: `npm run dev`

### Option 2: Use Different Browser

Try a different browser or incognito mode to isolate certificate caching issues.

## 🔍 Common Error Messages

### "DOMException: The operation is insecure"
- **Cause:** Mixed content (HTTPS page trying to connect to WS instead of WSS)
- **Fix:** Ensure frontend is using `wss://` protocol for WebSocket connections

### "WebSocket connection failed"
- **Cause:** Backend server not running or certificate not accepted
- **Fix:** Visit https://localhost:8443/rooms and accept certificate

### "Connection closed: Unknown reason"
- **Cause:** Certificate validation failed during WebSocket handshake
- **Fix:** Accept backend certificate first, then try connecting

## 📋 Verification Checklist

- [ ] Backend running on https://localhost:8443
- [ ] Frontend running on https://localhost:5173
- [ ] Visited https://localhost:8443/rooms and accepted certificate
- [ ] Browser console shows WebSocket attempting `wss://localhost:8443/ws`
- [ ] No mixed content warnings in browser console

## 🆘 Still Having Issues?

1. Check the browser's Network tab to see if WebSocket connection attempts are being made
2. Look for any CORS or certificate errors in the console
3. Try connecting directly to the WebSocket using a tool like: https://websocketking.com
4. Ensure no firewall or antivirus is blocking the connections

## 🎯 Success Indicators

When everything is working correctly, you should see:
1. No certificate warnings
2. "Connect" button changes the app state
3. Console shows: "WebSocket connected successfully"
4. App moves from title screen to lobby

Your Sphere of Influence application should now work with secure HTTPS/WSS connections!
