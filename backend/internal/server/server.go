package server

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type WSOut struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type PlayerID string

type Player struct {
	ID                PlayerID        `json:"id"`
	Name              string          `json:"name"`
	Money             int             `json:"money"`
	CurrentPlanet     string          `json:"currentPlanet"`
	DestinationPlanet string          `json:"destinationPlanet"`
	Inventory         map[string]int  `json:"inventory"`
	InventoryAvgCost  map[string]int  `json:"inventoryAvgCost"`
	conn              *websocket.Conn // not serialized
	roomID            string          // not serialized
}

type Room struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Started bool                 `json:"started"`
	Players map[PlayerID]*Player `json:"players"`
	Tick    int                  `json:"tick"`
	Planets map[string]*Planet   `json:"planets"`
	mu      sync.Mutex
}

type Planet struct {
	Name   string         `json:"name"`
	Goods  map[string]int `json:"goods"`
	Prices map[string]int `json:"prices"`
	// Prod is per-tick production for goods at this location (server-only)
	Prod map[string]int `json:"-"`
}

type GameServer struct {
	rooms    map[string]*Room
	roomsMu  sync.RWMutex
	upgrader websocket.Upgrader
}

func NewGameServer() *GameServer {
	gs := &GameServer{
		rooms: make(map[string]*Room),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	return gs
}

// HTTP handlers
func (gs *GameServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := gs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	// Create a transient player connection until they identify
	p := &Player{ID: PlayerID(randID()), Name: "", Money: 1000, CurrentPlanet: "Earth", DestinationPlanet: "", Inventory: map[string]int{}, InventoryAvgCost: map[string]int{}}
	p.conn = conn
	go gs.readLoop(p)
}

func (gs *GameServer) HandleListRooms(w http.ResponseWriter, r *http.Request) {
	gs.roomsMu.RLock()
	defer gs.roomsMu.RUnlock()
	resp := []map[string]interface{}{}
	for _, room := range gs.rooms {
		room.mu.Lock()
		resp = append(resp, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"started":     room.Started,
			"playerCount": len(room.Players),
		})
		room.mu.Unlock()
	}
	// sort for stable output
	sort.Slice(resp, func(i, j int) bool { return resp[i]["id"].(string) < resp[j]["id"].(string) })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (gs *GameServer) HandleCreateRoom(w http.ResponseWriter, r *http.Request) {
	room := gs.createRoom("")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": room.ID, "name": room.Name})
}

// WebSocket read loop
func (gs *GameServer) readLoop(p *Player) {
	defer func() {
		if p.conn != nil {
			p.conn.Close()
		}
		// remove from room if any
		if p.roomID != "" {
			gs.roomsMu.RLock()
			room := gs.rooms[p.roomID]
			gs.roomsMu.RUnlock()
			if room != nil {
				room.mu.Lock()
				delete(room.Players, p.ID)
				room.mu.Unlock()
				gs.broadcastRoom(room)
			}
		}
	}()

	for {
		var msg Message
		if err := p.conn.ReadJSON(&msg); err != nil {
			log.Println("read:", err)
			return
		}
		switch msg.Type {
		case "connect":
			// payload: {name}
			var data struct {
				Name string `json:"name"`
			}
			json.Unmarshal(msg.Payload, &data)
			p.Name = defaultStr(data.Name, "Player "+string(p.ID[len(p.ID)-4:]))
			// send lobby state
			gs.sendLobbyState(p)
		case "listRooms":
			gs.sendLobbyState(p)
		case "createRoom":
			room := gs.createRoom("")
			gs.joinRoom(p, room.ID)
		case "joinRoom":
			var data struct {
				RoomID string `json:"roomId"`
			}
			json.Unmarshal(msg.Payload, &data)
			if data.RoomID != "" {
				gs.joinRoom(p, data.RoomID)
			}
		case "startGame":
			if p.roomID != "" {
				gs.startGame(p.roomID)
			}
		case "selectPlanet":
			var data struct {
				Planet string `json:"planet"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				p.DestinationPlanet = data.Planet
				room.mu.Unlock()
				gs.sendRoomState(room, nil)
			}
		case "buy":
			var data struct {
				Good   string `json:"good"`
				Amount int    `json:"amount"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				gs.handleBuy(room, p, data.Good, data.Amount)
			}
		case "sell":
			var data struct {
				Good   string `json:"good"`
				Amount int    `json:"amount"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				gs.handleSell(room, p, data.Good, data.Amount)
			}
		}
	}
}

func (gs *GameServer) getRoom(id string) *Room {
	if id == "" {
		return nil
	}
	gs.roomsMu.RLock()
	defer gs.roomsMu.RUnlock()
	return gs.rooms[id]
}

func (gs *GameServer) sendLobbyState(p *Player) {
	gs.roomsMu.RLock()
	resp := []map[string]interface{}{}
	for _, room := range gs.rooms {
		room.mu.Lock()
		resp = append(resp, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"started":     room.Started,
			"playerCount": len(room.Players),
		})
		room.mu.Unlock()
	}
	gs.roomsMu.RUnlock()
	p.conn.WriteJSON(WSOut{Type: "lobbyState", Payload: map[string]interface{}{"rooms": resp}})
}

func (gs *GameServer) createRoom(name string) *Room {
	if name == "" {
		name = "Room " + randID()[0:4]
	}
	room := &Room{
		ID:      randID(),
		Name:    name,
		Players: map[PlayerID]*Player{},
		Planets: defaultPlanets(),
	}
	gs.roomsMu.Lock()
	gs.rooms[room.ID] = room
	gs.roomsMu.Unlock()
	return room
}

func (gs *GameServer) joinRoom(p *Player, roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}
	// remove from old room
	if p.roomID != "" && p.roomID != roomID {
		if old := gs.getRoom(p.roomID); old != nil {
			old.mu.Lock()
			delete(old.Players, p.ID)
			old.mu.Unlock()
			gs.broadcastRoom(old)
		}
	}
	room.mu.Lock()
	p.roomID = room.ID
	p.CurrentPlanet = "Earth"
	p.DestinationPlanet = ""
	if p.Inventory == nil {
		p.Inventory = map[string]int{}
	}
	if p.InventoryAvgCost == nil {
		p.InventoryAvgCost = map[string]int{}
	}
	room.Players[p.ID] = p
	room.mu.Unlock()
	gs.sendRoomState(room, p)
	gs.broadcastRoom(room)
}

func (gs *GameServer) startGame(roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	if !room.Started {
		room.Started = true
		room.Tick = 0
		go gs.runTicker(room)
	}
	room.mu.Unlock()
	gs.broadcastRoom(room)
}

func (gs *GameServer) runTicker(room *Room) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		room.mu.Lock()
		if !room.Started {
			room.mu.Unlock()
			return
		}
		room.Tick++
		// resolve travel
		for _, p := range room.Players {
			if p.DestinationPlanet != "" && p.DestinationPlanet != p.CurrentPlanet {
				if _, ok := room.Planets[p.DestinationPlanet]; ok {
					p.CurrentPlanet = p.DestinationPlanet
				}
				p.DestinationPlanet = ""
			}
		}
		// accumulate per-planet production
		for _, pl := range room.Planets {
			for g, amt := range pl.Prod {
				if amt <= 0 {
					continue
				}
				pl.Goods[g] = pl.Goods[g] + amt
			}
		}
		room.mu.Unlock()
		gs.broadcastRoom(room)
		<-ticker.C
	}
}

func (gs *GameServer) handleBuy(room *Room, p *Player, good string, amount int) {
	if amount <= 0 || good == "" {
		return
	}
	room.mu.Lock()
	defer func() { room.mu.Unlock(); gs.sendRoomState(room, nil) }()
	planet := room.Planets[p.CurrentPlanet]
	if planet == nil {
		return
	}
	available := planet.Goods[good]
	price := planet.Prices[good]
	if price <= 0 {
		return
	}
	// max you can buy
	maxByMoney := p.Money / price
	if amount > maxByMoney {
		amount = maxByMoney
	}
	if amount > available {
		amount = available
	}
	if amount <= 0 {
		return
	}
	cost := amount * price
	p.Money -= cost
	planet.Goods[good] -= amount
	// update quantity and weighted average cost
	oldQty := p.Inventory[good]
	oldAvg := p.InventoryAvgCost[good]
	newQty := oldQty + amount
	p.Inventory[good] = newQty
	if newQty > 0 {
		newAvg := (oldQty*oldAvg + amount*price) / newQty
		p.InventoryAvgCost[good] = newAvg
	} else {
		delete(p.InventoryAvgCost, good)
	}
}

func (gs *GameServer) handleSell(room *Room, p *Player, good string, amount int) {
	if amount <= 0 || good == "" {
		return
	}
	room.mu.Lock()
	defer func() { room.mu.Unlock(); gs.sendRoomState(room, nil) }()
	planet := room.Planets[p.CurrentPlanet]
	if planet == nil {
		return
	}
	price := planet.Prices[good]
	if price <= 0 {
		return
	}
	owned := p.Inventory[good]
	if amount > owned {
		amount = owned
	}
	if amount <= 0 {
		return
	}
	p.Inventory[good] -= amount
	planet.Goods[good] += amount
	p.Money += amount * price
	if p.Inventory[good] == 0 {
		delete(p.InventoryAvgCost, good)
	}
}

func (gs *GameServer) sendRoomState(room *Room, only *Player) {
	// prepare minimal view per-player (fog of goods for current planet only)
	room.mu.Lock()
	players := []map[string]interface{}{}
	for _, pp := range room.Players {
		players = append(players, map[string]interface{}{
			"id":                pp.ID,
			"name":              pp.Name,
			"money":             pp.Money,
			"currentPlanet":     pp.CurrentPlanet,
			"destinationPlanet": pp.DestinationPlanet,
		})
	}
	payloadByPlayer := map[PlayerID]interface{}{}
	for id, pp := range room.Players {
		planet := room.Planets[pp.CurrentPlanet]
		visible := map[string]interface{}{}
		if planet != nil {
			// copy goods so we can add zero-stock for player's inventory
			visGoods := map[string]int{}
			for k, v := range planet.Goods {
				visGoods[k] = v
			}
			for g := range pp.Inventory {
				if _, ok := visGoods[g]; !ok {
					visGoods[g] = 0
				}
			}
			visible = map[string]interface{}{
				"name":   planet.Name,
				"goods":  visGoods,
				"prices": planet.Prices,
			}
		}
		payloadByPlayer[id] = map[string]interface{}{
			"room": map[string]interface{}{
				"id":      room.ID,
				"name":    room.Name,
				"started": room.Started,
				"tick":    room.Tick,
				"players": players,
				"planets": planetNames(room.Planets),
			},
			"you": map[string]interface{}{
				"id":                pp.ID,
				"name":              pp.Name,
				"money":             pp.Money,
				"inventory":         pp.Inventory,
				"inventoryAvgCost":  pp.InventoryAvgCost,
				"currentPlanet":     pp.CurrentPlanet,
				"destinationPlanet": pp.DestinationPlanet,
			},
			"visiblePlanet": visible,
		}
	}
	room.mu.Unlock()

	if only != nil {
		only.conn.WriteJSON(WSOut{Type: "roomState", Payload: payloadByPlayer[only.ID]})
		return
	}
	for id, pp := range room.Players {
		pp.conn.WriteJSON(WSOut{Type: "roomState", Payload: payloadByPlayer[id]})
	}
}

func (gs *GameServer) broadcastRoom(room *Room) { gs.sendRoomState(room, nil) }

func planetNames(m map[string]*Planet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func defaultPlanets() map[string]*Planet {
	rand.Seed(time.Now().UnixNano())
	// All 8 planets + a few stations
	names := []string{"Mercury", "Venus", "Earth", "Mars", "Jupiter", "Saturn", "Uranus", "Neptune", "Pluto Station", "Titan Station", "Ceres Station"}
	// Standard goods produced broadly
	standard := []string{"Food", "Ore", "Water", "Fuel"}
	// Unique per-location goods
	uniqueByLoc := map[string][]string{
		"Mercury":       {"Solar Panels"},
		"Venus":         {"Acid Extract"},
		"Earth":         {"Electronics"},
		"Mars":          {"Iron Alloy"},
		"Jupiter":       {"Helium-3"},
		"Saturn":        {"Methane"},
		"Uranus":        {"Ice Crystals"},
		"Neptune":       {"Deep Blue Dye"},
		"Pluto Station": {"Xenon Gas"},
		"Titan Station": {"Titan Spice"},
		"Ceres Station": {"Rare Metals"},
	}
	// Union of all goods for global pricing presence
	allGoodsSet := map[string]struct{}{}
	for _, g := range standard {
		allGoodsSet[g] = struct{}{}
	}
	for _, arr := range uniqueByLoc {
		for _, g := range arr {
			allGoodsSet[g] = struct{}{}
		}
	}
	allGoods := make([]string, 0, len(allGoodsSet))
	for g := range allGoodsSet {
		allGoods = append(allGoods, g)
	}
	sort.Strings(allGoods)

	m := map[string]*Planet{}
	for _, n := range names {
		goods := map[string]int{}
		prices := map[string]int{}
		prod := map[string]int{}
		for _, g := range standard {
			goods[g] = 20 + rand.Intn(30)
			prices[g] = 5 + rand.Intn(20)
			prod[g] = 2 + rand.Intn(4) // 2-5 per tick
		}
		for _, g := range uniqueByLoc[n] {
			goods[g] = 10 + rand.Intn(20)
			prod[g] = 1 + rand.Intn(3) // 1-3 per tick
		}
		// ensure price exists for every good to allow selling anywhere
		for _, g := range allGoods {
			if _, ok := prices[g]; !ok {
				prices[g] = 8 + rand.Intn(25)
			}
		}
		m[n] = &Planet{Name: n, Goods: goods, Prices: prices, Prod: prod}
	}
	return m
}

func randID() string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	b := make([]rune, 8)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
