package server

import (
	"encoding/json"
	"log"
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
	conn              *websocket.Conn // not serialized
	roomID            string          // not serialized
	IsBot             bool            `json:"-"`
	writeMu           sync.Mutex      // guards conn writes
}

type Room struct {
	ID         string                        `json:"id"`
	Name       string                        `json:"name"`
	Started    bool                          `json:"started"`
	Players    map[PlayerID]*Player          `json:"players"`
	Turn       int                           `json:"turn"`
	Planets    map[string]*Planet            `json:"planets"`
	Persist    map[PlayerID]*PersistedPlayer `json:"-"`
	mu         sync.Mutex
	readyCh    chan struct{} // signal to end turn early when all humans are ready
	TurnEndsAt time.Time     `json:"-"`
	News       []NewsItem    `json:"-"`
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
}

// PersistedPlayer stores the subset of player state we want to keep per-room for rejoin
type PersistedPlayer struct {
	Money             int
	CurrentPlanet     string
	DestinationPlanet string
	Inventory         map[string]int
	InventoryAvgCost  map[string]int
	Ready             bool
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
				}
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
		delete(room.Persist, p.ID)
	} else {
		p.CurrentPlanet = "Earth"
		p.DestinationPlanet = ""
		p.Ready = false
	}
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
	}
	delete(room.Players, p.ID)
	p.roomID = ""
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
		// Process end-of-turn effects and start the next turn
		room.mu.Lock()
		if !room.Started {
			room.mu.Unlock()
			return
		}
		room.Turn++
		// new turn begins; set the next deadline and reset human ready
		room.TurnEndsAt = time.Now().Add(base)
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
		// Randomly generate 0-2 news items per turn
		gs.generateNews(room)
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
				amount := bp.Money / price
				if amount > avail {
					amount = avail
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
			// choose a new destination different from current
			pn := planetNames(room.Planets)
			if len(pn) > 1 {
				for tries := 0; tries < 5; tries++ {
					dest := pn[rand.Intn(len(pn))]
					if dest != bp.CurrentPlanet {
						bp.DestinationPlanet = dest
						break
					}
				}
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
			}
			txt := flavor[rand.Intn(len(flavor))]
			txt = strings.ReplaceAll(txt, "{planet}", planet)
			ni := NewsItem{Planet: planet, TurnsRemaining: turns, Headline: txt}
			room.News = append(room.News, ni)
			continue
		}
		// random effect type: price up/down or prod up/down
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
		ni.Headline = headline + " (" + strconv.Itoa(turns) + " turns)"
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
				"prices": visPrices,
				"priceRanges": visRanges,
			}
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
				"planets":    planetNames(room.Planets),
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
		for _, g := range standard {
			goods[g] = 20 + rand.Intn(30)
			if r, ok := ranges[g]; ok {
				prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
			} else {
				prices[g] = 10 + rand.Intn(15)
			}
			prod[g] = 2 + rand.Intn(4) // 2-5 per turn
		}
		for _, g := range uniqueByLoc[n] {
			goods[g] = 10 + rand.Intn(20)
			prod[g] = 1 + rand.Intn(3) // 1-3 per turn
		}
		// ensure price exists for every good to allow selling anywhere
		for _, g := range allGoods {
			if _, ok := prices[g]; !ok {
				if r, ok := ranges[g]; ok {
					prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
				} else {
					prices[g] = 8 + rand.Intn(25)
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
		m[n] = &Planet{Name: n, Goods: goods, Prices: prices, Prod: prod, BasePrices: basePrices, BaseProd: baseProd}
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
