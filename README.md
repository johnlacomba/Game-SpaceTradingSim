# Space Trader

Server-authoritative multiplayer space trading sim.

## Stack
- Backend: Go, Gorilla WebSocket, Gorilla Mux
- Frontend: React + Vite

## Run backend
```
cd backend
go mod tidy
go run ./cmd/server
```

## Run frontend
```
cd frontend
npm install
npm run dev
```

Then open the frontend and click Connect to reach the lobby.
