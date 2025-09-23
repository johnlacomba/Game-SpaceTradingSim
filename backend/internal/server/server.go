package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
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

const turnDuration = 60 * time.Second
const shipCapacity = 200 // maximum total units a ship can carry
const fuelCapacity = 100 // maximum fuel units (distance units)

type PlayerID string

type Player struct {
	ID                PlayerID        `json:"id"`
	Name              string          `json:"name"`
	Money             int             `json:"money"`
	CurrentPlanet     string          `json:"currentPlanet"`
	DestinationPlanet string          `json:"destinationPlanet"`
	Inventory         map[string]int  `json:"inventory"`
	InventoryAvgCost  map[string]int  `json:"inventoryAvgCost"`
	Ready             bool            `json:"ready"`
	EndGame           bool            `json:"endGame"`
	Modals            []ModalItem     `json:"-"`
	Fuel              int             `json:"fuel"`
	conn              *websocket.Conn // not serialized
	roomID            string          // not serialized
	IsBot             bool            `json:"-"`
	writeMu           sync.Mutex      // guards conn writes
	Bankrupt          bool            `json:"-"`
	// Transit state (server-only)
	InTransit         bool   `json:"-"`
	TransitFrom       string `json:"-"`
	TransitRemaining  int    `json:"-"` // units remaining to destination along straight line
	TransitTotal      int    `json:"-"` // initial units at start of transit
	CapacityBonus     int    `json:"-"`
	SpeedBonus        int    `json:"-"`
	FuelCapacityBonus int    `json:"-"`
	// Recent actions (last 10)
	ActionHistory []ActionLog `json:"-"`
}

// ActionLog captures a brief recent action for audit/history
type ActionLog struct {
	Turn int    `json:"turn"`
	Text string `json:"text"`
}

func (gs *GameServer) logAction(room *Room, p *Player, text string) {
	if room == nil || p == nil {
		return
	}
	entry := ActionLog{Turn: room.Turn, Text: text}
	p.ActionHistory = append(p.ActionHistory, entry)
	if len(p.ActionHistory) > 100 {
		p.ActionHistory = p.ActionHistory[len(p.ActionHistory)-100:]
	}
}

type Room struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Host          string                    `json:"host"`
	Players       map[string]Player         `json:"players"`
	MaxPlayers    int                       `json:"maxPlayers"`
	Started       bool                      `json:"started"`
	Turn          int                       `json:"turn"`
	MaxTurns      int                       `json:"maxTurns"`
	TimeRemaining int                       `json:"timeRemaining"`
	Systems       map[string]*System        `json:"systems"`
	Prices        map[string]map[string]int `json:"prices"`
	Events        []Event                   `json:"events"`
	stopChan      chan struct{}             // Channel to stop the game loop
	loopRunning   bool                      // Track if a loop is running
}

// ModalItem represents a queued modal to show to a specific player
type ModalItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Optional metadata for actionable modals (e.g., upgrade offers)
	Kind              string `json:"kind,omitempty"`
	Price             int    `json:"price,omitempty"`
	CapacityBonus     int    `json:"capacityBonus,omitempty"`
	PricePerUnit      int    `json:"pricePerUnit,omitempty"`
	Units             int    `json:"units,omitempty"`
	SpeedBonus        int    `json:"speedBonus,omitempty"`
	FuelCapacityBonus int    `json:"fuelCapacityBonus,omitempty"`
}

// NewsItem represents a temporary room-wide event affecting a planet's prices/production
type NewsItem struct {
	Headline       string         `json:"headline"`
	Planet         string         `json:"planet"`
	PriceDelta     map[string]int `json:"priceDelta,omitempty"`
	ProdDelta      map[string]int `json:"prodDelta,omitempty"`
	TurnsRemaining int            `json:"turnsRemaining"`
	FuelPriceDelta int            `json:"-"`
}
type Planet struct {
	Name   string         `json:"name"`
	Goods  map[string]int `json:"goods"`
	Prices map[string]int `json:"prices"`
	// Prod is per-turn production for goods at this location (server-only)
	// Prod is per-turn production for goods at this location (server-only)
	Prod map[string]int `json:"-"`
	// Baselines for recalculating each turn with news effects
	BasePrices map[string]int `json:"-"`
	BaseProd   map[string]int `json:"-"`
	// Persistent per-good price trend (small drift applied each turn)
	PriceTrend map[string]int `json:"-"`
	// Separate ship fuel price (not a trade good)
	FuelPrice     int `json:"-"`
	BaseFuelPrice int `json:"-"`
}

// PersistedPlayer stores the subset of player state we want to keep per-room for rejoin
type PersistedPlayer struct {
	Money             int
	CurrentPlanet     string
	DestinationPlanet string
	Inventory         map[string]int
	InventoryAvgCost  map[string]int
	Ready             bool
	EndGame           bool
	Modals            []ModalItem
	Fuel              int
	Bankrupt          bool
	InTransit         bool
	TransitFrom       string
	TransitRemaining  int
	TransitTotal      int
	CapacityBonus     int
	SpeedBonus        int
	FuelCapacityBonus int
	ActionHistory     []ActionLog
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
	p := &Player{ID: PlayerID(randID()), Name: "", Money: 1000, CurrentPlanet: "Earth", DestinationPlanet: "", Inventory: map[string]int{}, InventoryAvgCost: map[string]int{}, Fuel: fuelCapacity}
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
			"turn":        room.Turn,
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
			p.writeMu.Lock()
			p.conn.Close()
			p.writeMu.Unlock()
		}
		// remove from room if any
		if p.roomID != "" {
			gs.roomsMu.RLock()
			room := gs.rooms[p.roomID]
			gs.roomsMu.RUnlock()
			if room != nil {
				room.mu.Lock()
				// persist on disconnect
				room.Persist[p.ID] = &PersistedPlayer{
					Money:             p.Money,
					CurrentPlanet:     p.CurrentPlanet,
					DestinationPlanet: p.DestinationPlanet,
					Inventory:         cloneIntMap(p.Inventory),
					InventoryAvgCost:  cloneIntMap(p.InventoryAvgCost),
					Ready:             p.Ready,
					EndGame:           p.EndGame,
					Modals:            cloneModals(p.Modals),
					Fuel:              p.Fuel,
					Bankrupt:          p.Bankrupt,
					InTransit:         p.InTransit,
					TransitFrom:       p.TransitFrom,
					TransitRemaining:  p.TransitRemaining,
					TransitTotal:      p.TransitTotal,
					CapacityBonus:     p.CapacityBonus,
					SpeedBonus:        p.SpeedBonus,
					FuelCapacityBonus: p.FuelCapacityBonus,
					ActionHistory:     cloneActionHistory(p.ActionHistory),
				}
				delete(room.Players, p.ID)
				// If no humans remain, signal the ticker to end the current turn early
				if room.Started {
					hasHuman := false
					for _, pl := range room.Players {
						if !pl.IsBot {
							hasHuman = true
							break
						}
					}
					if !hasHuman {
						select {
						case room.readyCh <- struct{}{}:
						default:
						}
					}
				}
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
		case "ackModal":
			// payload: { id }
			var data struct {
				ID string `json:"id"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				if len(p.Modals) > 0 && (data.ID == "" || p.Modals[0].ID == data.ID) {
					// pop first
					p.Modals = append([]ModalItem(nil), p.Modals[1:]...)
				}
				room.mu.Unlock()
				gs.sendRoomState(room, p)
			}
		case "respondModal":
			// payload: { id, accept }
			var data struct {
				ID     string `json:"id"`
				Accept bool   `json:"accept"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				if len(p.Modals) > 0 && p.Modals[0].ID == data.ID {
					m := p.Modals[0]
					// Remove modal
					p.Modals = append([]ModalItem(nil), p.Modals[1:]...)
					if m.Kind == "upgrade-offer" && data.Accept {
						if p.Money >= m.Price {
							p.Money -= m.Price
							p.CapacityBonus += m.CapacityBonus
							// Confirm
							gs.enqueueModal(p, "Upgrade Installed", "Your cargo capacity increased by "+strconv.Itoa(m.CapacityBonus)+" to "+strconv.Itoa(shipCapacity+p.CapacityBonus)+".")
							gs.logAction(room, p, fmt.Sprintf("Purchased cargo upgrade +%d for $%d", m.CapacityBonus, m.Price))
						} else {
							gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits for this upgrade.")
						}
					}
					if m.Kind == "speed-offer" && data.Accept {
						price := m.PricePerUnit * m.Units
						if p.Money >= price {
							p.Money -= price
							p.SpeedBonus += m.Units
							gs.enqueueModal(p, "Engine Upgrade Installed", "Your ship speed increased by "+strconv.Itoa(m.Units)+" units/turn.")
							gs.logAction(room, p, fmt.Sprintf("Purchased engine upgrade +%d for $%d", m.Units, price))
						} else {
							gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits for this upgrade.")
						}
					}
					if m.Kind == "fuelcap-offer" && data.Accept {
						price := m.PricePerUnit * m.Units
						if p.Money >= price {
							p.Money -= price
							p.FuelCapacityBonus += m.Units
							gs.enqueueModal(p, "Fuel Tank Expanded", "Your fuel capacity increased by "+strconv.Itoa(m.Units)+" to "+strconv.Itoa(fuelCapacity+p.FuelCapacityBonus)+".")
							gs.logAction(room, p, fmt.Sprintf("Purchased fuel tank +%d for $%d", m.Units, price))
						} else {
							gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits for this upgrade.")
						}
					}
				}
				room.mu.Unlock()
				gs.sendRoomState(room, p)
			}
		case "startGame":
			if p.roomID != "" {
				gs.startGame(p.roomID)
			}
		case "addBot":
			if p.roomID != "" {
				gs.addBot(p.roomID)
			}
		case "selectPlanet":
			var data struct {
				Planet string `json:"planet"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				allow := true
				if p.Bankrupt {
					allow = false
				}
				if p.InTransit {
					allow = false
					gs.enqueueModal(p, "In Transit", "You are still in transit towards "+defaultStr(p.DestinationPlanet, "your destination")+".")
				}
				if len(room.PlanetPositions) > 0 && data.Planet != "" && data.Planet != p.CurrentPlanet {
					cost := distanceUnits(room, p.CurrentPlanet, data.Planet)
					if cost > p.Fuel {
						allow = false
						gs.enqueueModal(p, "Insufficient Fuel", "You don't have enough fuel to reach "+data.Planet+".")
					}
				}
				if allow {
					p.DestinationPlanet = data.Planet
					if data.Planet != "" && data.Planet != p.CurrentPlanet {
						units := distanceUnits(room, p.CurrentPlanet, data.Planet)
						gs.logAction(room, p, fmt.Sprintf("Traveling to %s (%d units)", data.Planet, units))
					}
				}
				room.mu.Unlock()
				if allow {
					gs.sendRoomState(room, nil)
				} else {
					gs.sendRoomState(room, p)
				}
			}
		case "buy":
			var data struct {
				Good   string `json:"good"`
				Amount int    `json:"amount"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				if p.Bankrupt {
					gs.sendRoomState(room, p)
					break
				}
				if p.InTransit {
					gs.enqueueModal(p, "In Transit", "You are still in transit towards "+defaultStr(p.DestinationPlanet, "your destination")+".")
					gs.sendRoomState(room, p)
					break
				}
				gs.handleBuy(room, p, data.Good, data.Amount)
			}
		case "sell":
			var data struct {
				Good   string `json:"good"`
				Amount int    `json:"amount"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				if p.Bankrupt {
					gs.sendRoomState(room, p)
					break
				}
				if p.InTransit {
					gs.enqueueModal(p, "In Transit", "You are still in transit towards "+defaultStr(p.DestinationPlanet, "your destination")+".")
					gs.sendRoomState(room, p)
					break
				}
				gs.handleSell(room, p, data.Good, data.Amount)
			}
		case "getPlayer":
			// payload: { playerId }
			var data struct {
				PlayerID string `json:"playerId"`
			}
			json.Unmarshal(msg.Payload, &data)
			if data.PlayerID == "" {
				break
			}
			room := gs.getRoom(p.roomID)
			if room == nil {
				break
			}
			room.mu.Lock()
			target := room.Players[PlayerID(data.PlayerID)]
			var payload interface{}
			if target != nil {
				inv := cloneIntMap(target.Inventory)
				avg := cloneIntMap(target.InventoryAvgCost)
				used := inventoryTotal(inv)
				payload = map[string]interface{}{
					"id":               target.ID,
					"name":             target.Name,
					"inventory":        inv,
					"inventoryAvgCost": avg,
					"usedSlots":        used,
					"capacity":         shipCapacity + target.CapacityBonus,
					"history": func() []map[string]interface{} {
						out := make([]map[string]interface{}, 0, len(target.ActionHistory))
						for _, h := range target.ActionHistory {
							out = append(out, map[string]interface{}{"turn": h.Turn, "text": h.Text})
						}
						return out
					}(),
				}
			}
			room.mu.Unlock()
			if payload != nil && p.conn != nil {
				p.writeMu.Lock()
				p.conn.WriteJSON(WSOut{Type: "playerInfo", Payload: payload})
				p.writeMu.Unlock()
			}
		case "setReady":
			var data struct {
				Ready bool `json:"ready"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				p.Ready = data.Ready
				// if game started and now all humans are ready, signal early turn end
				allReady := room.Started
				if allReady {
					for _, pl := range room.Players {
						if pl.IsBot {
							continue
						}
						if !pl.Ready {
							allReady = false
							break
						}
					}
				}
				if allReady {
					select {
					case room.readyCh <- struct{}{}:
					default:
					}
				}
				room.mu.Unlock()
				gs.broadcastRoom(room)
			}
		case "setEndGame":
			var data struct {
				EndGame bool `json:"endGame"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				room.mu.Lock()
				p.EndGame = data.EndGame
				// Check if all human players have toggled End Game
				allEnd := true
				for _, pl := range room.Players {
					if pl.IsBot || pl.Bankrupt {
						continue
					}
					if !pl.EndGame {
						allEnd = false
						break
					}
				}
				room.mu.Unlock()
				if allEnd {
					gs.closeRoom(room.ID)
				} else {
					gs.broadcastRoom(room)
				}
			}
		case "exitRoom":
			if p.roomID != "" {
				gs.exitRoom(p)
			}
		case "refuel":
			var data struct {
				Amount int `json:"amount"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				if p.Bankrupt {
					gs.sendRoomState(room, p)
					break
				}
				if p.InTransit {
					gs.enqueueModal(p, "In Transit", "You are still in transit towards "+defaultStr(p.DestinationPlanet, "your destination")+".")
					gs.sendRoomState(room, p)
					break
				}
				gs.handleRefuel(room, p, data.Amount)
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
			"turn":        room.Turn,
		})
		room.mu.Unlock()
	}
	gs.roomsMu.RUnlock()
	if p.conn != nil {
		p.writeMu.Lock()
		p.conn.WriteJSON(WSOut{Type: "lobbyState", Payload: map[string]interface{}{"rooms": resp}})
		p.writeMu.Unlock()
	}
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
		Persist: map[PlayerID]*PersistedPlayer{},
		readyCh: make(chan struct{}, 1),
	}
	// Pre-randomize planet order and positions so the map is ready before game start
	names := planetNames(room.Planets)
	for i := range names {
		j := rand.Intn(i + 1)
		names[i], names[j] = names[j], names[i]
	}
	room.PlanetOrder = names
	room.PlanetPositions = generatePlanetPositions(names)
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
			// Persist snapshot so rejoining the old room restores progress
			old.Persist[p.ID] = &PersistedPlayer{
				Money:             p.Money,
				CurrentPlanet:     p.CurrentPlanet,
				DestinationPlanet: p.DestinationPlanet,
				Inventory:         cloneIntMap(p.Inventory),
				InventoryAvgCost:  cloneIntMap(p.InventoryAvgCost),
				Ready:             p.Ready,
				Modals:            cloneModals(p.Modals),
				Fuel:              p.Fuel,
				Bankrupt:          p.Bankrupt,
				InTransit:         p.InTransit,
				TransitFrom:       p.TransitFrom,
				TransitRemaining:  p.TransitRemaining,
				TransitTotal:      p.TransitTotal,
				CapacityBonus:     p.CapacityBonus,
				SpeedBonus:        p.SpeedBonus,
				FuelCapacityBonus: p.FuelCapacityBonus,
				ActionHistory:     cloneActionHistory(p.ActionHistory),
			}
			delete(old.Players, p.ID)
			old.mu.Unlock()
			gs.broadcastRoom(old)
		}
	}
	room.mu.Lock()
	p.roomID = room.ID
	// restore from persistence if available, else initialize defaults
	if snap, ok := room.Persist[p.ID]; ok && snap != nil {
		p.Money = snap.Money
		p.CurrentPlanet = defaultStr(snap.CurrentPlanet, "Earth")
		p.DestinationPlanet = snap.DestinationPlanet
		if snap.Inventory != nil {
			p.Inventory = cloneIntMap(snap.Inventory)
		}
		if snap.InventoryAvgCost != nil {
			p.InventoryAvgCost = cloneIntMap(snap.InventoryAvgCost)
		}
		p.Ready = snap.Ready
		p.Modals = append([]ModalItem(nil), snap.Modals...)
		if snap.Fuel > 0 {
			p.Fuel = snap.Fuel
		} else {
			p.Fuel = fuelCapacity
		}
		p.InTransit = snap.InTransit
		p.TransitFrom = snap.TransitFrom
		p.TransitRemaining = snap.TransitRemaining
		p.TransitTotal = snap.TransitTotal
		p.CapacityBonus = snap.CapacityBonus
		p.SpeedBonus = snap.SpeedBonus
		p.FuelCapacityBonus = snap.FuelCapacityBonus
		p.Bankrupt = snap.Bankrupt
		// restore per-room action history
		p.ActionHistory = cloneActionHistory(snap.ActionHistory)
		delete(room.Persist, p.ID)
	} else {
		// New room without a snapshot: start with fresh per-room state
		p.Money = 1000
		p.CurrentPlanet = "Earth"
		p.DestinationPlanet = ""
		p.Ready = false
		p.Fuel = fuelCapacity
		p.Inventory = map[string]int{}
		p.InventoryAvgCost = map[string]int{}
		p.Modals = []ModalItem{}
		p.InTransit = false
		p.TransitFrom = ""
		p.TransitRemaining = 0
		p.TransitTotal = 0
		p.CapacityBonus = 0
		p.SpeedBonus = 0
		p.FuelCapacityBonus = 0
		p.Bankrupt = false
		// fresh room: clear per-room action history
		p.ActionHistory = nil
	}
	if p.Inventory == nil {
		p.Inventory = map[string]int{}
	}
	if p.InventoryAvgCost == nil {
		p.InventoryAvgCost = map[string]int{}
	}
	if p.Modals == nil {
		p.Modals = []ModalItem{}
	}
	room.Players[p.ID] = p
	room.mu.Unlock()
	gs.sendRoomState(room, p)
	gs.broadcastRoom(room)
}

// exitRoom removes the player from the room and returns them to the lobby, persisting their state
func (gs *GameServer) exitRoom(p *Player) {
	room := gs.getRoom(p.roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	room.Persist[p.ID] = &PersistedPlayer{
		Money:             p.Money,
		CurrentPlanet:     p.CurrentPlanet,
		DestinationPlanet: p.DestinationPlanet,
		Inventory:         cloneIntMap(p.Inventory),
		InventoryAvgCost:  cloneIntMap(p.InventoryAvgCost),
		Ready:             p.Ready,
		Fuel:              p.Fuel,
		Bankrupt:          p.Bankrupt,
		InTransit:         p.InTransit,
		TransitFrom:       p.TransitFrom,
		TransitRemaining:  p.TransitRemaining,
		TransitTotal:      p.TransitTotal,
		CapacityBonus:     p.CapacityBonus,
		SpeedBonus:        p.SpeedBonus,
		FuelCapacityBonus: p.FuelCapacityBonus,
		ActionHistory:     cloneActionHistory(p.ActionHistory),
	}
	delete(room.Players, p.ID)
	p.roomID = ""
	// If game running and no humans remain, prompt the ticker to end turn now
	if room.Started {
		hasHuman := false
		for _, pl := range room.Players {
			if !pl.IsBot {
				hasHuman = true
				break
			}
		}
		if !hasHuman {
			select {
			case room.readyCh <- struct{}{}:
			default:
			}
		}
	}
	room.mu.Unlock()
	gs.broadcastRoom(room)
	// send them back to the lobby
	if p.conn != nil {
		gs.sendLobbyState(p)
	}
}

func (gs *GameServer) startGame(roomID string) {
	room, exists := gs.rooms[roomID]
	if !exists {
		return
	}

	// Prevent multiple game loops for the same room
	if room.loopRunning {
		fmt.Printf("Game loop already running for room %s\n", roomID)
		return
	}

	// Ensure any old game is properly stopped
	if room.stopChan != nil {
		close(room.stopChan)
		time.Sleep(100 * time.Millisecond) // Give the old loop time to exit
	}

	// Initialize stop channel for this game
	room.stopChan = make(chan struct{})
	room.loopRunning = true
	room.Started = true
	room.Turn = 1
	room.TimeRemaining = turnDuration

	// Initialize game systems
	gs.initializeSystems(room)

	// Initialize players with starting positions
	for id, player := range room.Players {
		if !player.IsBot || player.IsBankrupt {
			continue
		}
		player.Cash = startingCash
		player.Cargo = make(map[string]int)
		player.Location = "Earth"
		player.Destination = ""
		player.TravelRemaining = 0
		player.NetWorth = startingCash
		room.Players[id] = player
	}

	// Start the game loop in a goroutine with the room ID
	go gs.startGameLoop(roomID)

	fmt.Printf("Started game for room %s\n", roomID)
}

func (gs *GameServer) startGameLoop(roomID string) {
	defer func() {
		gs.mu.Lock()
		if room, exists := gs.rooms[roomID]; exists {
			room.loopRunning = false
		}
		gs.mu.Unlock()
		fmt.Printf("Game loop stopped for room %s\n", roomID)
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gs.mu.Lock()
			room, exists := gs.rooms[roomID]
			if !exists {
				gs.mu.Unlock()
				return
			}

			if !room.Started {
				gs.mu.Unlock()
				return
			}

			// Process game tick for THIS specific room
			room.TimeRemaining--
			if room.TimeRemaining <= 0 {
				room.Turn++
				room.TimeRemaining = turnDuration

				if room.Turn > room.MaxTurns {
					gs.endGameInternal(room)
					gs.broadcastRoomState(roomID)
					gs.mu.Unlock()
					return
				}

				// Generate events for THIS room
				gs.generateEventsForRoom(room)
				// Update prices for THIS room
				gs.updatePricesForRoom(room)
			}

			// Process player movements for THIS room
			gs.processPlayerMovements(room)

			// Bot decisions for THIS room
			gs.processBotActions(room)

			// Broadcast state
			gs.broadcastRoomState(roomID)
			gs.mu.Unlock()

		case <-room.stopChan:
			return
		}
	}
}

func (gs *GameServer) endGame(roomID string) {
	room, exists := gs.rooms[roomID]
	if !exists {
		return
	}

	gs.endGameInternal(room)
}

func (gs *GameServer) endGameInternal(room *Room) {
	// Stop the game loop
	if room.stopChan != nil {
		select {
		case <-room.stopChan:
			// Already closed
		default:
			close(room.stopChan)
		}
		room.stopChan = nil
	}

	room.Started = false
	room.loopRunning = false
	room.Turn = 0
	room.TimeRemaining = 0

	// Reset player states
	for id, player := range room.Players {
		player.Ready = false
		player.EndGame = false
		player.IsBankrupt = false
		player.NetWorth = 0
		player.Cash = startingCash
		player.Cargo = make(map[string]int)
		player.Location = ""
		player.Destination = ""
		player.TravelRemaining = 0
		room.Players[id] = player
	}

	// Clear game state
	room.Systems = make(map[string]*System)
	room.Prices = make(map[string]map[string]int)
	room.Events = []Event{}

	fmt.Printf("Ended game for room %s\n", room.ID)
}

func (gs *GameServer) closeRoom(roomID string) {
	room, exists := gs.rooms[roomID]
	if !exists {
		return
	}

	fmt.Printf("Closing room %s\n", roomID)

	// Stop the game if it's running
	if room.Started || room.loopRunning {
		gs.endGameInternal(room)
		time.Sleep(100 * time.Millisecond) // Give the loop time to exit cleanly
	}

	// Move all players back to lobby
	for _, player := range room.Players {
		if conn, exists := gs.clients[player.ID]; exists {
			// Send lobby state to the player
			gs.sendLobbyState(conn)
		}
	}

	// Delete the room
	delete(gs.rooms, roomID)
}

func (gs *GameServer) generateEventsForRoom(room *Room) {
	// Generate events specific to THIS room
	room.Events = []Event{}

	eventTypes := []string{"price_spike", "shortage", "surplus", "pirate_activity"}
	if rand.Float64() < 0.3 {
		eventType := eventTypes[rand.Intn(len(eventTypes))]
		room.Events = append(room.Events, Event{
			Type:    eventType,
			Message: fmt.Sprintf("Turn %d: %s reported in the system", room.Turn, eventType),
			Turn:    room.Turn,
		})
	}
}

func (gs *GameServer) updatePricesForRoom(room *Room) {
	// Update prices for THIS room's systems only
	for _, system := range room.Systems {
		if room.Prices[system.Name] == nil {
			room.Prices[system.Name] = make(map[string]int)
		}

		for commodity := range commodities {
			basePrice := commodities[commodity]
			variation := rand.Float64()*0.4 - 0.2
			room.Prices[system.Name][commodity] = int(float64(basePrice) * (1 + variation))
		}
	}
}

func (gs *GameServer) processPlayerMovements(room *Room) {
	// Process movements for players in THIS room only
	for id, player := range room.Players {
		if player.TravelRemaining > 0 {
			player.TravelRemaining--
			if player.TravelRemaining == 0 && player.Destination != "" {
				// Player arrived
				player.Location = player.Destination
				player.Destination = ""

				// Random dock fee for THIS player in THIS room
				if rand.Float64() < 0.1 {
					fee := 10
					player.Cash -= fee
					room.Events = append(room.Events, Event{
						Type:    "fee",
						Message: fmt.Sprintf("%s paid $%d in docking fees at %s", player.Name, fee, player.Location),
						Turn:    room.Turn,
					})
				}
			}
			room.Players[id] = player
		}
	}
}

func (gs *GameServer) processBotActions(room *Room) {
	// Bot AI for THIS room only
	for id, player := range room.Players {
		if !player.IsBot || player.IsBankrupt || player.TravelRemaining > 0 {
			continue
		}

		// Simple bot AI - randomly travel or trade
		if player.Location != "" && player.Destination == "" {
			systems := []string{}
			for name := range room.Systems {
				if name != player.Location {
					systems = append(systems, name)
				}
			}

			if len(systems) > 0 && rand.Float64() < 0.3 {
				dest := systems[rand.Intn(len(systems))]
				player.Destination = dest
				player.TravelRemaining = 5
				room.Players[id] = player
			}
		}
	}
}

// ...rest of existing code...
