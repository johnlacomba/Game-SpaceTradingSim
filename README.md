# Space Trader

Server-authoritative multiplayer space trading sim.

## Stack
- Backend: Go, Gorilla WebSocket, Gorilla Mux
- Frontend: React + Vite

## Features
- Rooms: create/join from a lobby; per-room server state
- Server turns: 60-second turns; ends early if all humans Ready
- Ready flow: bots are always ready; Start is disabled until all humans are Ready
- Trading: buy/sell with inventory average cost; sell anywhere at local prices
- Planets: per-turn production; travel resolves at turn end; limited goods visibility (current planet)
- Visuals: player color dots, destination arrows, Players dropdown with Ready status
- Persistence: leaving room preserves player state for rejoin

## Build & Run
Run the backend and frontend in separate terminals.

Backend (Go):
```
cd backend
go mod tidy
go run ./cmd/server
```

Frontend (Vite):
```
cd frontend
npm install
npm run dev
```

Open http://localhost:5173 and click Connect. The client autodetects the backend at ws://<page-host>:8080/ws.

LAN: both servers bind to all interfaces; you can connect from other devices on your network using your machine’s IP.
