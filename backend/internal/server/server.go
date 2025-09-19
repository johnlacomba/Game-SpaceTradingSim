package server

import (
	"encoding/json"
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
	Modals            []ModalItem     `json:"-"`
	Fuel              int             `json:"fuel"`
	conn              *websocket.Conn // not serialized
	roomID            string          // not serialized
	IsBot             bool            `json:"-"`
	writeMu           sync.Mutex      // guards conn writes
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
}

// NewsItem represents a temporary room-wide event affecting a planet's prices/production
type NewsItem struct {
	Headline       string         `json:"headline"`
	Planet         string         `json:"planet"`
	PriceDelta     map[string]int `json:"priceDelta,omitempty"`
	ProdDelta      map[string]int `json:"prodDelta,omitempty"`
	TurnsRemaining int            `json:"turnsRemaining"`
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
}

// PersistedPlayer stores the subset of player state we want to keep per-room for rejoin
type PersistedPlayer struct {
	Money             int
	CurrentPlanet     string
	DestinationPlanet string
	Inventory         map[string]int
	InventoryAvgCost  map[string]int
	Ready             bool
	Modals            []ModalItem
	Fuel              int
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
					Modals:            cloneModals(p.Modals),
					Fuel:              p.Fuel,
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
				if len(room.PlanetPositions) > 0 && data.Planet != "" && data.Planet != p.CurrentPlanet {
					cost := distanceUnits(room, p.CurrentPlanet, data.Planet)
					if cost > p.Fuel {
						allow = false
						gs.enqueueModal(p, "Insufficient Fuel", "You don't have enough fuel to reach "+data.Planet+".")
					}
				}
				if allow {
					p.DestinationPlanet = data.Planet
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
		delete(room.Persist, p.ID)
	} else {
		p.CurrentPlanet = "Earth"
		p.DestinationPlanet = ""
		p.Ready = false
		if p.Fuel <= 0 {
			p.Fuel = fuelCapacity
		}
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
			// Randomize planet order for UI layout
			names := planetNames(room.Planets)
			for i := range names {
				j := rand.Intn(i + 1)
				names[i], names[j] = names[j], names[i]
			}
			room.PlanetOrder = names
			// Generate random positions (normalized 0..1) with spacing
			room.PlanetPositions = generatePlanetPositions(names)
			room.Started = true
			room.Turn = 0
			room.TurnEndsAt = time.Now().Add(turnDuration)
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
		room.mu.Unlock()

		if onlyBots {
			// Skip the long timer; tiny pause to avoid a tight CPU loop
			time.Sleep(150 * time.Millisecond)
		} else {
			// Wait for either timer or early ready signal
			timer := time.NewTimer(base)
			select {
			case <-timer.C:
				// time-based turn end
			case <-room.readyCh:
				// early turn end due to all humans ready
				if !timer.Stop() {
					<-timer.C
				}
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
		// First, recompute from baselines
		for _, pl := range room.Planets {
			for g, v := range pl.BasePrices {
				pl.Prices[g] = v
			}
			for g, v := range pl.BaseProd {
				pl.Prod[g] = v
			}
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
			if p.DestinationPlanet != "" && p.DestinationPlanet != p.CurrentPlanet {
				moved := false
				if _, ok := room.Planets[p.DestinationPlanet]; ok {
					cost := distanceUnits(room, p.CurrentPlanet, p.DestinationPlanet)
					if cost <= p.Fuel {
						p.Fuel -= cost
						p.CurrentPlanet = p.DestinationPlanet
						moved = true
					}
				}
				if !moved {
					if !p.IsBot {
						gs.enqueueModal(p, "Insufficient Fuel", "You didn't have enough fuel to make the trip.")
					}
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
		// simple bot AI: sell > $10, buy <= $10, pick a new random destination
		for _, bp := range room.Players {
			if !bp.IsBot {
				continue
			}
			planet := room.Planets[bp.CurrentPlanet]
			if planet == nil {
				continue
			}
			// sell everything profitable (>10)
			for g, qty := range bp.Inventory {
				price := planet.Prices[g]
				if qty > 0 && price > 10 {
					bp.Inventory[g] -= qty
					planet.Goods[g] += qty
					bp.Money += qty * price
					if bp.Inventory[g] <= 0 {
						delete(bp.Inventory, g)
						delete(bp.InventoryAvgCost, g)
					}
				}
			}
			// buy anything cheap (<=10)
			keys := make([]string, 0, len(planet.Goods))
			for k := range planet.Goods {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, g := range keys {
				price := planet.Prices[g]
				if price <= 0 || price > 10 {
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
				free := shipCapacity - used
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
			}
			// Bot refuel behavior (uses local planet Fuel price)
			if bp.Fuel < 20 {
				price := planet.Prices["Fuel"]
				if price <= 0 {
					price = 10
				}
				maxUnits := minInt(fuelCapacity-bp.Fuel, bp.Money/price)
				if maxUnits > 0 {
					bp.Money -= maxUnits * price
					bp.Fuel += maxUnits
				}
			}
			// choose a new destination different from current within fuel range
			pn := planetNames(room.Planets)
			if len(pn) > 1 {
				for tries := 0; tries < 8; tries++ {
					dest := pn[rand.Intn(len(pn))]
					if dest == bp.CurrentPlanet {
						continue
					}
					if distanceUnits(room, bp.CurrentPlanet, dest) <= bp.Fuel {
						bp.DestinationPlanet = dest
						break
					}
				}
			}
		}
		// Rare per-player events (humans only): tax, lottery, asteroid
		for _, hp := range room.Players {
			if hp.IsBot {
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
				gs.enqueueModal(hp, "Federation Tax", "Federation income tax is due. "+strconv.Itoa(tax)+" credits due.")
			}
			// Lottery win: ~1% chance per turn
			if rand.Intn(100) == 0 {
				amt := 500 + rand.Intn(10000-500+1)
				hp.Money += amt
				gs.enqueueModal(hp, "Lottery Winner!", "You won the lottery and collect "+strconv.Itoa(amt)+" credits!")
			}
			// Asteroid collision: ~1% chance per turn
			if rand.Intn(100) == 0 {
				// Lose all cargo
				hp.Inventory = map[string]int{}
				hp.InventoryAvgCost = map[string]int{}
				gs.enqueueModal(hp, "Asteroid Collision", "Your ship collided with an asteroid and you lost all cargo.")
			}
		}
		// reset human players' ready flags for the new turn
		for _, pl := range room.Players {
			if !pl.IsBot {
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
		// 20% chance to generate a Fuel price headline (price up/down for Fuel on a planet)
		if rand.Intn(5) == 0 {
			ni := NewsItem{Planet: planet, TurnsRemaining: turns}
			// fuel price delta +/- 2-5 credits
			delta := 2 + rand.Intn(4)
			if rand.Intn(2) == 0 {
				delta = -delta
			}
			ni.PriceDelta = map[string]int{"Fuel": delta}
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
	free := shipCapacity - used
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
	if p.Inventory[good] <= 0 {
		delete(p.Inventory, good)
		delete(p.InventoryAvgCost, good)
	}
}

func (gs *GameServer) sendRoomState(room *Room, only *Player) {
	// prepare minimal view per-player (fog of goods for current planet only)
	room.mu.Lock()
	// compute whether all non-bot players are ready
	allReady := true
	for _, pp := range room.Players {
		if pp.IsBot {
			continue
		}
		if !pp.Ready {
			allReady = false
			break
		}
	}
	players := []map[string]interface{}{}
	for _, pp := range room.Players {
		players = append(players, map[string]interface{}{
			"id":                pp.ID,
			"name":              pp.Name,
			"money":             pp.Money,
			"currentPlanet":     pp.CurrentPlanet,
			"destinationPlanet": pp.DestinationPlanet,
			"ready":             pp.Ready,
		})
	}
	payloadByPlayer := map[PlayerID]interface{}{}
	recipients := make([]*Player, 0, len(room.Players))
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
			}
		}
		nextModal := map[string]string{}
		if len(pp.Modals) > 0 {
			nextModal = map[string]string{"id": pp.Modals[0].ID, "title": pp.Modals[0].Title, "body": pp.Modals[0].Body}
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
				"fuel":              pp.Fuel,
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
			// Nudge initial Fuel price to ~ $10 on average at start (per-planet variation 8–12)
			if g == "Fuel" {
				prices[g] = 8 + rand.Intn(5) // 8..12
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
		m[n] = &Planet{Name: n, Goods: goods, Prices: prices, Prod: prod, BasePrices: basePrices, BaseProd: baseProd, PriceTrend: trend}
	}
	return m
}

// defaultPriceRanges returns a static min/max range for each good.
// Standard goods have a slightly lower range; unique goods are a bit higher.
func defaultPriceRanges() map[string][2]int {
	standard := []string{"Food", "Ore", "Water", "Fuel"}
	unique := []string{"Solar Panels", "Acid Extract", "Electronics", "Iron Alloy", "Helium-3", "Methane", "Ice Crystals", "Deep Blue Dye", "Xenon Gas", "Titan Spice", "Rare Metals"}
	m := map[string][2]int{}
	for _, g := range standard {
		m[g] = [2]int{5, 24}
	}
	for _, g := range unique {
		m[g] = [2]int{8, 32}
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

func (gs *GameServer) enqueueModal(p *Player, title, body string) {
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
	if planet != nil && planet.Prices != nil {
		if planet.Prices["Fuel"] > 0 {
			price = planet.Prices["Fuel"]
		}
	}
	maxCap := fuelCapacity - p.Fuel
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
}
