# Sphere of Influence Backend (Go)

Server-authoritative strategic empire server using WebSockets.

## Features
- Lobby with active game rooms
- Create/join rooms
- Per-room Start Game
- 10s global tick per room
- Server tracks: money, current planet, destination planet, ship inventory
- Server validates buy/sell and travel on tick

## Run
1. Install Go 1.22+
2. Install deps

```
cd backend
go mod tidy
```

3. Run server
```
go run ./cmd/server
```

Server runs on :8080 by default.

## API
- WebSocket: `/ws`
- REST (debug):
  - `GET /rooms`
  - `POST /rooms`

Messages are JSON with `type` and `payload`.
