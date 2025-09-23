package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	Started         bool                          `json:"started"`
	Players         map[PlayerID]*Player          `json:"players"`
	Turn            int                           `json:"turn"`
	Planets         map[string]*Planet            `json:"planets"`
	Persist         map[PlayerID]*PersistedPlayer `json:"-"`
	mu              sync.Mutex
	readyCh         chan struct{}         // signal to end turn early when all humans are ready
	closeCh         chan struct{}         // signal to stop the ticker when room is closed
	TurnEndsAt      time.Time             `json:"-"`
	News            []NewsItem            `json:"-"`
	PlanetOrder     []string              `json:"-"`
	PlanetPositions map[string][2]float64 `json:"-"`
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

				// Check if room is now empty
				isEmpty := len(room.Players) == 0

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

				// If room is empty, close it to stop the ticker and clean up
				if isEmpty {
					gs.roomsMu.Lock()
					delete(gs.rooms, room.ID)
					gs.roomsMu.Unlock()
					// Signal ticker to stop
					select {
					case room.closeCh <- struct{}{}:
					default:
					}
				} else {
					gs.broadcastRoom(room)
				}
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
		closeCh: make(chan struct{}),
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
		EndGame:           p.EndGame,
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

	// Check if room is now empty and should be cleaned up
	isEmpty := len(room.Players) == 0

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

	// If room is empty, close it to stop the ticker and clean up
	if isEmpty {
		gs.roomsMu.Lock()
		delete(gs.rooms, room.ID)
		gs.roomsMu.Unlock()
		// Signal ticker to stop
		select {
		case room.closeCh <- struct{}{}:
		default:
		}
	} else {
		gs.broadcastRoom(room)
	}

	// send them back to the lobby
	if p.conn != nil {
		gs.sendLobbyState(p)
	}
}

func (gs *GameServer) startGame(roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	if !room.Started {
		// Only start if all non-bot players are ready
		canStart := true
		for _, pl := range room.Players {
			if pl.IsBot {
				continue
			}
			if !pl.Ready {
				canStart = false
				break
			}
		}
		if canStart {
			// reset human ready flags at game start
			for _, pl := range room.Players {
				if !pl.IsBot {
					pl.Ready = false
				}
			}
			// Planet order/positions are set at room creation; keep them unless unset
			if len(room.PlanetOrder) == 0 {
				names := planetNames(room.Planets)
				for i := range names {
					j := rand.Intn(i + 1)
					names[i], names[j] = names[j], names[i]
				}
				room.PlanetOrder = names
			}
			if len(room.PlanetPositions) == 0 {
				room.PlanetPositions = generatePlanetPositions(room.PlanetOrder)
			}
			room.Started = true
			room.Turn = 0
			// If no humans at start, set deadline to now; runTicker will extend when a human appears
			if func() bool {
				for _, pl := range room.Players {
					if !pl.IsBot {
						return true
					}
				}
				return false
			}() {
				room.TurnEndsAt = time.Now().Add(turnDuration)
			} else {
				room.TurnEndsAt = time.Now()
			}
			go gs.runTicker(room)
		}
	}
	room.mu.Unlock()
	gs.broadcastRoom(room)
}

// addBot creates a server-controlled bot player and adds it to the room
func (gs *GameServer) addBot(roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}
	b := &Player{
		ID:                PlayerID(randID()),
		Name:              "Bot " + randID()[0:3],
		Money:             1000,
		CurrentPlanet:     "Earth",
		DestinationPlanet: "",
		Inventory:         map[string]int{},
		InventoryAvgCost:  map[string]int{},
		Fuel:              fuelCapacity,
		Ready:             true, // bots are always ready
		IsBot:             true,
	}
	b.roomID = room.ID
	room.mu.Lock()
	room.Players[b.ID] = b
	room.mu.Unlock()
	gs.broadcastRoom(room)
}

func (gs *GameServer) runTicker(room *Room) {
	base := turnDuration
	for {
		// Determine if there are any human players; if only bots, don't wait 60s
		room.mu.Lock()
		onlyBots := true
		for _, pl := range room.Players {
			if !pl.IsBot {
				onlyBots = false
				break
			}
		}
		// If humans are present but the current deadline is stale (e.g., came from a bot-only turn), push a fresh deadline now
		if !onlyBots {
			now := time.Now()
			if room.TurnEndsAt.Before(now.Add(1 * time.Second)) {
				room.TurnEndsAt = now.Add(base)
				// We'll broadcast below after unlocking so clients see a proper countdown immediately upon join
			}
		}
		room.mu.Unlock()
		if !onlyBots {
			// Broadcast any updated deadline on transition from bot-only to human-present
			gs.broadcastRoom(room)
		}

		if onlyBots {
			// Skip the long timer; tiny pause to avoid a tight CPU loop
			select {
			case <-room.closeCh:
				return // Room is being closed, exit ticker
			case <-time.After(150 * time.Millisecond):
				// Continue to next iteration
			}
		} else {
			// Wait for either timer or early ready signal or close signal
			timer := time.NewTimer(base)
			select {
			case <-timer.C:
				// time-based turn end
			case <-room.readyCh:
				// early turn end due to all humans ready
				if !timer.Stop() {
					<-timer.C
				}
			case <-room.closeCh:
				if !timer.Stop() {
					<-timer.C
				}
				return // Room is being closed, exit ticker
			}
		}
		// Process end-of-turn effects and start the next turn
		room.mu.Lock()
		if !room.Started {
			room.mu.Unlock()
			return
		}
		room.Turn++
		// new turn begins; set the next deadline and reset human ready
		if onlyBots {
			room.TurnEndsAt = time.Now()
		} else {
			room.TurnEndsAt = time.Now().Add(base)
		}
		// Apply news effects to prices and production based on baselines
		// First, recompute from baselines (and reset fuel price)
		for _, pl := range room.Planets {
			for g, v := range pl.BasePrices {
				pl.Prices[g] = v
			}
			for g, v := range pl.BaseProd {
				pl.Prod[g] = v
			}
			pl.FuelPrice = pl.BaseFuelPrice
		}
		// Decrement news and apply active deltas, clamping to static ranges
		nextNews := make([]NewsItem, 0, len(room.News))
		ranges := defaultPriceRanges()
		// Track per-planet per-good bias from price-affecting headlines (sign only)
		newsBias := map[string]map[string]int{}
		for _, ni := range room.News {
			if ni.TurnsRemaining <= 0 {
				continue
			}
			// apply
			planet := room.Planets[ni.Planet]
			if planet != nil {
				for g, d := range ni.PriceDelta {
					p := planet.Prices[g] + d
					if r, ok := ranges[g]; ok {
						p = clampInt(p, r[0], r[1])
					} else if p < 0 {
						p = 0
					}
					planet.Prices[g] = p
					// accumulate bias by the sign of the delta
					if _, ok := newsBias[ni.Planet]; !ok {
						newsBias[ni.Planet] = map[string]int{}
					}
					if d > 0 {
						newsBias[ni.Planet][g] += 1
					} else if d < 0 {
						newsBias[ni.Planet][g] -= 1
					}
				}
				for g, d := range ni.ProdDelta {
					planet.Prod[g] = maxInt(0, planet.Prod[g]+d)
				}
				if ni.FuelPriceDelta != 0 {
					np := planet.FuelPrice + ni.FuelPriceDelta
					if np < 5 {
						np = 5
					}
					if np > 24 {
						np = 24
					}
					planet.FuelPrice = np
				}
			}
			ni.TurnsRemaining--
			if ni.TurnsRemaining > 0 {
				nextNews = append(nextNews, ni)
			}
		}
		room.News = nextNews
		// Apply small per-good price drift based on persistent trend, biased by active news
		for pname, pl := range room.Planets {
			if pl.PriceTrend == nil {
				pl.PriceTrend = map[string]int{}
			}
			for g := range pl.Prices {
				// step trend by +/-1 with bias from news: tilt towards the sign of recent price headlines
				b := 0
				if m, ok := newsBias[pname]; ok {
					b = m[g]
				}
				// probability weighting: if bias>0 prefer +1 (75%), if bias<0 prefer -1 (75%), else 50/50
				step := 1
				if b > 0 {
					if rand.Intn(4) == 0 { // 25% chance to go opposite
						step = -1
					} else {
						step = 1
					}
				} else if b < 0 {
					if rand.Intn(4) == 0 {
						step = 1
					} else {
						step = -1
					}
				} else {
					if rand.Intn(2) == 0 {
						step = -1
					} else {
						step = 1
					}
				}
				pl.PriceTrend[g] += step
				base := pl.BasePrices[g]
				if r, ok := ranges[g]; ok {
					// keep base + trend within bounds
					if base+pl.PriceTrend[g] < r[0] {
						pl.PriceTrend[g] = r[0] - base
					}
					if base+pl.PriceTrend[g] > r[1] {
						pl.PriceTrend[g] = r[1] - base
					}
					// apply trend on top of (base + news deltas)
					p := pl.Prices[g] + pl.PriceTrend[g]
					pl.Prices[g] = clampInt(p, r[0], r[1])
				} else {
					// No explicit range; ensure non-negative
					p := pl.Prices[g] + pl.PriceTrend[g]
					if p < 0 {
						p = 0
						// adjust trend to reflect clamp vs base
						if base < 0 {
							pl.PriceTrend[g] = 0
						} else {
							pl.PriceTrend[g] = -base
						}
					}
					pl.Prices[g] = p
				}
			}
		}
		// Randomly generate 0-2 news items per turn
		gs.generateNews(room)
		// resolve travel with fuel consumption
		for _, p := range room.Players {
			if p.Bankrupt {
				continue
			}
			if p.DestinationPlanet != "" && p.DestinationPlanet != p.CurrentPlanet {
				// initialize transit if needed
				if !p.InTransit || p.TransitRemaining <= 0 || p.TransitFrom == "" {
					p.InTransit = true
					p.TransitFrom = p.CurrentPlanet
					p.TransitRemaining = distanceUnits(room, p.CurrentPlanet, p.DestinationPlanet)
					p.TransitTotal = p.TransitRemaining
				}
				// Determine this turn's movement: up to (20 + SpeedBonus) units, but cannot exceed fuel
				moveCap := 20 + p.SpeedBonus
				move := minInt(moveCap, p.TransitRemaining)
				move = minInt(move, p.Fuel)
				if move <= 0 {
					// No fuel to progress
					if p.IsBot {
						// For bots, cancel transit so they can refuel or adjust plans during AI step
						p.InTransit = false
						p.DestinationPlanet = ""
						p.TransitFrom = ""
						p.TransitRemaining = 0
						p.TransitTotal = 0
					} else {
						gs.enqueueModal(p, "Insufficient Fuel", "You didn't have enough fuel to make progress toward "+p.DestinationPlanet+".")
					}
					// Either we showed a modal (human) and stayed in transit, or we canceled (bot). Proceed to next player.
					continue
				}
				// Consume fuel and reduce remaining distance
				p.Fuel -= move
				p.TransitRemaining -= move
				if p.TransitRemaining <= 0 {
					// Arrived
					p.CurrentPlanet = p.DestinationPlanet
					p.DestinationPlanet = ""
					p.InTransit = false
					p.TransitFrom = ""
					p.TransitRemaining = 0
					p.TransitTotal = 0
					// Dock tax on arrival
					p.Money -= 10
					gs.logAction(room, p, "Dock tax paid: $10")
					if !p.IsBot {
						gs.enqueueModal(p, "Dock Tax", "Docking fee of 10 credits charged at "+p.CurrentPlanet+".")
					}
					if p.Money < -500 && !p.Bankrupt {
						// Queue Game Over first, then mark bankrupt to preserve the modal
						if !p.IsBot {
							gs.enqueueModal(p, "Game Over", "Your ship was impounded for unpaid dock taxes. You may continue watching.")
						}
						// Impound and bankrupt
						p.Bankrupt = true
						// Announce via news ticker
						room.News = append(room.News, NewsItem{Headline: p.Name + " bankrupted by dock taxes at " + p.CurrentPlanet, Planet: p.CurrentPlanet, TurnsRemaining: 3})
					}
				} else {
					// Still en route
					p.InTransit = true
					if !p.IsBot {
						gs.enqueueModal(p, "In Transit", "You are still in transit towards "+p.DestinationPlanet+".")
					}
				}
			} else {
				// Staying in same location: apply dock tax if not in transit
				if !p.InTransit {
					p.Money -= 10
					gs.logAction(room, p, "Dock tax paid: $10")
					if !p.IsBot {
						gs.enqueueModal(p, "Dock Tax", "Docking fee of 10 credits charged at "+p.CurrentPlanet+".")
					}
					if p.Money < -500 && !p.Bankrupt {
						if !p.IsBot {
							gs.enqueueModal(p, "Game Over", "Your ship was impounded for unpaid dock taxes. You may continue watching.")
						}
						p.Bankrupt = true
						room.News = append(room.News, NewsItem{Headline: p.Name + " bankrupted by dock taxes at " + p.CurrentPlanet, Planet: p.CurrentPlanet, TurnsRemaining: 3})
					}
				}
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
		// simple bot AI: sell when price > 50% of max, buy when price < 46% of max, pick a new random destination
		for _, bp := range room.Players {
			if !bp.IsBot {
				continue
			}
			// Bots should not trade/refuel or pick new destinations while in transit
			if bp.InTransit {
				continue
			}
			planet := room.Planets[bp.CurrentPlanet]
			if planet == nil {
				continue
			}
			// reference price ranges per good
			ranges := defaultPriceRanges()
			// sell when price is above 50% of max
			for g, qty := range bp.Inventory {
				price := planet.Prices[g]
				max := 0
				if r, ok := ranges[g]; ok {
					max = r[1]
				}
				threshold := (max * 50) / 100
				if qty > 0 && price > threshold {
					bp.Inventory[g] -= qty
					planet.Goods[g] += qty
					proceeds := qty * price
					bp.Money += proceeds
					gs.logAction(room, bp, fmt.Sprintf("Sold %d %s for $%d", qty, g, proceeds))
					if bp.Inventory[g] <= 0 {
						delete(bp.Inventory, g)
						delete(bp.InventoryAvgCost, g)
					}
				}
			}
			// Fuel-first policy: ensure a minimum reserve before buying goods
			minReserve := 20
			if bp.Fuel < minReserve {
				fp := planet.FuelPrice
				if fp <= 0 {
					fp = 10
				}
				capLeft := (fuelCapacity + bp.FuelCapacityBonus) - bp.Fuel
				if capLeft > 0 {
					need := minInt(minReserve-bp.Fuel, capLeft)
					if need > 0 {
						cost := need * fp
						if bp.Money < cost {
							// Sell cargo at current prices (highest first) to fund fuel
							type kv struct {
								g string
								p int
							}
							goods := make([]kv, 0, len(bp.Inventory))
							for g := range bp.Inventory {
								goods = append(goods, kv{g: g, p: planet.Prices[g]})
							}
							sort.Slice(goods, func(i, j int) bool { return goods[i].p > goods[j].p })
							short := cost - bp.Money
							for _, item := range goods {
								if short <= 0 {
									break
								}
								g := item.g
								price := planet.Prices[g]
								if price <= 0 {
									continue
								}
								qty := bp.Inventory[g]
								if qty <= 0 {
									continue
								}
								needUnits := (short + price - 1) / price
								sellUnits := qty
								if sellUnits > needUnits {
									sellUnits = needUnits
								}
								bp.Inventory[g] -= sellUnits
								planet.Goods[g] += sellUnits
								proceeds := sellUnits * price
								bp.Money += proceeds
								gs.logAction(room, bp, fmt.Sprintf("Liquidated %d %s for $%d to fund fuel", sellUnits, g, proceeds))
								short -= proceeds
								if bp.Inventory[g] <= 0 {
									delete(bp.Inventory, g)
									delete(bp.InventoryAvgCost, g)
								}
							}
						}
						// Buy as much as needed (or affordable) toward the reserve
						canBuy := minInt(need, bp.Money/fp)
						if canBuy > 0 {
							total := canBuy * fp
							bp.Money -= total
							bp.Fuel += canBuy
							gs.logAction(room, bp, fmt.Sprintf("Refueled %d units for $%d at %s", canBuy, total, bp.CurrentPlanet))
						}
					}
				}
			}
			// buy only when price is below 46% of max — skip if still below fuel reserve
			if bp.Fuel >= minReserve {
				keys := make([]string, 0, len(planet.Goods))
				for k := range planet.Goods {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, g := range keys {
					price := planet.Prices[g]
					if price <= 0 {
						continue
					}
					max := 0
					if r, ok := ranges[g]; ok {
						max = r[1]
					}
					if max <= 0 {
						continue
					}
					threshold := (max * 46) / 100
					// only buy when strictly less than 46% of max
					if price >= threshold {
						continue
					}
					avail := planet.Goods[g]
					if avail <= 0 || bp.Money < price {
						continue
					}
					// Determine purchase amount subject to money, availability, and ship capacity
					amount := bp.Money / price
					if amount > avail {
						amount = avail
					}
					// Respect ship capacity for bots as well
					used := inventoryTotal(bp.Inventory)
					free := shipCapacity + bp.CapacityBonus - used
					if free <= 0 {
						break
					}
					if amount > free {
						amount = free
					}
					if amount <= 0 {
						continue
					}
					cost := amount * price
					bp.Money -= cost
					planet.Goods[g] -= amount
					oldQty := bp.Inventory[g]
					oldAvg := bp.InventoryAvgCost[g]
					newQty := oldQty + amount
					bp.Inventory[g] = newQty
					if newQty > 0 {
						newAvg := (oldQty*oldAvg + amount*price) / newQty
						bp.InventoryAvgCost[g] = newAvg
					} else {
						delete(bp.InventoryAvgCost, g)
					}
					gs.logAction(room, bp, fmt.Sprintf("Bought %d %s for $%d", amount, g, cost))
				}
			}
			// Bot refuel behavior (uses local planet fuel price)
			if bp.Fuel < 20 {
				price := planet.FuelPrice
				if price <= 0 {
					price = 10
				}
				maxUnits := minInt((fuelCapacity+bp.FuelCapacityBonus)-bp.Fuel, bp.Money/price)
				if maxUnits > 0 {
					total := maxUnits * price
					bp.Money -= total
					bp.Fuel += maxUnits
					gs.logAction(room, bp, fmt.Sprintf("Refueled %d units for $%d at %s", maxUnits, total, bp.CurrentPlanet))
				}
			}
			// choose a new destination different from current; if out of range, try to sell cargo to afford fuel to reach it
			pn := planetNames(room.Planets)
			if len(pn) > 1 {
				for tries := 0; tries < 8; tries++ {
					dest := pn[rand.Intn(len(pn))]
					if dest == bp.CurrentPlanet {
						continue
					}
					dist := distanceUnits(room, bp.CurrentPlanet, dest)
					if dist <= bp.Fuel {
						bp.DestinationPlanet = dest
						gs.logAction(room, bp, fmt.Sprintf("Traveling to %s (%d units)", dest, dist))
						break
					}
					// Need more fuel to reach this destination
					fuelNeeded := dist - bp.Fuel
					// Respect fuel capacity
					capLeft := (fuelCapacity + bp.FuelCapacityBonus) - bp.Fuel
					if capLeft <= 0 || capLeft < fuelNeeded {
						// Can't carry enough fuel to reach; try another destination
						continue
					}
					fp := planet.FuelPrice
					if fp <= 0 {
						fp = 10
					}
					cost := fuelNeeded * fp
					if bp.Money < cost {
						// Try to liquidate cargo (even at a loss) to raise funds for fuel
						short := cost - bp.Money
						// Build goods list sorted by current price desc to sell fewer units
						type kv struct {
							g string
							p int
						}
						goods := make([]kv, 0, len(bp.Inventory))
						for g := range bp.Inventory {
							goods = append(goods, kv{g: g, p: planet.Prices[g]})
						}
						sort.Slice(goods, func(i, j int) bool { return goods[i].p > goods[j].p })
						for _, item := range goods {
							if short <= 0 {
								break
							}
							g := item.g
							price := planet.Prices[g]
							if price <= 0 {
								continue
							}
							qty := bp.Inventory[g]
							if qty <= 0 {
								continue
							}
							// units to sell to cover the shortfall (rounded up)
							needUnits := (short + price - 1) / price
							sellUnits := qty
							if sellUnits > needUnits {
								sellUnits = needUnits
							}
							// perform sale
							bp.Inventory[g] -= sellUnits
							planet.Goods[g] += sellUnits
							proceeds := sellUnits * price
							bp.Money += proceeds
							short -= proceeds
							if bp.Inventory[g] <= 0 {
								delete(bp.Inventory, g)
								delete(bp.InventoryAvgCost, g)
							}
						}
					}
					if bp.Money >= cost {
						// Buy the fuel now and set destination
						bp.Money -= cost
						bp.Fuel += fuelNeeded
						bp.DestinationPlanet = dest
						gs.logAction(room, bp, fmt.Sprintf("Purchased fuel %d for $%d and traveling to %s (%d units)", fuelNeeded, cost, dest, dist))
						break
					}
					// Otherwise, try another candidate
				}
			}
		}
		// Rare per-player events; bots are impacted too. Humans receive modals; bots auto-resolve.
		for _, hp := range room.Players {
			if hp.Bankrupt {
				// Ensure no destination/arrow for bankrupt players
				hp.DestinationPlanet = ""
				continue
			}
			if hp.IsBot {
				// Auto-accept capacity upgrade sometimes if affordable
				if rand.Intn(50) == 0 { // ~2% per turn
					price := 5000
					bonus := 50
					if hp.Money >= price {
						hp.Money -= price
						hp.CapacityBonus += bonus
						gs.logAction(room, hp, fmt.Sprintf("Purchased cargo upgrade +%d for $%d", bonus, price))
					}
				}
				// Consider engine speed offer if rolled this turn
				if rand.Intn(40) == 0 {
					units := 1 + rand.Intn(10)
					price := units * 1000
					if hp.Money >= price {
						hp.Money -= price
						hp.SpeedBonus += units
						gs.logAction(room, hp, fmt.Sprintf("Purchased engine upgrade +%d for $%d", units, price))
					}
				}
				// Consider fuel capacity offer if rolled this turn
				if rand.Intn(40) == 0 {
					units := 20 + rand.Intn(81)
					price := units * 50
					if hp.Money >= price {
						hp.Money -= price
						hp.FuelCapacityBonus += units
						gs.logAction(room, hp, fmt.Sprintf("Purchased fuel tank +%d for $%d", units, price))
					}
				}
				// Bot asteroid collision chance mirrored for completeness
				if rand.Intn(100) == 0 {
					hp.Inventory = map[string]int{}
					hp.InventoryAvgCost = map[string]int{}
					gs.logAction(room, hp, "Asteroid collision: lost all cargo")
				}
				// Bots skip modals; move on to next player
				continue
			}
			// Income tax: ~1% chance per turn
			if rand.Intn(100) == 0 {
				// total wealth = money + value paid for current cargo
				totalWealth := hp.Money
				for g, qty := range hp.Inventory {
					avg := hp.InventoryAvgCost[g]
					totalWealth += qty * avg
				}
				if totalWealth < 0 {
					totalWealth = 0
				}
				tax := (totalWealth * 8) / 100 // 8%
				// Deduct tax (can go negative to reflect debt)
				hp.Money -= tax
				gs.logAction(room, hp, fmt.Sprintf("Income tax paid: $%d", tax))
				gs.enqueueModal(hp, "Federation Tax", "Federation income tax is due. "+strconv.Itoa(tax)+" credits due.")
				if hp.Money < -500 && !hp.Bankrupt {
					// Queue Game Over first, then mark bankrupt so modal isn't blocked
					gs.enqueueModal(hp, "Game Over", "Your ship was impounded for unpaid debts. You may continue watching.")
					hp.Bankrupt = true
					room.News = append(room.News, NewsItem{Headline: hp.Name + " bankrupted by taxes at " + hp.CurrentPlanet, Planet: hp.CurrentPlanet, TurnsRemaining: 3})
					// Skip the rest of events for this player this turn
					continue
				}
			}
			// Lottery win: ~1% chance per turn
			if rand.Intn(100) == 0 {
				amt := 500 + rand.Intn(10000-500+1)
				hp.Money += amt
				gs.logAction(room, hp, fmt.Sprintf("Lottery winnings: +$%d", amt))
				gs.enqueueModal(hp, "Lottery Winner!", "You won the lottery and collect "+strconv.Itoa(amt)+" credits!")
			}
			// Asteroid collision: ~1% chance per turn
			if rand.Intn(100) == 0 {
				// Lose all cargo
				hp.Inventory = map[string]int{}
				hp.InventoryAvgCost = map[string]int{}
				gs.logAction(room, hp, "Asteroid collision: lost all cargo")
				gs.enqueueModal(hp, "Asteroid Collision", "Your ship collided with an asteroid and you lost all cargo.")
			}
			// Capacity upgrade offer: ~2% chance per turn
			if rand.Intn(50) == 0 {
				price := 5000
				bonus := 50
				mi := ModalItem{ID: randID(), Title: "Shipyard Offer", Body: "Special offer: +" + strconv.Itoa(bonus) + " cargo capacity for $" + strconv.Itoa(price) + ". Accept?", Kind: "upgrade-offer", Price: price, CapacityBonus: bonus}
				gs.logAction(room, hp, fmt.Sprintf("Offer: +%d cargo for $%d", bonus, price))
				hp.Modals = append(hp.Modals, mi)
			}
			// Speed upgrade offer: 1-10 units, $1000 per unit
			if rand.Intn(40) == 0 { // ~2.5%/turn
				units := 1 + rand.Intn(10)
				ppu := 1000
				price := units * ppu
				mi := ModalItem{ID: randID(), Title: "Engine Upgrade", Body: "Offer: +" + strconv.Itoa(units) + " speed (units/turn) for $" + strconv.Itoa(ppu) + " per unit (total $" + strconv.Itoa(price) + "). Accept?", Kind: "speed-offer", PricePerUnit: ppu, Units: units}
				gs.logAction(room, hp, fmt.Sprintf("Offer: +%d speed for $%d total", units, price))
				hp.Modals = append(hp.Modals, mi)
			}
			// Fuel capacity upgrade offer: 20-100 units, $50 per unit
			if rand.Intn(40) == 0 { // ~2.5%/turn
				units := 20 + rand.Intn(81) // 20..100
				ppu := 50
				price := units * ppu
				mi := ModalItem{ID: randID(), Title: "Fuel Tank Expansion", Body: "Offer: +" + strconv.Itoa(units) + " fuel capacity for $" + strconv.Itoa(ppu) + " per unit (total $" + strconv.Itoa(price) + "). Accept?", Kind: "fuelcap-offer", PricePerUnit: ppu, Units: units}
				gs.logAction(room, hp, fmt.Sprintf("Offer: +%d fuel capacity for $%d total", units, price))
				hp.Modals = append(hp.Modals, mi)
			}
		}
		// reset human players' ready flags for the new turn
		for _, pl := range room.Players {
			if !pl.IsBot && !pl.Bankrupt {
				pl.Ready = false
			}
		}
		room.mu.Unlock()
		gs.broadcastRoom(room)
	}
}

func (gs *GameServer) generateNews(room *Room) {
	// 50% chance to generate one item, 25% chance to generate two
	count := 0
	if rand.Intn(2) == 0 {
		count = 1
	}
	if rand.Intn(4) == 0 {
		count = 2
	}
	if count == 0 {
		return
	}
	planets := planetNames(room.Planets)
	if len(planets) == 0 {
		return
	}
	goodsSet := map[string]struct{}{}
	for _, pl := range room.Planets {
		for g := range pl.Prices {
			goodsSet[g] = struct{}{}
		}
	}
	goods := make([]string, 0, len(goodsSet))
	for g := range goodsSet {
		goods = append(goods, g)
	}
	sort.Strings(goods)
	ranges := defaultPriceRanges()
	for i := 0; i < count; i++ {
		planet := planets[rand.Intn(len(planets))]
		g := goods[rand.Intn(len(goods))]
		turns := 2 + rand.Intn(3) // 2-4 turns
		// Occasionally generate purely whimsical, no-effect headlines
		if rand.Intn(4) == 0 { // ~25% chance
			flavor := []string{
				"Giant rubber duck spotted orbiting {planet}",
				"Space sloths delay cargo lanes near {planet}",
				"Galactic karaoke night declared a hit on {planet}",
				"Meteor shower forms perfect smiley face above {planet}",
				"Zero-G bake-off crowns new croissant champion on {planet}",
				"Mystery signal from {planet} turns out to be an enthusiastic toaster",
				"Tourists report seeing a nebula shaped like a llama near {planet}",
				"Local asteroid adopts three moons near {planet}",
				"Quantum bubble tea craze sweeps {planet}",
				"Holographic parade confuses satellites around {planet}",
				// New additions
				"Cosmic ping-pong tournament announced over {planet}",
				"Wandering comet leaves glitter trail admired from {planet}",
				"Station coffee machine achieves sentience, requests sugar on {planet}",
				"Tiny black hole politely returns lost sock near {planet}",
				"Astronomers confirm cloud shaped like a giant cat over {planet}",
				"Time traveler arrives early to meeting on {planet}",
				"Cargo drones form boy band, release debut single near {planet}",
				"Solar flare writes 'Hi' in cursive above {planet}",
				"Anti-gravity hiccup causes floating picnics on {planet}",
				"Space whales migrate past {planet}, sing in 7/8 time",
				"Local robot wins pie-eating contest on {planet}",
				"Invisible asteroid apologizes for bumping satellites near {planet}",
				"Quantum confetti discovered after birthday on {planet}",
				"Orbiting billboard displays motivational quotes to {planet}",
				"Alien tourists give {planet} five stars for friendly microbes",
				"Nebula selfie breaks galactic internet near {planet}",
				"Hologram weather predicts 100% chance of sparkles over {planet}",
				"Interstellar bakery opens pop-up croissant cloud by {planet}",
				"Diplomatic treaty signed between two feuding moons near {planet}",
				"Synchronized satellite dance delights stargazers on {planet}",
				"Quantum cat simultaneously naps on and off {planet}",
				"Friendly UFO offers free car wash to ships around {planet}",
				"Asteroid fashion week debuts crater chic near {planet}",
				"Half-price warp day causes cheerful traffic jams at {planet}",
				"Rare double rainbow ring encircles {planet}",
				"Space gardeners plant glitter-vines on station above {planet}",
				"Galactic librarian shushes a supernova near {planet}",
				"Meteorologist misplaces a small cumulonimbus over {planet}",
				"AI names new comet 'Snacks-42' as it passes {planet}",
				"Cosmic pancake festival returns to orbit of {planet}",
				"Wormhole pops in to say hello near {planet} and leaves politely",
				"Jazz nebula improvises midnight set above {planet}",
				"Rogue satellite learns to juggle meteoroids near {planet}",
				"Space dolphins spotted surfing solar wind by {planet}",
				"Magnetic storm braids astronaut hair near {planet}",
				"Helpful micro-meteor politely knocks on hulls around {planet}",
			}
			txt := flavor[rand.Intn(len(flavor))]
			txt = strings.ReplaceAll(txt, "{planet}", planet)
			ni := NewsItem{Planet: planet, TurnsRemaining: turns, Headline: txt}
			room.News = append(room.News, ni)
			continue
		}
		// 20% chance to generate a ship fuel price headline (up/down)
		if rand.Intn(5) == 0 {
			ni := NewsItem{Planet: planet, TurnsRemaining: turns}
			// fuel price delta +/- 2-5 credits
			delta := 2 + rand.Intn(4)
			if rand.Intn(2) == 0 {
				delta = -delta
			}
			ni.FuelPriceDelta = delta
			if delta > 0 {
				ni.Headline = "Fuel prices spike on " + planet
			} else {
				ni.Headline = "Fuel prices dip on " + planet
			}
			room.News = append(room.News, ni)
			continue
		}
		// random effect type: price up/down or prod up/down for non-fuel good
		var headline string
		ni := NewsItem{Planet: planet, TurnsRemaining: turns}
		if rand.Intn(2) == 0 {
			// price delta within a fraction of range width
			rng := ranges[g]
			width := maxInt(1, rng[1]-rng[0])
			delta := (width / 5) * (1 + rand.Intn(2)) // ~20-40% of range
			if rand.Intn(2) == 0 {
				delta = -delta
			}
			ni.PriceDelta = map[string]int{g: delta}
			if delta > 0 {
				headline = g + " prices surge on " + planet
			}
			if delta < 0 {
				headline = g + " prices slump on " + planet
			}
		} else {
			// production delta: +/- 1-3 units
			delta := 1 + rand.Intn(3)
			if rand.Intn(2) == 0 {
				delta = -delta
			}
			ni.ProdDelta = map[string]int{g: delta}
			if delta > 0 {
				headline = planet + " boosts " + g + " output"
			}
			if delta < 0 {
				headline = planet + " suffers " + g + " shortages"
			}
		}
		if headline == "" {
			headline = "Market turbulence on " + planet
		}
		ni.Headline = headline
		room.News = append(room.News, ni)
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
	// Enforce ship capacity: cap purchase to remaining free slots
	used := inventoryTotal(p.Inventory)
	free := shipCapacity + p.CapacityBonus - used
	if free <= 0 {
		return
	}
	maxByMoney := p.Money / price
	if amount > maxByMoney {
		amount = maxByMoney
	}
	if amount > available {
		amount = available
	}
	if amount > free {
		amount = free
	}
	if amount <= 0 {
		return
	}
	cost := amount * price
	p.Money -= cost
	planet.Goods[good] -= amount
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
	gs.logAction(room, p, fmt.Sprintf("Purchased %d %s for $%d", amount, good, cost))
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
	proceeds := amount * price
	p.Money += proceeds
	if p.Inventory[good] <= 0 {
		delete(p.Inventory, good)
		delete(p.InventoryAvgCost, good)
	}
	gs.logAction(room, p, fmt.Sprintf("Sold %d %s for $%d", amount, good, proceeds))
}

func (gs *GameServer) sendRoomState(room *Room, only *Player) {
	// prepare minimal view per-player (fog of goods for current planet only)
	room.mu.Lock()
	// compute whether all non-bot, non-bankrupt players are ready
	allReady := true
	for _, pp := range room.Players {
		if pp.IsBot || pp.Bankrupt {
			continue
		}
		if !pp.Ready {
			allReady = false
			break
		}
	}
	players := []map[string]interface{}{}
	for _, pp := range room.Players {
		displayMoney := pp.Money
		moneyField := interface{}(displayMoney)
		if pp.Bankrupt {
			moneyField = "Bankrupt"
		}
		players = append(players, map[string]interface{}{
			"id":                pp.ID,
			"name":              pp.Name,
			"money":             moneyField,
			"currentPlanet":     pp.CurrentPlanet,
			"destinationPlanet": pp.DestinationPlanet,
			"ready":             pp.Ready,
			"endGame":           pp.EndGame,
			"bankrupt":          pp.Bankrupt,
		})
	}
	payloadByPlayer := map[PlayerID]interface{}{}
	recipients := make([]*Player, 0, len(room.Players))
	for id, pp := range room.Players {
		if pp.Bankrupt {
			// Build next modal if any (bankrupt players still see modals like Game Over)
			var nm map[string]interface{}
			if len(pp.Modals) > 0 {
				nm = map[string]interface{}{"id": pp.Modals[0].ID, "title": pp.Modals[0].Title, "body": pp.Modals[0].Body}
				if pp.Modals[0].Kind != "" {
					nm["kind"] = pp.Modals[0].Kind
				}
				if pp.Modals[0].Price != 0 {
					nm["price"] = pp.Modals[0].Price
				}
				if pp.Modals[0].CapacityBonus != 0 {
					nm["capacityBonus"] = pp.Modals[0].CapacityBonus
				}
				if pp.Modals[0].PricePerUnit != 0 {
					nm["pricePerUnit"] = pp.Modals[0].PricePerUnit
				}
				if pp.Modals[0].Units != 0 {
					nm["units"] = pp.Modals[0].Units
				}
				if pp.Modals[0].SpeedBonus != 0 {
					nm["speedBonus"] = pp.Modals[0].SpeedBonus
				}
				if pp.Modals[0].FuelCapacityBonus != 0 {
					nm["fuelCapacityBonus"] = pp.Modals[0].FuelCapacityBonus
				}
			} else {
				nm = map[string]interface{}{}
			}
			// Bankrupt players see no planet detail and cannot interact
			payloadByPlayer[id] = map[string]interface{}{
				"room": map[string]interface{}{
					"id":         room.ID,
					"name":       room.Name,
					"started":    room.Started,
					"turn":       room.Turn,
					"players":    players,
					"turnEndsAt": room.TurnEndsAt.UnixMilli(),
					"allReady":   allReady,
					"planets": func() []string {
						if len(room.PlanetOrder) > 0 {
							out := make([]string, len(room.PlanetOrder))
							copy(out, room.PlanetOrder)
							return out
						}
						return planetNames(room.Planets)
					}(),
					"planetPositions": func() map[string]map[string]float64 {
						if len(room.PlanetPositions) == 0 {
							return nil
						}
						out := make(map[string]map[string]float64, len(room.PlanetPositions))
						for k, v := range room.PlanetPositions {
							out[k] = map[string]float64{"x": v[0], "y": v[1]}
						}
						return out
					}(),
					"news": func() []map[string]interface{} {
						arr := make([]map[string]interface{}, 0, len(room.News))
						for _, n := range room.News {
							arr = append(arr, map[string]interface{}{
								"headline":       n.Headline,
								"planet":         n.Planet,
								"turnsRemaining": n.TurnsRemaining,
							})
						}
						return arr
					}(),
				},
				"you": map[string]interface{}{
					"id":                pp.ID,
					"name":              pp.Name,
					"money":             "Bankrupt",
					"inventory":         map[string]int{},
					"inventoryAvgCost":  map[string]int{},
					"currentPlanet":     pp.CurrentPlanet,
					"destinationPlanet": "",
					"ready":             false,
					"endGame":           false,
					"fuel":              0,
					"inTransit":         false,
					"transitFrom":       "",
					"transitRemaining":  0,
					"transitTotal":      0,
					"capacity":          shipCapacity + pp.CapacityBonus,
					"fuelCapacity":      fuelCapacity + pp.FuelCapacityBonus,
					"speedPerTurn":      20 + pp.SpeedBonus,
					"modal":             nm,
				},
				"visiblePlanet": map[string]interface{}{},
			}
			if pp.conn != nil {
				recipients = append(recipients, pp)
			}
			continue
		}
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
			// copy prices map to avoid concurrent mutation during encoding
			visPrices := map[string]int{}
			for k, v := range planet.Prices {
				visPrices[k] = v
			}
			// attach static price ranges for each visible good
			ranges := defaultPriceRanges()
			visRanges := map[string][2]int{}
			for g := range visGoods {
				if r, ok := ranges[g]; ok {
					visRanges[g] = r
				}
			}
			visible = map[string]interface{}{
				"name":        planet.Name,
				"goods":       visGoods,
				"prices":      visPrices,
				"priceRanges": visRanges,
				"fuelPrice":   planet.FuelPrice,
			}
		}
		var nextModal map[string]interface{}
		if len(pp.Modals) > 0 {
			nm := map[string]interface{}{"id": pp.Modals[0].ID, "title": pp.Modals[0].Title, "body": pp.Modals[0].Body}
			if pp.Modals[0].Kind != "" {
				nm["kind"] = pp.Modals[0].Kind
			}
			if pp.Modals[0].Price != 0 {
				nm["price"] = pp.Modals[0].Price
			}
			if pp.Modals[0].CapacityBonus != 0 {
				nm["capacityBonus"] = pp.Modals[0].CapacityBonus
			}
			nextModal = nm
		} else {
			nextModal = map[string]interface{}{}
		}
		payloadByPlayer[id] = map[string]interface{}{
			"room": map[string]interface{}{
				"id":         room.ID,
				"name":       room.Name,
				"started":    room.Started,
				"turn":       room.Turn,
				"players":    players,
				"turnEndsAt": room.TurnEndsAt.UnixMilli(),
				"allReady":   allReady,
				"planets": func() []string {
					if len(room.PlanetOrder) > 0 {
						out := make([]string, len(room.PlanetOrder))
						copy(out, room.PlanetOrder)
						return out
					}
					return planetNames(room.Planets)
				}(),
				"planetPositions": func() map[string]map[string]float64 {
					if len(room.PlanetPositions) == 0 {
						return nil
					}
					out := make(map[string]map[string]float64, len(room.PlanetPositions))
					for k, v := range room.PlanetPositions {
						out[k] = map[string]float64{"x": v[0], "y": v[1]}
					}
					return out
				}(),
				"news": func() []map[string]interface{} {
					arr := make([]map[string]interface{}, 0, len(room.News))
					for _, n := range room.News {
						arr = append(arr, map[string]interface{}{
							"headline":       n.Headline,
							"planet":         n.Planet,
							"turnsRemaining": n.TurnsRemaining,
						})
					}
					return arr
				}(),
			},
			"you": map[string]interface{}{
				"id":                pp.ID,
				"name":              pp.Name,
				"money":             pp.Money,
				"inventory":         cloneIntMap(pp.Inventory),
				"inventoryAvgCost":  cloneIntMap(pp.InventoryAvgCost),
				"currentPlanet":     pp.CurrentPlanet,
				"destinationPlanet": pp.DestinationPlanet,
				"ready":             pp.Ready,
				"endGame":           pp.EndGame,
				"fuel":              pp.Fuel,
				"inTransit":         pp.InTransit,
				"transitFrom":       pp.TransitFrom,
				"transitRemaining":  pp.TransitRemaining,
				"transitTotal":      pp.TransitTotal,
				"capacity":          shipCapacity + pp.CapacityBonus,
				"fuelCapacity":      fuelCapacity + pp.FuelCapacityBonus,
				"speedPerTurn":      20 + pp.SpeedBonus,
				"modal":             nextModal,
			},
			"visiblePlanet": visible,
		}
		if pp.conn != nil {
			recipients = append(recipients, pp)
		}
	}
	room.mu.Unlock()

	if only != nil && only.conn != nil {
		only.writeMu.Lock()
		only.conn.WriteJSON(WSOut{Type: "roomState", Payload: payloadByPlayer[only.ID]})
		only.writeMu.Unlock()
		return
	}
	for _, pp := range recipients {
		pp.writeMu.Lock()
		pp.conn.WriteJSON(WSOut{Type: "roomState", Payload: payloadByPlayer[pp.ID]})
		pp.writeMu.Unlock()
	}
}

func (gs *GameServer) broadcastRoom(room *Room) { gs.sendRoomState(room, nil) }

// closeRoom ejects all players to the lobby and deletes the room
func (gs *GameServer) closeRoom(roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}

	// Signal the ticker to stop
	room.mu.Lock()
	select {
	case room.closeCh <- struct{}{}:
	default:
	}
	room.mu.Unlock()

	// Snapshot players to notify after deletion
	room.mu.Lock()
	players := make([]*Player, 0, len(room.Players))
	for _, pl := range room.Players {
		players = append(players, pl)
	}
	room.mu.Unlock()

	// Remove room from registry
	gs.roomsMu.Lock()
	delete(gs.rooms, roomID)
	gs.roomsMu.Unlock()

	// Reset player state and send them to lobby
	for _, pl := range players {
		pl.roomID = ""
		pl.InTransit = false
		pl.DestinationPlanet = ""
		pl.TransitFrom = ""
		pl.TransitRemaining = 0
		pl.TransitTotal = 0
		pl.Ready = false
		pl.EndGame = false
		if pl.conn != nil {
			gs.sendLobbyState(pl)
		}
	}
}

func planetNames(m map[string]*Planet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func defaultPlanets() map[string]*Planet {
	// All 8 planets + a few stations
	names := []string{"Mercury", "Venus", "Earth", "Mars", "Jupiter", "Saturn", "Uranus", "Neptune", "Pluto Station", "Titan Station", "Ceres Station"}
	// Standard goods produced broadly (Fuel is not a trade good)
	standard := []string{
		"Sky Kelp",
		"Moon Ferns",
		"Desalinated Sodium",
		"Reticulated Splines",
		"Zero-G Noodles",
		"Quantum Bubblegum",
		"Cosmic Coffee Beans",
		"Nano Lint",
	}
	// Unique per-location goods (whimsical)
	uniqueByLoc := map[string][]string{
		"Mercury":       {"Cyber Toasters", "Photon Socks"},
		"Venus":         {"Extradimensional Sea Monkeys", "Nebula Nectar"},
		"Earth":         {"Depleted Clown Shoes", "Holographic Honey"},
		"Mars":          {"Martian Dust Bunnies", "Laser Lemons"},
		"Jupiter":       {"Stellar Marshmallows", "Gamma Grit"},
		"Saturn":        {"Plasma Donuts", "Ring Popcorn"},
		"Uranus":        {"Anti-Gravity Paperclips", "Void Raisins"},
		"Neptune":       {"Galactic Jelly", "Comet Cotton Candy"},
		"Pluto Station": {"Wormhole Licorice", "Singularity Seeds"},
		"Titan Station": {"Orbital Oregano", "Alien Hot Sauce"},
		"Ceres Station": {"Rocket Rations", "Chrono Crystals"},
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

	// Static price ranges per good
	ranges := defaultPriceRanges()

	m := map[string]*Planet{}
	for _, n := range names {
		goods := map[string]int{}
		prices := map[string]int{}
		prod := map[string]int{}
		trend := map[string]int{}
		for _, g := range standard {
			goods[g] = 20 + rand.Intn(30)
			if r, ok := ranges[g]; ok {
				prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
			} else {
				prices[g] = 10 + rand.Intn(15)
			}
			prod[g] = 2 + rand.Intn(4) // 2-5 per turn
			trend[g] = 0
		}
		for _, g := range uniqueByLoc[n] {
			goods[g] = 10 + rand.Intn(20)
			prod[g] = 1 + rand.Intn(3) // 1-3 per turn
			trend[g] = 0
		}
		// ensure price exists for every good to allow selling anywhere
		for _, g := range allGoods {
			if _, ok := prices[g]; !ok {
				if r, ok := ranges[g]; ok {
					prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
				} else {
					prices[g] = 8 + rand.Intn(25)
				}
				if _, ok := trend[g]; !ok {
					trend[g] = 0
				}
			}
		}
		// Keep baselines for dynamic news effects
		basePrices := make(map[string]int, len(prices))
		for k, v := range prices {
			basePrices[k] = v
		}
		baseProd := make(map[string]int, len(prod))
		for k, v := range prod {
			baseProd[k] = v
		}
		// Initialize separate per-planet ship fuel price (~$10 average)
		fp := 8 + rand.Intn(5) // 8..12
		m[n] = &Planet{Name: n, Goods: goods, Prices: prices, Prod: prod, BasePrices: basePrices, BaseProd: baseProd, PriceTrend: trend, FuelPrice: fp, BaseFuelPrice: fp}
	}
	return m
}

// defaultPriceRanges returns a static min/max range for each good.
// Standard goods have a slightly lower range; unique goods are a bit higher.
func defaultPriceRanges() map[string][2]int {
	// Keep canonical lists in a fixed order for deterministic ranges
	standard := []string{
		"Sky Kelp",
		"Moon Ferns",
		"Desalinated Sodium",
		"Reticulated Splines",
		"Zero-G Noodles",
		"Quantum Bubblegum",
		"Cosmic Coffee Beans",
		"Nano Lint",
	}
	unique := []string{
		"Cyber Toasters",
		"Extradimensional Sea Monkeys",
		"Depleted Clown Shoes",
		"Photon Socks",
		"Nebula Nectar",
		"Holographic Honey",
		"Martian Dust Bunnies",
		"Laser Lemons",
		"Stellar Marshmallows",
		"Gamma Grit",
		"Plasma Donuts",
		"Ring Popcorn",
		"Anti-Gravity Paperclips",
		"Void Raisins",
		"Galactic Jelly",
		"Comet Cotton Candy",
		"Wormhole Licorice",
		"Singularity Seeds",
		"Orbital Oregano",
		"Alien Hot Sauce",
		"Rocket Rations",
		"Chrono Crystals",
	}
	m := map[string][2]int{}
	// Generate a unique [min,max] for every good using a deterministic pattern.
	// We ensure uniqueness by giving each good a distinct min; widths vary slightly.
	combined := make([]string, 0, len(standard)+len(unique))
	combined = append(combined, standard...)
	combined = append(combined, unique...)
	for idx, g := range combined {
		min := 5 + idx          // strictly increasing min ensures unique ranges
		width := 16 + (idx % 7) // widths between 16..22 to add variety
		max := min + width
		m[g] = [2]int{min, max}
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

func cloneIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneModals(in []ModalItem) []ModalItem {
	if in == nil {
		return nil
	}
	out := make([]ModalItem, len(in))
	copy(out, in)
	return out
}

// cloneActionHistory returns a shallow copy of the action history slice.
// ActionLog is immutable for our purposes (just Turn and Text), so shallow copy is fine.
func cloneActionHistory(in []ActionLog) []ActionLog {
	if in == nil {
		return nil
	}
	out := make([]ActionLog, len(in))
	copy(out, in)
	return out
}

func (gs *GameServer) enqueueModal(p *Player, title, body string) {
	// Do not enqueue new modals once a player is bankrupt; they can still review existing queue.
	if p == nil || p.Bankrupt {
		return
	}
	mi := ModalItem{ID: randID(), Title: title, Body: body}
	p.Modals = append(p.Modals, mi)
}

// inventoryTotal returns the sum of all units across goods
func inventoryTotal(inv map[string]int) int {
	if inv == nil {
		return 0
	}
	total := 0
	for _, v := range inv {
		total += v
	}
	return total
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func clampInt(v, lo, hi int) int { return maxInt(lo, minInt(v, hi)) }

// generatePlanetPositions returns normalized positions in [0,1]x[0,1] with a minimal spacing
func generatePlanetPositions(names []string) map[string][2]float64 {
	m := make(map[string][2]float64, len(names))
	if len(names) == 0 {
		return m
	}
	minDist := 0.18 // minimal distance between planets (normalized)
	margin := 0.08  // keep away from edges
	placed := make([][2]float64, 0, len(names))
	for _, n := range names {
		var x, y float64
		ok := false
		for tries := 0; tries < 200; tries++ {
			x = margin + rand.Float64()*(1-2*margin)
			y = margin + rand.Float64()*(1-2*margin)
			good := true
			for _, p := range placed {
				dx := p[0] - x
				dy := p[1] - y
				if math.Hypot(dx, dy) < minDist {
					good = false
					break
				}
			}
			if good {
				ok = true
				break
			}
		}
		if !ok {
			// fallback without spacing guarantee
			x = margin + rand.Float64()*(1-2*margin)
			y = margin + rand.Float64()*(1-2*margin)
		}
		placed = append(placed, [2]float64{x, y})
		m[n] = [2]float64{x, y}
	}
	return m
}

// distanceUnits computes integer travel cost between two planets using normalized positions.
// Returns at least 1 for distinct planets; 0 if names are empty or same.
func distanceUnits(room *Room, from, to string) int {
	if from == "" || to == "" || from == to {
		return 0
	}
	pos := room.PlanetPositions
	if len(pos) == 0 {
		// fallback fixed cost when positions unknown
		return 5
	}
	a, okA := pos[from]
	b, okB := pos[to]
	if !okA || !okB {
		return 5
	}
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	d := math.Hypot(dx, dy)
	// scale normalized distance (~0..1.4) to a reasonable unit cost range
	// Use ceil to make longer hops cost a bit more; ensure minimum 1
	units := int(math.Ceil(d * 40))
	if units < 1 {
		units = 1
	}
	return units
}

// handleRefuel processes a refuel request for the player.
// amount<=0 means "fill to max you can afford and capacity".
func (gs *GameServer) handleRefuel(room *Room, p *Player, amount int) {
	room.mu.Lock()
	defer func() { room.mu.Unlock(); gs.sendRoomState(room, p) }()
	if amount < 0 {
		amount = 0
	}
	planet := room.Planets[p.CurrentPlanet]
	price := 10
	if planet != nil {
		if planet.FuelPrice > 0 {
			price = planet.FuelPrice
		}
	}
	maxCap := (fuelCapacity + p.FuelCapacityBonus) - p.Fuel
	if maxCap <= 0 {
		return
	}
	if amount == 0 || amount > maxCap {
		amount = maxCap
	}
	if price <= 0 {
		price = 10
	}
	maxByMoney := 0
	if price > 0 {
		maxByMoney = p.Money / price
	}
	if amount > maxByMoney {
		amount = maxByMoney
	}
	if amount <= 0 {
		return
	}
	cost := amount * price
	p.Money -= cost
	p.Fuel += amount
	if amount > 0 && cost > 0 {
		gs.logAction(room, p, fmt.Sprintf("Purchased %d fuel for $%d", amount, cost))
	}
}
