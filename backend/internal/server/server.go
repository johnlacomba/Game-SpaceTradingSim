package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/space-trader/internal/auth"
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
const maxFacilitiesPerPlanet = 3

const (
	worldWidth             = 5200.0
	worldHeight            = 5200.0
	worldPlanetCount       = 100
	minPlanetSpacingUnits  = 240.0
	discoveryRadiusStation = 650.0
	discoveryRadiusTransit = 500.0
	distanceUnitScale      = 160.0
)

// Bot names for variety
var botNames = []string{
	"Captain Nova", "Admiral Stardust", "Commander Vega", "Captain Nebula", "Admiral Comet",
	"Captain Orion", "Commander Stellar", "Admiral Cosmos", "Captain Quasar", "Commander Galaxy",
	"Captain Aurora", "Admiral Phoenix", "Commander Astro", "Captain Meteor", "Admiral Solar",
	"Captain Luna", "Commander Titan", "Admiral Helios", "Captain Andromeda", "Commander Pulsar",
	"Captain Rigel", "Admiral Draco", "Commander Sirius", "Captain Vortex", "Admiral Eclipse",
	"Captain Zenith", "Commander Nexus", "Admiral Infinity", "Captain Paradox", "Commander Flux",
}

var locationPrefixes = []string{
	"Astra", "Nebula", "Ion", "Vanta", "Cypher", "Luma", "Nova", "Zetra", "Omni", "Spectra",
	"Proto", "Velora", "Lucid", "Hyper", "Quanta", "Delta", "Zerra", "Krios", "Lyra", "Helio",
	"Solace", "Nyx", "Umbra", "Epsilon", "Omega", "Cobalt", "Axia", "Zenith", "Flux", "Obsidia",
}

var locationCores = []string{
	"Arc", "Crown", "Drift", "Spire", "Reach", "Horizon", "Bastion", "Nexus", "Harbor", "Forge",
	"Haven", "Citadel", "Ridge", "Hollow", "Gate", "Lattice", "Field", "Matrix", "Rift", "Anchor",
	"Veil", "Vault", "Fathom", "Chorus", "Beacon", "Circuit", "Array", "Orbit", "Signal", "Pulse",
}

var locationSuffixes = []string{
	"Station", "Outpost", "Port", "Relay", "Hold", "Terminal", "Sanctum", "Depot", "Garrison", "Anchor",
	"Dome", "Colony", "Platform", "Span", "Cluster", "Axis", "Stronghold", "Bulwark", "Archive", "Keep",
	"Dock", "Observatory", "Node", "Vault", "Garden", "Shell", "Cove", "Harbor", "Breach", "Mezzanine",
}

var locationQualifiers = []string{
	"Prime", "Sigma", "Omega", "Theta", "V", "VII", "Alpha", "Rho", "Lambda", "Pulse",
	"Cinder", "Aurora", "Halo", "Specter", "Zen", "Flux", "Parallel", "Eclipse", "Mirage", "Lumen",
}

var standardGoods = []string{
	"Sky Kelp",
	"Moon Ferns",
	"Desalinated Sodium",
	"Reticulated Splines",
	"Zero-G Noodles",
	"Quantum Bubblegum",
	"Cosmic Coffee Beans",
	"Nano Lint",
}

var uniqueGoodsPool = []string{
	"Cyber Toasters",
	"Photon Socks",
	"Extradimensional Sea Monkeys",
	"Nebula Nectar",
	"Depleted Clown Shoes",
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
	"Aurora Spice",
	"Solar Syrup",
	"Plasma Pomegranates",
	"Ionized Ice Cream",
	"Nebula Nougat",
	"Gravimetric Granola",
	"Quantum Quinoa",
	"Void Vanilla Beans",
	"Stasis Sesame",
	"Phase Fudge",
	"Auric Anchovies",
	"Lunar Licorice",
	"Perigee Peppers",
	"Aphelion Almonds",
	"Flux Fennel",
	"Zenith Ziti",
	"Kaleido Kale",
	"Pulsar Pistachios",
	"Glacier Gelato",
	"Radiant Ramen",
	"Echo Espresso Pods",
	"Prism Pearls",
	"Nimbus Nougat",
	"Halo Hazelnut Spread",
	"Specter Spices",
	"Driftwood Dulse",
	"Plasma Pickles",
	"Nova Noodles",
	"Cobalt Cereal",
	"Frostlight Feta",
}

var alphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func sanitizeAlphanumeric(input string) string {
	if input == "" {
		return ""
	}
	return alphanumericRegex.ReplaceAllString(input, "")
}

type PlayerID string

// PriceMemory stores remembered prices from visited planets
type PriceMemory struct {
	Prices          map[string]int `json:"prices"`          // good -> price
	Turn            int            `json:"turn"`            // when this was recorded
	GoodsAvg        int            `json:"goodsAvg"`        // average availability when recorded
	LastPurchased   map[string]int `json:"lastPurchased"`   // good -> turn when last purchased here
	PurchaseAmounts map[string]int `json:"purchaseAmounts"` // good -> amount purchased last time
	VisitCount      int            `json:"visitCount"`      // how many times we've been here
	LastProfit      int            `json:"lastProfit"`      // profit/loss from last visit
	ProfitHistory   []int          `json:"profitHistory"`   // recent profit history (last 5 visits)
}

// MarketSnapshot captures the last known market state for a planet when a player visited
type MarketSnapshot struct {
	Turn        int               `json:"turn"`
	UpdatedAt   int64             `json:"updatedAt"`
	Goods       map[string]int    `json:"goods"`
	Prices      map[string]int    `json:"prices"`
	PriceRanges map[string][2]int `json:"priceRanges"`
	FuelPrice   int               `json:"fuelPrice"`
}

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
	KnownPlanets      map[string]bool `json:"-"`
	// Transit state (server-only)
	InTransit          bool   `json:"-"`
	TransitFrom        string `json:"-"`
	TransitRemaining   int    `json:"-"` // units remaining to destination along straight line
	TransitTotal       int    `json:"-"` // initial units at start of transit
	CapacityBonus      int    `json:"-"`
	SpeedBonus         int    `json:"-"`
	FuelCapacityBonus  int    `json:"-"`
	FacilityInvestment int    `json:"-"`
	UpgradeInvestment  int    `json:"-"`
	// Recent actions (last 10)
	ActionHistory []ActionLog `json:"-"`
	// Bot-specific memory (only used by bots)
	PriceMemory        map[string]*PriceMemory `json:"-"` // planet -> price data
	MarketMemory       map[string]*MarketSnapshot
	LastTripStartMoney int            `json:"-"` // money at start of current trip
	ConsecutiveVisits  map[string]int `json:"-"` // planet -> consecutive visits to track loops
}

// ActionLog captures a brief recent action for audit/history
type ActionLog struct {
	Turn int    `json:"turn"`
	Text string `json:"text"`
}

// BlackOpsContract tracks a covert action purchased by a player that will
// trigger setbacks for other players on a future turn.
type BlackOpsContract struct {
	ID          string
	Instigator  PlayerID
	TriggerTurn int
	Applied     map[PlayerID]bool
	Price       int
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
	Private         bool                  `json:"-"`
	CreatorID       PlayerID              `json:"-"`
	Paused          bool                  `json:"-"`
	stateCh         chan struct{}         `json:"-"`
	TurnEndsAt      time.Time             `json:"-"`
	News            []NewsItem            `json:"-"`
	PlanetOrder     []string              `json:"-"`
	PlanetPositions map[string][2]float64 `json:"-"`
	ActiveAuction   *FederationAuction    `json:"-"`
	PendingBlackOps []*BlackOpsContract   `json:"-"`
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
	// Auction-specific fields
	AuctionID    string `json:"auctionId,omitempty"`
	FacilityType string `json:"facilityType,omitempty"`
	Planet       string `json:"planet,omitempty"`
	UsageCharge  int    `json:"usageCharge,omitempty"`
	SuggestedBid int    `json:"suggestedBid,omitempty"`
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

type singleplayerSavePayload struct {
	Version  int                        `json:"version"`
	RoomID   string                     `json:"roomId"`
	RoomName string                     `json:"roomName"`
	OwnerID  string                     `json:"ownerId"`
	Turns    []singleplayerTurnSnapshot `json:"turns"`
}

type singleplayerTurnSnapshot struct {
	Turn       int                       `json:"turn"`
	RecordedAt int64                     `json:"recordedAt"`
	State      singleplayerStateSnapshot `json:"state"`
}

type singleplayerStateSnapshot struct {
	Room              singleplayerRoomSnapshot   `json:"room"`
	You               singleplayerPlayerSnapshot `json:"you"`
	DiscoveredPlanets []string                   `json:"discoveredPlanets,omitempty"`
}

type singleplayerRoomSnapshot struct {
	ID         string                           `json:"id"`
	Name       string                           `json:"name"`
	Started    bool                             `json:"started"`
	Turn       int                              `json:"turn"`
	Private    bool                             `json:"private"`
	Paused     bool                             `json:"paused"`
	CreatorID  string                           `json:"creatorId"`
	TurnEndsAt int64                            `json:"turnEndsAt"`
	Players    []singleplayerRoomPlayerSnapshot `json:"players"`
	News       []singleplayerNewsSnapshot       `json:"news"`
	Planets    []string                         `json:"planets"`
}

type singleplayerRoomPlayerSnapshot struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	CurrentPlanet     string `json:"currentPlanet"`
	DestinationPlanet string `json:"destinationPlanet"`
	Ready             bool   `json:"ready"`
	EndGame           bool   `json:"endGame"`
	Bankrupt          bool   `json:"bankrupt"`
	CashValue         int    `json:"cashValue"`
}

type singleplayerNewsSnapshot struct {
	Headline       string `json:"headline"`
	Planet         string `json:"planet"`
	TurnsRemaining int    `json:"turnsRemaining"`
}

type singleplayerPlayerSnapshot struct {
	ID                 string                     `json:"id"`
	Name               string                     `json:"name"`
	Money              int                        `json:"cashValue"`
	CurrentPlanet      string                     `json:"currentPlanet"`
	DestinationPlanet  string                     `json:"destinationPlanet"`
	Ready              bool                       `json:"ready"`
	EndGame            bool                       `json:"endGame"`
	Fuel               int                        `json:"fuel"`
	Inventory          map[string]int             `json:"inventory"`
	InventoryAvgCost   map[string]int             `json:"inventoryAvgCost"`
	InTransit          bool                       `json:"inTransit"`
	TransitFrom        string                     `json:"transitFrom"`
	TransitRemaining   int                        `json:"transitRemaining"`
	TransitTotal       int                        `json:"transitTotal"`
	Capacity           int                        `json:"capacity"`
	FuelCapacity       int                        `json:"fuelCapacity"`
	SpeedPerTurn       int                        `json:"speedPerTurn"`
	FacilityInvestment int                        `json:"facilityInvestment"`
	UpgradeInvestment  int                        `json:"upgradeInvestment"`
	UpgradeValue       int                        `json:"upgradeValue"`
	CargoValue         int                        `json:"cargoValue"`
	MarketMemory       map[string]*MarketSnapshot `json:"marketMemory"`
	KnownPlanets       []string                   `json:"knownPlanets"`
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
	// Facilities owned by players
	Facilities []*Facility `json:"facilities,omitempty"`
}

// Facility represents a player-owned facility at a planet
type Facility struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`  // e.g., "Mining Station", "Trade Hub", "Refinery"
	Owner         PlayerID `json:"owner"` // player who owns this facility
	OwnerName     string   `json:"ownerName"`
	UsageCharge   int      `json:"usageCharge"`  // cost per turn for non-owners
	AccruedMoney  int      `json:"accruedMoney"` // money waiting to be collected by owner
	PurchasePrice int      `json:"purchasePrice"`
}

// FederationAuction represents an active facility auction
type FederationAuction struct {
	ID           string           `json:"id"`
	FacilityType string           `json:"facilityType"`
	Planet       string           `json:"planet"`
	UsageCharge  int              `json:"usageCharge"`
	SuggestedBid int              `json:"suggestedBid"`
	Bids         map[PlayerID]int `json:"bids"`
	TurnsLeft    int              `json:"turnsLeft"`
}

// PersistedPlayer stores the subset of player state we want to keep per-room for rejoin
type PersistedPlayer struct {
	Money              int
	CurrentPlanet      string
	DestinationPlanet  string
	Inventory          map[string]int
	InventoryAvgCost   map[string]int
	Ready              bool
	EndGame            bool
	Modals             []ModalItem
	Fuel               int
	Bankrupt           bool
	InTransit          bool
	TransitFrom        string
	TransitRemaining   int
	TransitTotal       int
	CapacityBonus      int
	SpeedBonus         int
	FuelCapacityBonus  int
	ActionHistory      []ActionLog
	FacilityInvestment int
	UpgradeInvestment  int
	MarketMemory       map[string]*MarketSnapshot
	KnownPlanets       map[string]bool
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
func (gs *GameServer) HandleWS(w http.ResponseWriter, r *http.Request, cognitoConfig *auth.CognitoConfig) {
	// Try to authenticate WebSocket connection via query parameter or header
	var userClaims *auth.UserClaims
	var err error

	// Check for token in query parameter first (easier for WebSocket clients)
	tokenParam := r.URL.Query().Get("token")
	if tokenParam != "" {
		userClaims, err = cognitoConfig.ValidateToken(tokenParam)
		if err != nil {
			log.Printf("WebSocket auth failed via query param: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	} else {
		// Check Authorization header as fallback
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString != authHeader {
				userClaims, err = cognitoConfig.ValidateToken(tokenString)
				if err != nil {
					log.Printf("WebSocket auth failed via header: %v", err)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
	}

	if userClaims == nil {
		log.Printf("WebSocket connection attempted without valid authentication")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := gs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}

	// Create player with authenticated user info
	p := &Player{
		ID:                PlayerID(userClaims.Sub), // Use Cognito user ID
		Name:              userClaims.Name,
		Money:             1000,
		CurrentPlanet:     "Earth",
		DestinationPlanet: "",
		Inventory:         map[string]int{},
		InventoryAvgCost:  map[string]int{},
		Fuel:              fuelCapacity,
		PriceMemory:       make(map[string]*PriceMemory),
		MarketMemory:      make(map[string]*MarketSnapshot),
	}
	p.conn = conn
	go gs.readLoop(p)
}

func (gs *GameServer) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	userClaims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	profile := map[string]interface{}{
		"id":       userClaims.Sub,
		"name":     userClaims.Name,
		"email":    userClaims.Email,
		"username": userClaims.Username,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
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
	var data struct {
		Name         string `json:"name"`
		Singleplayer bool   `json:"singleplayer"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil && err != io.EOF {
			log.Printf("createRoom decode error: %v", err)
		}
	}
	data.Name = sanitizeAlphanumeric(data.Name)
	room := gs.createRoom(data.Name, "", data.Singleplayer)
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
					Money:              p.Money,
					CurrentPlanet:      p.CurrentPlanet,
					DestinationPlanet:  p.DestinationPlanet,
					Inventory:          cloneIntMap(p.Inventory),
					InventoryAvgCost:   cloneIntMap(p.InventoryAvgCost),
					Ready:              p.Ready,
					EndGame:            p.EndGame,
					Modals:             cloneModals(p.Modals),
					Fuel:               p.Fuel,
					Bankrupt:           p.Bankrupt,
					InTransit:          p.InTransit,
					TransitFrom:        p.TransitFrom,
					TransitRemaining:   p.TransitRemaining,
					TransitTotal:       p.TransitTotal,
					CapacityBonus:      p.CapacityBonus,
					SpeedBonus:         p.SpeedBonus,
					FuelCapacityBonus:  p.FuelCapacityBonus,
					ActionHistory:      cloneActionHistory(p.ActionHistory),
					FacilityInvestment: p.FacilityInvestment,
					UpgradeInvestment:  p.UpgradeInvestment,
					MarketMemory:       cloneMarketMemory(p.MarketMemory),
					KnownPlanets:       cloneBoolMap(p.KnownPlanets),
				}
				delete(room.Players, p.ID)
				p.roomID = ""

				isEmpty := len(room.Players) == 0
				isCreator := room.CreatorID != "" && room.CreatorID == p.ID
				keepAlive := room.Private && isCreator
				if keepAlive {
					room.Paused = true
					room.TurnEndsAt = time.Time{}
					select {
					case room.stateCh <- struct{}{}:
					default:
					}
				}

				if room.Started && !keepAlive {
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

				if keepAlive {
					gs.broadcastRoom(room)
				} else if isEmpty {
					gs.roomsMu.Lock()
					delete(gs.rooms, room.ID)
					gs.roomsMu.Unlock()
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
		case "ping":
			// Respond to ping with pong for connection keepalive
			p.writeMu.Lock()
			err := p.conn.WriteJSON(WSOut{Type: "pong"})
			p.writeMu.Unlock()
			if err != nil {
				log.Printf("Error sending pong to player %s: %v", p.ID, err)
				return
			}
		case "connect":
			// payload: {name}
			var data struct {
				Name string `json:"name"`
			}
			json.Unmarshal(msg.Payload, &data)
			cleanName := sanitizeAlphanumeric(data.Name)
			if cleanName == "" {
				cleanName = "Player " + string(p.ID[len(p.ID)-4:])
			}
			p.Name = cleanName
			// send lobby state
			gs.sendLobbyState(p)
		case "listRooms":
			gs.sendLobbyState(p)
		case "createRoom":
			var data struct {
				Name         string `json:"name"`
				Singleplayer bool   `json:"singleplayer"`
			}
			if len(msg.Payload) > 0 {
				if err := json.Unmarshal(msg.Payload, &data); err != nil {
					log.Printf("createRoom payload decode error: %v", err)
				}
			}
			data.Name = sanitizeAlphanumeric(data.Name)
			room := gs.createRoom(data.Name, p.ID, data.Singleplayer)
			gs.joinRoom(p, room.ID)
		case "joinRoom":
			var data struct {
				RoomID string `json:"roomId"`
			}
			json.Unmarshal(msg.Payload, &data)
			if data.RoomID != "" {
				gs.joinRoom(p, data.RoomID)
			}
		case "restoreSingleplayer":
			gs.handleRestoreSingleplayer(p, msg.Payload)
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
							p.UpgradeInvestment += m.Price
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
							p.UpgradeInvestment += price
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
							p.UpgradeInvestment += price
							gs.enqueueModal(p, "Fuel Tank Expanded", "Your fuel capacity increased by "+strconv.Itoa(m.Units)+" to "+strconv.Itoa(fuelCapacity+p.FuelCapacityBonus)+".")
							gs.logAction(room, p, fmt.Sprintf("Purchased fuel tank +%d for $%d", m.Units, price))
						} else {
							gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits for this upgrade.")
						}
					}
					if m.Kind == "shady-contract" {
						if data.Accept {
							price := m.Price
							if price <= 0 {
								price = 3000 + rand.Intn(3001)
							}
							if p.Money < price {
								gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits to pay the fixer.")
							} else {
								p.Money -= price
								// Small chance the deal is a Federation sting operation
								if rand.Intn(20) == 0 { // ~5%
									fine := 1000
									p.Money -= fine
									if len(p.Inventory) > 0 {
										p.Inventory = map[string]int{}
										p.InventoryAvgCost = map[string]int{}
									}
									gs.enqueueModal(p, "Federation Sting!", fmt.Sprintf("Federation agents confiscate your %d-credit payment, fine you an additional %d credits, and impound your cargo.", price, fine))
									gs.logAction(room, p, fmt.Sprintf("Federation sting seized shady contract funds ($%d) and cargo", price+fine))
								} else {
									contract := &BlackOpsContract{
										ID:          randID(),
										Instigator:  p.ID,
										TriggerTurn: room.Turn + 1,
										Applied:     make(map[PlayerID]bool),
										Price:       price,
									}
									room.PendingBlackOps = append(room.PendingBlackOps, contract)
									gs.enqueueModal(p, "Underhanded Deal", "The fixer vanishes into the crowd. Expect your rivals to experience setbacks next turn.")
									gs.logAction(room, p, fmt.Sprintf("Funded a shady contract for $%d", price))
								}
							}
						} else {
							gs.logAction(room, p, "Declined a shady contract offer")
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
				if data.Planet != "" && !p.KnownPlanets[data.Planet] {
					allow = false
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
		case "auctionBid":
			var data struct {
				AuctionID string `json:"auctionId"`
				Bid       int    `json:"bid"`
			}
			json.Unmarshal(msg.Payload, &data)
			if room := gs.getRoom(p.roomID); room != nil {
				gs.handleAuctionBid(room, p, data.AuctionID, data.Bid)
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
		if room.Private && room.CreatorID != "" && room.CreatorID != p.ID {
			room.mu.Unlock()
			continue
		}
		resp = append(resp, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"started":     room.Started,
			"playerCount": len(room.Players),
			"turn":        room.Turn,
			"private":     room.Private,
			"paused":      room.Paused,
			"creatorId":   string(room.CreatorID),
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

func (gs *GameServer) createRoom(name string, creator PlayerID, private bool) *Room {
	name = sanitizeAlphanumeric(name)
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
		Private: private,
		CreatorID: func() PlayerID {
			if private {
				return creator
			}
			return ""
		}(),
		Paused:  false,
		stateCh: make(chan struct{}, 1),
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
	room.mu.Lock()
	if room.Private && room.CreatorID != "" && room.CreatorID != p.ID {
		room.mu.Unlock()
		if p.conn != nil {
			p.writeMu.Lock()
			p.conn.WriteJSON(WSOut{Type: "joinDenied", Payload: map[string]string{"message": "This room is private."}})
			p.writeMu.Unlock()
		}
		gs.sendLobbyState(p)
		return
	}
	room.mu.Unlock()
	// remove from old room
	if p.roomID != "" && p.roomID != roomID {
		if old := gs.getRoom(p.roomID); old != nil {
			old.mu.Lock()
			// Persist snapshot so rejoining the old room restores progress
			old.Persist[p.ID] = &PersistedPlayer{
				Money:              p.Money,
				CurrentPlanet:      p.CurrentPlanet,
				DestinationPlanet:  p.DestinationPlanet,
				Inventory:          cloneIntMap(p.Inventory),
				InventoryAvgCost:   cloneIntMap(p.InventoryAvgCost),
				Ready:              p.Ready,
				Modals:             cloneModals(p.Modals),
				Fuel:               p.Fuel,
				Bankrupt:           p.Bankrupt,
				InTransit:          p.InTransit,
				TransitFrom:        p.TransitFrom,
				TransitRemaining:   p.TransitRemaining,
				TransitTotal:       p.TransitTotal,
				CapacityBonus:      p.CapacityBonus,
				SpeedBonus:         p.SpeedBonus,
				FuelCapacityBonus:  p.FuelCapacityBonus,
				ActionHistory:      cloneActionHistory(p.ActionHistory),
				FacilityInvestment: p.FacilityInvestment,
				UpgradeInvestment:  p.UpgradeInvestment,
				MarketMemory:       cloneMarketMemory(p.MarketMemory),
				KnownPlanets:       cloneBoolMap(p.KnownPlanets),
			}
			delete(old.Players, p.ID)
			old.mu.Unlock()
			gs.broadcastRoom(old)
		}
	}
	room.mu.Lock()
	if room.Private && room.CreatorID == "" {
		room.CreatorID = p.ID
	}
	wasPaused := room.Paused
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
		p.EndGame = false // Always reset EndGame state when joining a new room
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
		p.FacilityInvestment = snap.FacilityInvestment
		p.UpgradeInvestment = snap.UpgradeInvestment
		p.KnownPlanets = cloneBoolMap(snap.KnownPlanets)
		// restore per-room action history
		p.ActionHistory = cloneActionHistory(snap.ActionHistory)
		// Initialize price memory for bots (important for restored bots)
		if p.PriceMemory == nil {
			p.PriceMemory = make(map[string]*PriceMemory)
		}
		if snap.MarketMemory != nil {
			p.MarketMemory = cloneMarketMemory(snap.MarketMemory)
		} else if p.MarketMemory == nil {
			p.MarketMemory = make(map[string]*MarketSnapshot)
		}
		delete(room.Persist, p.ID)
	} else {
		// New room without a snapshot: start with fresh per-room state
		p.Money = 1000
		start := chooseStartingPlanet(room)
		if start == "" {
			if names := planetNames(room.Planets); len(names) > 0 {
				start = names[rand.Intn(len(names))]
			}
		}
		p.CurrentPlanet = start
		p.DestinationPlanet = ""
		p.Ready = false
		p.EndGame = false // Always start with EndGame false in new rooms
		p.Fuel = fuelCapacity
		p.Inventory = map[string]int{}
		p.InventoryAvgCost = map[string]int{}
		p.Modals = []ModalItem{}
		p.FacilityInvestment = 0
		p.UpgradeInvestment = 0
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
		// Initialize price memory
		p.PriceMemory = make(map[string]*PriceMemory)
		p.MarketMemory = make(map[string]*MarketSnapshot)
		p.KnownPlanets = make(map[string]bool)
		if start != "" {
			p.KnownPlanets[start] = true
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
	if p.MarketMemory == nil {
		p.MarketMemory = make(map[string]*MarketSnapshot)
	}
	if p.KnownPlanets == nil {
		p.KnownPlanets = make(map[string]bool)
	}
	room.Players[p.ID] = p
	if pos, ok := room.PlanetPositions[p.CurrentPlanet]; ok {
		discoverAroundPosition(room, p, pos, discoveryRadiusStation)
	}
	if room.Private && room.CreatorID == p.ID {
		room.Paused = false
		if room.Started {
			room.TurnEndsAt = time.Now().Add(turnDuration)
		}
		if wasPaused {
			select {
			case room.stateCh <- struct{}{}:
			default:
			}
		}
	}
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
		Money:              p.Money,
		CurrentPlanet:      p.CurrentPlanet,
		DestinationPlanet:  p.DestinationPlanet,
		Inventory:          cloneIntMap(p.Inventory),
		InventoryAvgCost:   cloneIntMap(p.InventoryAvgCost),
		Ready:              p.Ready,
		EndGame:            p.EndGame,
		Fuel:               p.Fuel,
		Bankrupt:           p.Bankrupt,
		InTransit:          p.InTransit,
		TransitFrom:        p.TransitFrom,
		TransitRemaining:   p.TransitRemaining,
		TransitTotal:       p.TransitTotal,
		CapacityBonus:      p.CapacityBonus,
		SpeedBonus:         p.SpeedBonus,
		FuelCapacityBonus:  p.FuelCapacityBonus,
		ActionHistory:      cloneActionHistory(p.ActionHistory),
		FacilityInvestment: p.FacilityInvestment,
		UpgradeInvestment:  p.UpgradeInvestment,
		MarketMemory:       cloneMarketMemory(p.MarketMemory),
		KnownPlanets:       cloneBoolMap(p.KnownPlanets),
	}
	delete(room.Players, p.ID)
	p.roomID = ""

	// Check if room is now empty and should be cleaned up
	isEmpty := len(room.Players) == 0
	isCreator := room.CreatorID != "" && room.CreatorID == p.ID
	shouldKeep := room.Private && isCreator
	if shouldKeep {
		room.Paused = true
		room.TurnEndsAt = time.Time{}
		select {
		case room.stateCh <- struct{}{}:
		default:
		}
	}

	// If game running and no humans remain, prompt the ticker to end turn now
	if room.Started && !shouldKeep {
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

	// If the creator left a private room, keep the room but broadcast the pause state
	if shouldKeep {
		gs.broadcastRoom(room)
	} else if isEmpty {
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

func (gs *GameServer) handleRestoreSingleplayer(p *Player, payload json.RawMessage) {
	var data struct {
		RoomID  string `json:"roomId"`
		Save    string `json:"save"`
		OwnerID string `json:"ownerId"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		gs.sendRestoreAck(p, "", false, "Invalid restore request.")
		return
	}

	room, startTicker, err := gs.restoreSingleplayerSnapshot(p, data.RoomID, data.Save, data.OwnerID)
	if err != nil {
		gs.sendRestoreAck(p, data.RoomID, false, err.Error())
		return
	}

	if startTicker {
		go gs.runTicker(room)
	}
	gs.sendRoomState(room, nil)
	gs.sendRestoreAck(p, room.ID, true, "Singleplayer mission restored.")
}

func (gs *GameServer) restoreSingleplayerSnapshot(p *Player, roomID, encoded, ownerID string) (*Room, bool, error) {
	if roomID == "" {
		return nil, false, fmt.Errorf("missing room identifier")
	}
	if encoded == "" {
		return nil, false, fmt.Errorf("missing saved mission payload")
	}
	room := gs.getRoom(roomID)
	if room == nil {
		return nil, false, fmt.Errorf("room no longer exists")
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false, fmt.Errorf("unable to decode saved mission snapshot")
	}
	var save singleplayerSavePayload
	if err := json.Unmarshal(raw, &save); err != nil {
		return nil, false, fmt.Errorf("unable to parse saved mission snapshot")
	}
	if len(save.Turns) == 0 {
		return nil, false, fmt.Errorf("saved mission contains no turns")
	}
	if ownerID != "" && ownerID != string(p.ID) {
		return nil, false, fmt.Errorf("restore request owner mismatch")
	}
	if save.OwnerID != "" && save.OwnerID != string(p.ID) {
		return nil, false, fmt.Errorf("saved mission belongs to another commander")
	}

	allKnown := make(map[string]struct{})
	for _, turn := range save.Turns {
		state := turn.State
		for _, name := range state.DiscoveredPlanets {
			if name != "" {
				allKnown[name] = struct{}{}
			}
		}
		for _, name := range state.Room.Planets {
			if name != "" {
				allKnown[name] = struct{}{}
			}
		}
		for _, name := range state.You.KnownPlanets {
			if name != "" {
				allKnown[name] = struct{}{}
			}
		}
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if !room.Private {
		return nil, false, fmt.Errorf("cannot restore into a public room")
	}
	if room.CreatorID != "" && room.CreatorID != p.ID {
		return nil, false, fmt.Errorf("you are not the owner of this room")
	}

	snap := save.Turns[len(save.Turns)-1]
	state := snap.State
	if len(allKnown) > 0 {
		rebuilt := make([]string, 0, len(allKnown))
		for name := range allKnown {
			rebuilt = append(rebuilt, name)
		}
		sort.Strings(rebuilt)
		state.DiscoveredPlanets = rebuilt
		state.Room.Planets = rebuilt
		state.You.KnownPlanets = rebuilt
		log.Printf("[SingleplayerRestore] room=%s turn=%d union=%d preview=[%s]", room.ID, snap.Turn, len(rebuilt), sampleNames(rebuilt, 12))
	}
	snap.State = state
	save.Turns[len(save.Turns)-1] = snap

	originalStarted := room.Started

	room.Name = defaultStr(state.Room.Name, room.Name)
	room.Started = state.Room.Started
	room.Paused = state.Room.Paused
	room.Turn = state.Room.Turn
	if state.Room.CreatorID != "" {
		room.CreatorID = PlayerID(state.Room.CreatorID)
	} else if room.CreatorID == "" {
		room.CreatorID = p.ID
	}
	if state.Room.TurnEndsAt > 0 {
		room.TurnEndsAt = time.UnixMilli(state.Room.TurnEndsAt)
	} else if room.Started {
		room.TurnEndsAt = time.Now().Add(turnDuration)
	} else {
		room.TurnEndsAt = time.Now()
	}

	if len(state.Room.News) > 0 {
		news := make([]NewsItem, 0, len(state.Room.News))
		for _, n := range state.Room.News {
			news = append(news, NewsItem{
				Headline:       n.Headline,
				Planet:         n.Planet,
				TurnsRemaining: n.TurnsRemaining,
			})
		}
		room.News = news
	} else {
		room.News = nil
	}

	delete(room.Persist, p.ID)

	you := state.You
	p.Name = defaultStr(you.Name, p.Name)
	p.Money = you.Money
	p.CurrentPlanet = defaultStr(you.CurrentPlanet, p.CurrentPlanet)
	p.DestinationPlanet = you.DestinationPlanet
	p.Ready = you.Ready
	p.EndGame = you.EndGame
	p.Fuel = maxInt(0, you.Fuel)
	if you.Inventory != nil {
		p.Inventory = cloneIntMap(you.Inventory)
	} else {
		p.Inventory = map[string]int{}
	}
	if you.InventoryAvgCost != nil {
		p.InventoryAvgCost = cloneIntMap(you.InventoryAvgCost)
	} else {
		p.InventoryAvgCost = map[string]int{}
	}
	p.InTransit = you.InTransit
	p.TransitFrom = you.TransitFrom
	p.TransitRemaining = you.TransitRemaining
	p.TransitTotal = you.TransitTotal
	p.FacilityInvestment = you.FacilityInvestment
	p.UpgradeInvestment = you.UpgradeInvestment
	if you.MarketMemory != nil {
		p.MarketMemory = cloneMarketMemory(you.MarketMemory)
	} else {
		p.MarketMemory = make(map[string]*MarketSnapshot)
	}
	p.Modals = []ModalItem{}
	knownPlanets := make(map[string]bool)
	addKnown := func(name string) {
		if name == "" {
			return
		}
		knownPlanets[name] = true
	}
	for _, name := range state.DiscoveredPlanets {
		addKnown(name)
	}
	for _, name := range state.Room.Planets {
		addKnown(name)
	}
	for _, name := range you.KnownPlanets {
		addKnown(name)
	}
	addKnown(p.CurrentPlanet)
	addKnown(p.DestinationPlanet)
	if len(knownPlanets) == 0 && p.CurrentPlanet != "" {
		knownPlanets[p.CurrentPlanet] = true
	}
	p.KnownPlanets = knownPlanets
	knownList := make([]string, 0, len(p.KnownPlanets))
	for name := range p.KnownPlanets {
		knownList = append(knownList, name)
	}
	sort.Strings(knownList)
	log.Printf("[SingleplayerRestore] player=%s knownCount=%d preview=[%s]", p.ID, len(knownList), sampleNames(knownList, 12))
	capacityBonus := you.Capacity - shipCapacity
	if capacityBonus < 0 {
		capacityBonus = 0
	}
	p.CapacityBonus = capacityBonus
	speedBonus := you.SpeedPerTurn - 20
	if speedBonus < 0 {
		speedBonus = 0
	}
	p.SpeedBonus = speedBonus
	fuelBonus := you.FuelCapacity - fuelCapacity
	if fuelBonus < 0 {
		fuelBonus = 0
	}
	p.FuelCapacityBonus = fuelBonus
	p.Bankrupt = false
	p.roomID = room.ID

	room.Players[p.ID] = p

	for _, ps := range state.Room.Players {
		pid := PlayerID(ps.ID)
		if pid == "" {
			continue
		}
		if pid == p.ID {
			p.Bankrupt = ps.Bankrupt
			continue
		}
		if other := room.Players[pid]; other != nil {
			other.Name = defaultStr(ps.Name, other.Name)
			other.CurrentPlanet = defaultStr(ps.CurrentPlanet, other.CurrentPlanet)
			other.DestinationPlanet = ps.DestinationPlanet
			other.Ready = ps.Ready
			other.EndGame = ps.EndGame
			other.Bankrupt = ps.Bankrupt
			if ps.CashValue != 0 {
				other.Money = ps.CashValue
			}
		}
	}

	startTicker := state.Room.Started && !originalStarted

	select {
	case room.stateCh <- struct{}{}:
	default:
	}

	return room, startTicker, nil
}

func (gs *GameServer) sendRestoreAck(p *Player, roomID string, success bool, message string) {
	if p == nil || p.conn == nil {
		return
	}
	payload := map[string]interface{}{
		"roomId":  roomID,
		"success": success,
	}
	if message != "" {
		payload["message"] = message
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.conn.WriteJSON(WSOut{Type: "restoreAck", Payload: payload})
}

func (gs *GameServer) startGame(roomID string) {
	room := gs.getRoom(roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	if !room.Started {
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

	// Choose a random bot name that's not already taken
	var botName string
	usedNames := make(map[string]bool)
	for _, p := range room.Players {
		usedNames[p.Name] = true
	}

	availableNames := []string{}
	for _, name := range botNames {
		if !usedNames[name] {
			availableNames = append(availableNames, name)
		}
	}

	if len(availableNames) == 0 {
		// Fallback if all names are taken
		botName = "Bot " + randID()[0:3]
	} else {
		botName = availableNames[rand.Intn(len(availableNames))]
	}

	start := chooseStartingPlanet(room)
	if start == "" {
		if names := planetNames(room.Planets); len(names) > 0 {
			start = names[rand.Intn(len(names))]
		}
	}

	b := &Player{
		ID:                 PlayerID(randID()),
		Name:               botName,
		Money:              1000,
		CurrentPlanet:      start,
		DestinationPlanet:  "",
		Inventory:          map[string]int{},
		InventoryAvgCost:   map[string]int{},
		Fuel:               fuelCapacity,
		Ready:              true, // bots are always ready
		IsBot:              true,
		PriceMemory:        make(map[string]*PriceMemory),
		MarketMemory:       make(map[string]*MarketSnapshot),
		LastTripStartMoney: 1000,
		ConsecutiveVisits:  make(map[string]int),
		KnownPlanets:       make(map[string]bool),
	}
	if start != "" {
		b.KnownPlanets[start] = true
	}
	b.roomID = room.ID
	room.mu.Lock()
	room.Players[b.ID] = b
	if pos, ok := room.PlanetPositions[b.CurrentPlanet]; ok {
		discoverAroundPosition(room, b, pos, discoveryRadiusStation)
	}
	room.mu.Unlock()
	gs.broadcastRoom(room)
}

func (gs *GameServer) runTicker(room *Room) {
	base := turnDuration
	for {
		room.mu.Lock()
		if room.Paused {
			room.mu.Unlock()
			select {
			case <-room.closeCh:
				return
			case <-room.stateCh:
				continue
			}
		}
		onlyBots := true
		for _, pl := range room.Players {
			if !pl.IsBot {
				onlyBots = false
				break
			}
		}
		if !onlyBots {
			now := time.Now()
			if room.TurnEndsAt.Before(now.Add(1 * time.Second)) {
				room.TurnEndsAt = now.Add(base)
			}
		}
		room.mu.Unlock()
		if !onlyBots {
			gs.broadcastRoom(room)
		}

		if onlyBots {
			select {
			case <-room.closeCh:
				return
			case <-room.stateCh:
				continue
			case <-time.After(150 * time.Millisecond):
			}
		} else {
			timer := time.NewTimer(base)
			select {
			case <-timer.C:
			case <-room.readyCh:
				if !timer.Stop() {
					<-timer.C
				}
			case <-room.closeCh:
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-room.stateCh:
				if !timer.Stop() {
					<-timer.C
				}
				continue
			}
		}
		room.mu.Lock()
		if !room.Started {
			room.mu.Unlock()
			return
		}
		if room.Paused {
			room.mu.Unlock()
			continue
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
					gs.updateDiscoveryForPlayer(room, p)
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
			gs.updateDiscoveryForPlayer(room, p)
		}

		// Handle facility charges and collection
		gs.handleFacilities(room)

		// accumulate per-planet production
		for _, pl := range room.Planets {
			for g, amt := range pl.Prod {
				if amt <= 0 {
					continue
				}
				pl.Goods[g] = pl.Goods[g] + amt
			}
		}

		// Bot helper functions
		updateBotPriceMemory := func(bot *Player, planet *Planet, turn int) {
			if bot.PriceMemory == nil {
				bot.PriceMemory = make(map[string]*PriceMemory)
			}

			// Calculate average goods availability
			totalGoods := 0
			goodCount := 0
			for _, qty := range planet.Goods {
				totalGoods += qty
				goodCount++
			}
			avgGoods := 0
			if goodCount > 0 {
				avgGoods = totalGoods / goodCount
			}

			// Preserve existing purchase history if it exists
			var existingMemory *PriceMemory
			if existing, exists := bot.PriceMemory[bot.CurrentPlanet]; exists {
				existingMemory = existing
			}

			bot.PriceMemory[bot.CurrentPlanet] = &PriceMemory{
				Prices:          make(map[string]int),
				Turn:            turn,
				GoodsAvg:        avgGoods,
				LastPurchased:   make(map[string]int),
				PurchaseAmounts: make(map[string]int),
				VisitCount:      1,
				LastProfit:      0,
				ProfitHistory:   make([]int, 0),
			}

			// Restore purchase history and visit data if it existed
			if existingMemory != nil {
				for good, lastTurn := range existingMemory.LastPurchased {
					bot.PriceMemory[bot.CurrentPlanet].LastPurchased[good] = lastTurn
				}
				for good, amount := range existingMemory.PurchaseAmounts {
					bot.PriceMemory[bot.CurrentPlanet].PurchaseAmounts[good] = amount
				}
				// Increment visit count and calculate profit from this trip
				bot.PriceMemory[bot.CurrentPlanet].VisitCount = existingMemory.VisitCount + 1
				currentProfit := bot.Money - bot.LastTripStartMoney
				bot.PriceMemory[bot.CurrentPlanet].LastProfit = currentProfit

				// Update profit history (keep last 5)
				profitHistory := existingMemory.ProfitHistory
				profitHistory = append(profitHistory, currentProfit)
				if len(profitHistory) > 5 {
					profitHistory = profitHistory[1:]
				}
				bot.PriceMemory[bot.CurrentPlanet].ProfitHistory = profitHistory

				// Update trip start money for next trip
				bot.LastTripStartMoney = bot.Money
			}

			for good, price := range planet.Prices {
				bot.PriceMemory[bot.CurrentPlanet].Prices[good] = price
			}
		}

		recordBotPurchase := func(bot *Player, good string, amount int, turn int) {
			if bot.PriceMemory == nil {
				bot.PriceMemory = make(map[string]*PriceMemory)
			}

			if memory, exists := bot.PriceMemory[bot.CurrentPlanet]; exists {
				if memory.LastPurchased == nil {
					memory.LastPurchased = make(map[string]int)
				}
				if memory.PurchaseAmounts == nil {
					memory.PurchaseAmounts = make(map[string]int)
				}
				memory.LastPurchased[good] = turn
				memory.PurchaseAmounts[good] = amount
			}
		}

		findBestTradingRoute := func(bot *Player, room *Room) string {
			if len(bot.PriceMemory) < 2 {
				// Not enough price data, pick random destination
				return ""
			}

			bestDestination := ""
			bestProfitPotential := 0.0
			currentPlanet := room.Planets[bot.CurrentPlanet]
			if currentPlanet == nil {
				return ""
			}

			// Constants for replenishment logic
			const minReplenishmentTime = 3          // Minimum turns to wait before returning to buy
			const significantPurchaseThreshold = 10 // Amount considered "significant"

			// Emergency mode: if bot is very low on money, be more flexible
			emergencyMode := bot.Money < 200
			lowMoneyMode := bot.Money < 500

			// Look for profitable opportunities based on remembered prices
			for planetName, memory := range bot.PriceMemory {
				if planetName == bot.CurrentPlanet {
					continue // Skip current planet
				}

				// In emergency mode, reduce restrictions significantly
				if !emergencyMode {
					// Check if this planet is being visited too consecutively (anti-loop logic)
					if consecutiveVisits, exists := bot.ConsecutiveVisits[planetName]; exists && consecutiveVisits >= 4 {
						// Skip if visiting too much, unless it's profitable enough
						profitThreshold := 100
						if lowMoneyMode {
							profitThreshold = 50 // Lower threshold when money is low
						}
						if memory.LastProfit < profitThreshold {
							continue
						}
					}

					// Check profitability trend - but be less strict in low money situations
					if !lowMoneyMode {
						isProfitDecreasing := false
						if len(memory.ProfitHistory) >= 3 {
							history := memory.ProfitHistory
							lastThree := history[len(history)-3:]
							if lastThree[0] > lastThree[1] && lastThree[1] > lastThree[2] && lastThree[2] < 20 {
								isProfitDecreasing = true
							}
						}
						if isProfitDecreasing {
							continue // Skip routes showing declining profits
						}
					}
				}

				// Calculate distance and check if reachable
				distance := distanceUnits(room, bot.CurrentPlanet, planetName)
				if distance > bot.Fuel {
					continue // Can't reach
				}

				// Encourage exploration - but reduce penalty in emergency situations
				explorationPenalty := 0.0
				if !emergencyMode && memory.VisitCount > 6 {
					penalty := float64((memory.VisitCount - 6) * 15) // Reduced from 20
					if lowMoneyMode {
						penalty = penalty * 0.5 // Halve penalty when money is low
					}
					explorationPenalty = penalty
				}

				// Calculate potential profit based on price differences
				sellingProfit := 0.0
				buyingProfit := 0.0
				sellingOpportunities := 0
				buyingOpportunities := 0

				// Check goods we have in inventory that might sell well there (prioritize selling)
				for good, qty := range bot.Inventory {
					if qty > 0 {
						if rememberedPrice, exists := memory.Prices[good]; exists {
							currentPrice := currentPlanet.Prices[good]
							if rememberedPrice > currentPrice {
								profitMargin := float64(rememberedPrice - currentPrice)
								staleness := float64(room.Turn - memory.Turn + 1)
								// Reduce staleness penalty in emergency mode
								if emergencyMode {
									staleness = staleness * 0.5
								}
								weightedProfit := profitMargin / staleness * float64(qty) * 0.8 // Increased weight for selling
								sellingProfit += weightedProfit
								sellingOpportunities++
							}
						}
					}
				}

				// Check goods we can buy here and sell there (but consider market depletion)
				for good, currentPrice := range currentPlanet.Prices {
					if currentGoods := currentPlanet.Goods[good]; currentGoods > 0 && currentPrice > 0 {
						if rememberedPrice, exists := memory.Prices[good]; exists && rememberedPrice > currentPrice {
							// Check if we recently bought this good from the destination planet
							buyingAtDestination := false
							if !emergencyMode { // Skip replenishment checks in emergency mode
								if lastPurchased, purchased := memory.LastPurchased[good]; purchased {
									timeSinceLastPurchase := room.Turn - lastPurchased
									purchaseAmount := memory.PurchaseAmounts[good]

									// If we made a significant purchase recently, wait for replenishment
									if timeSinceLastPurchase < minReplenishmentTime && purchaseAmount >= significantPurchaseThreshold {
										buyingAtDestination = true
									}
								}
							}

							// Only consider this opportunity if we're not planning to buy there soon
							if !buyingAtDestination {
								// We remember this good being more expensive there - good for selling
								profitMargin := float64(rememberedPrice - currentPrice)
								// Weight by availability and freshness of memory
								staleness := float64(room.Turn - memory.Turn + 1)
								if emergencyMode {
									staleness = staleness * 0.5
								}
								weightedProfit := profitMargin / staleness * 0.5 // Increased weight for buying opportunities
								buyingProfit += weightedProfit
								buyingOpportunities++
							}
						}
					}
				}

				// Combine profits with selling getting priority, then apply exploration penalty
				totalProfit := sellingProfit + buyingProfit - explorationPenalty
				totalOpportunities := sellingOpportunities + buyingOpportunities

				// Prefer routes with selling opportunities and multiple opportunities
				if sellingOpportunities > 0 {
					totalProfit = totalProfit * 1.4 // Increased bonus for selling opportunities
				}
				if totalOpportunities > 0 {
					totalProfit = totalProfit * (1.0 + float64(totalOpportunities)*0.15) // Increased opportunity bonus
				}

				// In emergency mode, accept any positive profit
				if emergencyMode && totalProfit > 5.0 {
					totalProfit = totalProfit * 2.0 // Double profits in emergency mode to encourage any trade
				}

				if totalProfit > bestProfitPotential {
					bestProfitPotential = totalProfit
					bestDestination = planetName
				}
			}

			// Update consecutive visits tracking only if we found a destination
			if bestDestination != "" {
				// Reset other planet counters
				for planet := range bot.ConsecutiveVisits {
					if planet != bestDestination {
						bot.ConsecutiveVisits[planet] = 0
					}
				}
				// Increment counter for chosen planet
				bot.ConsecutiveVisits[bestDestination]++
			}

			return bestDestination
		}

		// Enhanced bot AI: remember prices and plan routes intelligently
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

			// Update bot's price memory for current planet
			updateBotPriceMemory(bp, planet, room.Turn)

			// Use intelligent trading logic based on memory, fall back to simple logic
			useIntelligentTrading := len(bp.PriceMemory) >= 2
			// reference price ranges per good
			ranges := defaultPriceRanges()

			// Enhanced selling logic using price memory
			emergencyMode := bp.Money < 200
			lowMoneyMode := bp.Money < 500

			for g, qty := range bp.Inventory {
				if qty <= 0 {
					continue
				}
				price := planet.Prices[g]
				shouldSell := false

				if useIntelligentTrading {
					// Check if this is a good price compared to what we remember
					maxRememberedPrice := 0
					for _, memory := range bp.PriceMemory {
						if memPrice, exists := memory.Prices[g]; exists && memPrice > maxRememberedPrice {
							maxRememberedPrice = memPrice
						}
					}

					// Adjust selling threshold based on financial situation
					sellThreshold := 0.8 // Default: 80% of best remembered price
					if emergencyMode {
						sellThreshold = 0.5 // Emergency: sell at 50% of best price
					} else if lowMoneyMode {
						sellThreshold = 0.65 // Low money: sell at 65% of best price
					}

					// Sell if current price meets our threshold
					if maxRememberedPrice > 0 && price >= int(float64(maxRememberedPrice)*sellThreshold) {
						shouldSell = true
					}

					// Emergency selling: sell any profitable goods regardless of remembered prices
					if emergencyMode && price > 0 {
						// Check if we can make any profit based on average cost
						avgCost := bp.InventoryAvgCost[g]
						if avgCost > 0 && price > avgCost {
							shouldSell = true
						}
					}
				} else {
					// Fallback to original logic
					max := 0
					if r, ok := ranges[g]; ok {
						max = r[1]
					}
					threshold := (max * 50) / 100
					if emergencyMode {
						threshold = (max * 30) / 100 // Lower threshold in emergency
					}
					shouldSell = price > threshold
				}

				if shouldSell {
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
			// Enhanced buying logic using price memory — skip if still below fuel reserve
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

					shouldBuy := false
					if useIntelligentTrading {
						// Check if this is a good price compared to what we remember at other planets
						maxRememberedPrice := 0
						for planetName, memory := range bp.PriceMemory {
							if planetName == bp.CurrentPlanet {
								continue // Skip current planet
							}
							if memPrice, exists := memory.Prices[g]; exists && memPrice > maxRememberedPrice {
								maxRememberedPrice = memPrice
							}
						}

						// Adjust buying threshold based on financial situation
						buyThreshold := 0.7 // Default: buy if 70% cheaper than best selling price
						if emergencyMode {
							buyThreshold = 0.9 // Emergency: buy even if only 10% cheaper
						} else if lowMoneyMode {
							buyThreshold = 0.8 // Low money: buy if 20% cheaper
						}

						// Buy if current price is significantly lower than what we expect to sell for elsewhere
						if maxRememberedPrice > 0 && price <= int(float64(maxRememberedPrice)*buyThreshold) {
							shouldBuy = true
						}

						// If we haven't seen this good elsewhere yet, be more willing to buy in financial trouble
						if maxRememberedPrice == 0 && (emergencyMode || lowMoneyMode) {
							shouldBuy = true
						}

						// Additional check: avoid buying too much if supply is low compared to our memory
						// But skip this check in emergency mode
						if shouldBuy && !emergencyMode {
							currentMemory := bp.PriceMemory[bp.CurrentPlanet]
							goodsAvailable := planet.Goods[g]
							if currentMemory != nil && currentMemory.GoodsAvg > 0 {
								// If this good's availability is much lower than the average we remember,
								// be more conservative about buying (market might be depleted)
								if goodsAvailable < currentMemory.GoodsAvg/3 {
									shouldBuy = false // Skip goods that seem severely depleted
								}
							}
						}
					} else {
						// Fallback to original logic
						max := 0
						if r, ok := ranges[g]; ok {
							max = r[1]
						}
						if max <= 0 {
							continue
						}
						threshold := (max * 46) / 100
						if emergencyMode {
							threshold = (max * 60) / 100 // More willing to buy in emergency
						} else if lowMoneyMode {
							threshold = (max * 52) / 100 // Slightly more willing when low on money
						}
						shouldBuy = price < threshold
					}

					if !shouldBuy {
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

					// Record this purchase to avoid returning too soon to buy more of this good
					recordBotPurchase(bp, g, amount, room.Turn)
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
			// Intelligent destination selection based on price memory
			if useIntelligentTrading {
				// Try to find profitable route based on remembered prices
				bestDest := findBestTradingRoute(bp, room)
				if bestDest != "" {
					dist := distanceUnits(room, bp.CurrentPlanet, bestDest)
					if dist <= bp.Fuel {
						bp.DestinationPlanet = bestDest
						gs.logAction(room, bp, fmt.Sprintf("Planning profitable route to %s (%d units)", bestDest, dist))
					} else {
						// Try to refuel for the profitable route
						fuelNeeded := dist - bp.Fuel
						capLeft := (fuelCapacity + bp.FuelCapacityBonus) - bp.Fuel
						if capLeft >= fuelNeeded {
							fp := planet.FuelPrice
							if fp <= 0 {
								fp = 10
							}
							cost := fuelNeeded * fp
							if bp.Money >= cost {
								bp.Money -= cost
								bp.Fuel += fuelNeeded
								bp.DestinationPlanet = bestDest
								gs.logAction(room, bp, fmt.Sprintf("Refueled %d units ($%d) for profitable route to %s", fuelNeeded, cost, bestDest))
							}
						}
					}
				}
			}

			// Fallback to random destination selection if no intelligent route found
			if bp.DestinationPlanet == "" || bp.DestinationPlanet == bp.CurrentPlanet {
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
					}
				}
			}
		}
		// Rare per-player events; bots are impacted too. Humans receive modals; bots auto-resolve.
		for _, hp := range room.Players {
			if len(room.PendingBlackOps) > 0 {
				for _, contract := range room.PendingBlackOps {
					if room.Turn >= contract.TriggerTurn && hp.ID != contract.Instigator && !hp.Bankrupt {
						gs.applyBlackOpsHit(room, contract, hp)
					}
				}
			}
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

			// Pirate raid: ~0.8% chance per turn - lose money but keep cargo
			if rand.Intn(125) == 0 {
				lossPercent := 10 + rand.Intn(21) // 10-30%
				loss := (hp.Money * lossPercent) / 100
				if loss > 0 {
					hp.Money -= loss
					gs.logAction(room, hp, fmt.Sprintf("Pirate raid: lost $%d (%d%% of credits)", loss, lossPercent))
					gs.enqueueModal(hp, "Pirate Raid!", "Space pirates demanded tribute and took "+strconv.Itoa(loss)+" credits. Your cargo was spared.")
				}
			}

			// Insurance payout: ~0.7% chance per turn
			if rand.Intn(140) == 0 {
				payout := 800 + rand.Intn(1201) // 800-2000 credits
				hp.Money += payout
				gs.logAction(room, hp, fmt.Sprintf("Insurance payout: +$%d", payout))
				gs.enqueueModal(hp, "Insurance Payout", "Your ship insurance paid out "+strconv.Itoa(payout)+" credits for a previous incident.")
			}

			// Cargo spoilage: ~0.6% chance per turn - lose some random goods
			if rand.Intn(160) == 0 && len(hp.Inventory) > 0 {
				// Pick a random good from inventory
				var goods []string
				for good := range hp.Inventory {
					goods = append(goods, good)
				}
				if len(goods) > 0 {
					spoiledGood := goods[rand.Intn(len(goods))]
					currentQty := hp.Inventory[spoiledGood]
					if currentQty > 0 {
						spoiledQty := 1 + rand.Intn(minInt(currentQty, 5)) // spoil 1-5 units or all if less
						hp.Inventory[spoiledGood] -= spoiledQty
						if hp.Inventory[spoiledGood] <= 0 {
							delete(hp.Inventory, spoiledGood)
							delete(hp.InventoryAvgCost, spoiledGood)
						}
						gs.logAction(room, hp, fmt.Sprintf("Cargo spoilage: lost %d %s", spoiledQty, spoiledGood))
						gs.enqueueModal(hp, "Cargo Spoilage", "Storage malfunction caused "+strconv.Itoa(spoiledQty)+" "+spoiledGood+" to spoil and be jettisoned.")
					}
				}
			}

			// Trade route discovery bonus: ~0.5% chance per turn
			if rand.Intn(200) == 0 {
				bonus := 1200 + rand.Intn(1801) // 1200-3000 credits
				hp.Money += bonus
				gs.logAction(room, hp, fmt.Sprintf("Trade route bonus: +$%d", bonus))
				gs.enqueueModal(hp, "Trade Route Discovery", "You discovered a lucrative trade route shortcut! Navigation data sold for "+strconv.Itoa(bonus)+" credits.")
			}

			// Equipment malfunction: ~0.4% chance per turn - repair cost based on upgrades
			if rand.Intn(250) == 0 && hp.SpeedBonus > 0 {
				speedLoss := 1 + rand.Intn(minInt(hp.SpeedBonus, 3)) // lose 1-3 speed worth of repairs
				repairCost := speedLoss * 200
				hp.Money -= repairCost
				gs.logAction(room, hp, fmt.Sprintf("Engine malfunction: paid $%d for repairs", repairCost))
				gs.enqueueModal(hp, "Engine Malfunction", "Your enhanced engines malfunctioned and required emergency repairs costing "+strconv.Itoa(repairCost)+" credits.")
			}

			// Salvage discovery: ~0.4% chance per turn - free goods
			if rand.Intn(250) == 0 {
				// Pick a random good type
				allGoods := []string{"Water", "Food", "Minerals", "Chemicals", "Energy", "Medicine", "Electronics", "Luxury"}
				salvageGood := allGoods[rand.Intn(len(allGoods))]
				salvageQty := 1 + rand.Intn(8) // 1-8 units

				// Check if we have capacity
				used := inventoryTotal(hp.Inventory)
				free := shipCapacity + hp.CapacityBonus - used
				if salvageQty > free {
					salvageQty = free
				}

				if salvageQty > 0 {
					hp.Inventory[salvageGood] += salvageQty
					// Set a reasonable average cost (market mid-range)
					ranges := defaultPriceRanges()
					if rng, exists := ranges[salvageGood]; exists {
						avgPrice := (rng[0] + rng[1]) / 2
						oldQty := hp.Inventory[salvageGood] - salvageQty
						oldAvg := hp.InventoryAvgCost[salvageGood]
						if oldQty > 0 {
							newAvg := (oldQty*oldAvg + salvageQty*avgPrice) / hp.Inventory[salvageGood]
							hp.InventoryAvgCost[salvageGood] = newAvg
						} else {
							hp.InventoryAvgCost[salvageGood] = avgPrice
						}
					}
					gs.logAction(room, hp, fmt.Sprintf("Salvage discovered: found %d %s", salvageQty, salvageGood))
					gs.enqueueModal(hp, "Salvage Discovery", "You found abandoned cargo: "+strconv.Itoa(salvageQty)+" "+salvageGood+" floating in space!")
				}
			}

			// Fuel leak: ~0.3% chance per turn - lose some fuel
			if rand.Intn(330) == 0 && hp.Fuel > 10 {
				fuelLoss := 5 + rand.Intn(16) // lose 5-20 fuel
				if fuelLoss > hp.Fuel-5 {     // always leave at least 5 fuel
					fuelLoss = hp.Fuel - 5
				}
				if fuelLoss > 0 {
					hp.Fuel -= fuelLoss
					gs.logAction(room, hp, fmt.Sprintf("Fuel leak: lost %d fuel units", fuelLoss))
					gs.enqueueModal(hp, "Fuel Leak", "A micro-meteorite punctured your fuel tank. You lost "+strconv.Itoa(fuelLoss)+" fuel units.")
				}
			}

			// Trade guild membership offer: ~0.2% chance per turn - pay for ongoing benefits
			if rand.Intn(500) == 0 {
				membershipFee := 2500 + rand.Intn(2501) // 2500-5000 credits
				// This could provide ongoing small benefits (not implemented here)
				gs.logAction(room, hp, fmt.Sprintf("Trade guild membership offered for $%d", membershipFee))
				gs.enqueueModal(hp, "Trade Guild Invitation", "The Galactic Traders Guild invites you to join for "+strconv.Itoa(membershipFee)+" credits. Membership provides access to exclusive routes and better fuel prices. (This is currently just flavor - no actual benefits implemented)")
			}

			if !hp.IsBot && len(room.Players) > 1 && !playerHasModalOfKind(hp, "shady-contract") && !roomHasPendingBlackOps(room, hp.ID) {
				if rand.Intn(400) == 0 { // ~0.25% chance per turn
					price := 3000 + rand.Intn(3001)
					body := fmt.Sprintf("A shady character offers to \"take care\" of your competition for %d credits.\nRumors whisper that some of these deals are Federation stings. Pay them?", price)
					mi := ModalItem{ID: randID(), Title: "Shadowy Proposition", Body: body, Kind: "shady-contract", Price: price}
					hp.Modals = append(hp.Modals, mi)
					gs.logAction(room, hp, fmt.Sprintf("Received shady contract offer for $%d", price))
				}
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

		if len(room.PendingBlackOps) > 0 {
			remaining := make([]*BlackOpsContract, 0, len(room.PendingBlackOps))
			for _, contract := range room.PendingBlackOps {
				if contract == nil {
					continue
				}
				resolved := true
				for pid, pl := range room.Players {
					if pid == contract.Instigator {
						continue
					}
					if pl == nil || pl.Bankrupt {
						continue
					}
					if contract.Applied == nil || !contract.Applied[pid] {
						resolved = false
						break
					}
				}
				if resolved {
					if inst := room.Players[contract.Instigator]; inst != nil && !inst.IsBot && !inst.Bankrupt {
						gs.enqueueModal(inst, "Satisfied Whisper", "Your rivals suffered a streak of unexplained setbacks. No one traced it back to you.")
					}
					if inst := room.Players[contract.Instigator]; inst != nil {
						gs.logAction(room, inst, "Shady contract resolved without exposure")
					}
				} else {
					remaining = append(remaining, contract)
				}
			}
			room.PendingBlackOps = remaining
		}

		// Handle federation auctions at the end of turn processing
		gs.handleFederationAuctions(room)

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

func (gs *GameServer) handleAuctionBid(room *Room, p *Player, auctionID string, bid int) {
	if bid <= 0 {
		return
	}
	room.mu.Lock()
	defer func() { room.mu.Unlock(); gs.sendRoomState(room, nil) }()

	log.Printf("Room %s: Player %s attempting to bid %d for auction %s", room.ID, p.Name, bid, auctionID)

	// Check if auction exists and is still active
	if room.ActiveAuction == nil {
		log.Printf("Room %s: Auction bid rejected - no active auction", room.ID)
		gs.enqueueModal(p, "Auction Ended", "This auction is no longer active.")
		return
	}
	if room.ActiveAuction.ID != auctionID {
		log.Printf("Room %s: Auction bid rejected - ID mismatch (active: %s, requested: %s)",
			room.ID, room.ActiveAuction.ID, auctionID)
		gs.enqueueModal(p, "Auction Ended", "This auction is no longer active.")
		return
	}

	// Check if player can afford the bid
	if p.Money < bid {
		log.Printf("Room %s: Player %s cannot afford bid %d (has %d)", room.ID, p.Name, bid, p.Money)
		gs.enqueueModal(p, "Insufficient Funds", "You don't have enough credits for this bid.")
		return
	}

	// Record the bid
	room.ActiveAuction.Bids[p.ID] = bid
	log.Printf("Room %s: Recorded bid %d from player %s for auction %s", room.ID, bid, p.Name, auctionID)
	gs.logAction(room, p, fmt.Sprintf("Placed auction bid of $%d for %s on %s", bid, room.ActiveAuction.FacilityType, room.ActiveAuction.Planet))

	// Confirm bid to player
	gs.enqueueModal(p, "Bid Placed", fmt.Sprintf("Your bid of $%d for the %s on %s has been recorded.", bid, room.ActiveAuction.FacilityType, room.ActiveAuction.Planet))
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
	playersBase := []map[string]interface{}{}
	for _, pp := range room.Players {
		displayMoney := pp.Money
		moneyField := interface{}(displayMoney)
		if pp.Bankrupt {
			moneyField = "Bankrupt"
		}
		cargoValue := inventoryValue(pp.Inventory, pp.InventoryAvgCost)
		upgradeValue := pp.UpgradeInvestment
		facilityValue := pp.FacilityInvestment
		playersBase = append(playersBase, map[string]interface{}{
			"id":                 pp.ID,
			"name":               pp.Name,
			"money":              moneyField,
			"cashValue":          displayMoney,
			"currentPlanet":      pp.CurrentPlanet,
			"destinationPlanet":  pp.DestinationPlanet,
			"ready":              pp.Ready,
			"endGame":            pp.EndGame,
			"bankrupt":           pp.Bankrupt,
			"cargoValue":         cargoValue,
			"upgradeValue":       upgradeValue,
			"facilityInvestment": facilityValue,
		})
	}

	facilityOverview := map[string][]map[string]interface{}{}
	for planetName, planet := range room.Planets {
		if planet == nil || len(planet.Facilities) == 0 {
			continue
		}
		entries := make([]map[string]interface{}, 0, len(planet.Facilities))
		for _, facility := range planet.Facilities {
			if facility == nil {
				continue
			}
			ownerName := facility.OwnerName
			if ownerName == "" {
				if op := room.Players[facility.Owner]; op != nil {
					ownerName = op.Name
				}
			}
			entries = append(entries, map[string]interface{}{
				"id":            facility.ID,
				"type":          facility.Type,
				"ownerId":       facility.Owner,
				"ownerName":     ownerName,
				"usageCharge":   facility.UsageCharge,
				"accruedMoney":  facility.AccruedMoney,
				"purchasePrice": facility.PurchasePrice,
			})
		}
		if len(entries) > 0 {
			facilityOverview[planetName] = entries
		}
	}
	const unknownLabel = "Uncharted Space"

	filterPlanets := func(known map[string]bool) ([]string, map[string]map[string]float64) {
		orderSource := room.PlanetOrder
		if len(orderSource) == 0 {
			orderSource = planetNames(room.Planets)
		}
		list := make([]string, 0, len(known))
		for _, name := range orderSource {
			if known[name] {
				list = append(list, name)
			}
		}
		positions := make(map[string]map[string]float64, len(list))
		for _, name := range list {
			if pos, ok := room.PlanetPositions[name]; ok {
				positions[name] = map[string]float64{"x": pos[0], "y": pos[1]}
			}
		}
		return list, positions
	}

	filterFacilities := func(known map[string]bool) map[string][]map[string]interface{} {
		if len(facilityOverview) == 0 {
			return nil
		}
		out := make(map[string][]map[string]interface{}, len(facilityOverview))
		for planet, entries := range facilityOverview {
			if !known[planet] {
				continue
			}
			clone := make([]map[string]interface{}, 0, len(entries))
			for _, entry := range entries {
				if entry == nil {
					continue
				}
				copyEntry := make(map[string]interface{}, len(entry))
				for k, v := range entry {
					copyEntry[k] = v
				}
				clone = append(clone, copyEntry)
			}
			if len(clone) > 0 {
				out[planet] = clone
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}

	filterNews := func(known map[string]bool) []map[string]interface{} {
		if len(room.News) == 0 {
			return nil
		}
		arr := make([]map[string]interface{}, 0, len(room.News))
		for _, n := range room.News {
			if n.Planet != "" && !known[n.Planet] {
				continue
			}
			arr = append(arr, map[string]interface{}{
				"headline":       n.Headline,
				"planet":         n.Planet,
				"turnsRemaining": n.TurnsRemaining,
			})
		}
		if len(arr) == 0 {
			return nil
		}
		return arr
	}

	buildPlayersView := func(base []map[string]interface{}, known map[string]bool, selfID PlayerID) []map[string]interface{} {
		out := make([]map[string]interface{}, 0, len(base))
		for _, entry := range base {
			if entry == nil {
				continue
			}
			clone := make(map[string]interface{}, len(entry))
			for k, v := range entry {
				clone[k] = v
			}
			var idVal PlayerID
			switch v := clone["id"].(type) {
			case PlayerID:
				idVal = v
			case string:
				idVal = PlayerID(v)
			}
			if idVal != selfID {
				if cp, ok := clone["currentPlanet"].(string); ok && cp != "" && !known[cp] {
					clone["currentPlanet"] = unknownLabel
				}
				if dp, ok := clone["destinationPlanet"].(string); ok && dp != "" && !known[dp] {
					clone["destinationPlanet"] = unknownLabel
				}
			}
			out = append(out, clone)
		}
		return out
	}
	payloadByPlayer := map[PlayerID]interface{}{}
	recipients := make([]*Player, 0, len(room.Players))
	for id, pp := range room.Players {
		if pp.KnownPlanets == nil {
			pp.KnownPlanets = make(map[string]bool)
		}
		if pp.CurrentPlanet != "" {
			pp.KnownPlanets[pp.CurrentPlanet] = true
		}
		knownOrder, positionsView := filterPlanets(pp.KnownPlanets)
		facilitiesView := filterFacilities(pp.KnownPlanets)
		newsView := filterNews(pp.KnownPlanets)
		playersView := buildPlayersView(playersBase, pp.KnownPlanets, id)
		marketMemoryView := buildMarketPayload(pp.MarketMemory, pp.KnownPlanets)
		knownList := make([]string, len(knownOrder))
		copy(knownList, knownOrder)
		if room.Private && !pp.IsBot {
			log.Printf("[RoomState] room=%s player=%s knownMap=%d knownOrder=%d preview=[%s]", room.ID, pp.ID, len(pp.KnownPlanets), len(knownOrder), sampleNames(knownOrder, 12))
		}

		if pp.Bankrupt {
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
				if pp.Modals[0].AuctionID != "" {
					nm["auctionId"] = pp.Modals[0].AuctionID
				}
				if pp.Modals[0].FacilityType != "" {
					nm["facilityType"] = pp.Modals[0].FacilityType
				}
				if pp.Modals[0].Planet != "" {
					nm["planet"] = pp.Modals[0].Planet
				}
				if pp.Modals[0].UsageCharge != 0 {
					nm["usageCharge"] = pp.Modals[0].UsageCharge
				}
				if pp.Modals[0].SuggestedBid != 0 {
					nm["suggestedBid"] = pp.Modals[0].SuggestedBid
				}
			} else {
				nm = map[string]interface{}{}
			}
			payloadByPlayer[id] = map[string]interface{}{
				"room": map[string]interface{}{
					"id":         room.ID,
					"name":       room.Name,
					"started":    room.Started,
					"turn":       room.Turn,
					"players":    playersView,
					"turnEndsAt": room.TurnEndsAt.UnixMilli(),
					"allReady":   allReady,
					"planets":    knownOrder,
					"planetPositions": func() map[string]map[string]float64 {
						if len(positionsView) == 0 {
							return nil
						}
						return positionsView
					}(),
					"news":       newsView,
					"facilities": facilitiesView,
					"world": map[string]float64{
						"width":     worldWidth,
						"height":    worldHeight,
						"unitScale": distanceUnitScale,
					},
				},
				"you": map[string]interface{}{
					"id":                 pp.ID,
					"name":               pp.Name,
					"money":              "Bankrupt",
					"cashValue":          pp.Money,
					"inventory":          map[string]int{},
					"inventoryAvgCost":   map[string]int{},
					"currentPlanet":      pp.CurrentPlanet,
					"destinationPlanet":  "",
					"ready":              false,
					"endGame":            false,
					"fuel":               0,
					"inTransit":          false,
					"transitFrom":        "",
					"transitRemaining":   0,
					"transitTotal":       0,
					"capacity":           shipCapacity + pp.CapacityBonus,
					"fuelCapacity":       fuelCapacity + pp.FuelCapacityBonus,
					"speedPerTurn":       20 + pp.SpeedBonus,
					"facilityInvestment": pp.FacilityInvestment,
					"upgradeInvestment":  pp.UpgradeInvestment,
					"upgradeValue":       pp.UpgradeInvestment,
					"cargoValue":         0,
					"modal":              nm,
					"marketMemory":       marketMemoryView,
					"knownPlanets":       knownList,
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
			visGoods := map[string]int{}
			for k, v := range planet.Goods {
				visGoods[k] = v
			}
			for g := range pp.Inventory {
				if _, ok := visGoods[g]; !ok {
					visGoods[g] = 0
				}
			}
			visPrices := map[string]int{}
			for k, v := range planet.Prices {
				visPrices[k] = v
			}
			ranges := defaultPriceRanges()
			visRanges := map[string][2]int{}
			for g := range visGoods {
				if r, ok := ranges[g]; ok {
					visRanges[g] = r
				}
			}
			if pp.MarketMemory == nil {
				pp.MarketMemory = make(map[string]*MarketSnapshot)
			}
			goodsCopy := cloneIntMap(visGoods)
			pricesCopy := cloneIntMap(visPrices)
			rangeCopy := make(map[string][2]int, len(visRanges))
			for g, rng := range visRanges {
				rangeCopy[g] = rng
			}
			pp.MarketMemory[planet.Name] = &MarketSnapshot{
				Turn:        room.Turn,
				UpdatedAt:   time.Now().UnixMilli(),
				Goods:       goodsCopy,
				Prices:      pricesCopy,
				PriceRanges: rangeCopy,
				FuelPrice:   planet.FuelPrice,
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
			if pp.Modals[0].AuctionID != "" {
				nm["auctionId"] = pp.Modals[0].AuctionID
			}
			if pp.Modals[0].FacilityType != "" {
				nm["facilityType"] = pp.Modals[0].FacilityType
			}
			if pp.Modals[0].Planet != "" {
				nm["planet"] = pp.Modals[0].Planet
			}
			if pp.Modals[0].UsageCharge != 0 {
				nm["usageCharge"] = pp.Modals[0].UsageCharge
			}
			if pp.Modals[0].SuggestedBid != 0 {
				nm["suggestedBid"] = pp.Modals[0].SuggestedBid
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
				"players":    playersView,
				"turnEndsAt": room.TurnEndsAt.UnixMilli(),
				"private":    room.Private,
				"paused":     room.Paused,
				"creatorId":  string(room.CreatorID),
				"allReady":   allReady,
				"planets":    knownOrder,
				"planetPositions": func() map[string]map[string]float64 {
					if len(positionsView) == 0 {
						return nil
					}
					return positionsView
				}(),
				"news":       newsView,
				"facilities": facilitiesView,
				"world": map[string]float64{
					"width":     worldWidth,
					"height":    worldHeight,
					"unitScale": distanceUnitScale,
				},
			},
			"you": map[string]interface{}{
				"id":                 pp.ID,
				"name":               pp.Name,
				"money":              pp.Money,
				"cashValue":          pp.Money,
				"inventory":          cloneIntMap(pp.Inventory),
				"inventoryAvgCost":   cloneIntMap(pp.InventoryAvgCost),
				"currentPlanet":      pp.CurrentPlanet,
				"destinationPlanet":  pp.DestinationPlanet,
				"ready":              pp.Ready,
				"endGame":            pp.EndGame,
				"fuel":               pp.Fuel,
				"inTransit":          pp.InTransit,
				"transitFrom":        pp.TransitFrom,
				"transitRemaining":   pp.TransitRemaining,
				"transitTotal":       pp.TransitTotal,
				"capacity":           shipCapacity + pp.CapacityBonus,
				"fuelCapacity":       fuelCapacity + pp.FuelCapacityBonus,
				"speedPerTurn":       20 + pp.SpeedBonus,
				"facilityInvestment": pp.FacilityInvestment,
				"upgradeInvestment":  pp.UpgradeInvestment,
				"upgradeValue":       pp.UpgradeInvestment,
				"cargoValue":         inventoryValue(pp.Inventory, pp.InventoryAvgCost),
				"modal":              nextModal,
				"marketMemory":       marketMemoryView,
				"knownPlanets":       knownList,
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

func generateLocationNames(count int) []string {
	used := make(map[string]bool)
	names := make([]string, 0, count)
	for len(names) < count {
		name := randomLocationName(used)
		if !used[name] {
			used[name] = true
			names = append(names, name)
		}
	}
	return names
}

func randomLocationName(used map[string]bool) string {
	for tries := 0; tries < 500; tries++ {
		prefix := locationPrefixes[rand.Intn(len(locationPrefixes))]
		core := locationCores[rand.Intn(len(locationCores))]
		suffix := locationSuffixes[rand.Intn(len(locationSuffixes))]
		name := strings.TrimSpace(fmt.Sprintf("%s %s %s", prefix, core, suffix))
		if rand.Intn(4) == 0 {
			qual := locationQualifiers[rand.Intn(len(locationQualifiers))]
			name = strings.TrimSpace(name + " " + qual)
		}
		if !used[name] {
			return name
		}
	}
	return "Sector " + strings.ToUpper(randID())
}

func pickUniqueGoods(pool []string, count int) []string {
	if count <= 0 || len(pool) == 0 {
		return nil
	}
	if count >= len(pool) {
		out := make([]string, len(pool))
		copy(out, pool)
		rand.Shuffle(len(out), func(i, j int) {
			out[i], out[j] = out[j], out[i]
		})
		return out
	}
	used := make(map[string]bool)
	out := make([]string, 0, count)
	for len(out) < count {
		candidate := pool[rand.Intn(len(pool))]
		if used[candidate] {
			continue
		}
		used[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func defaultPlanets() map[string]*Planet {
	names := generateLocationNames(worldPlanetCount)
	ranges := defaultPriceRanges()

	allGoodsSet := map[string]struct{}{}
	for _, g := range standardGoods {
		allGoodsSet[g] = struct{}{}
	}
	for _, g := range uniqueGoodsPool {
		allGoodsSet[g] = struct{}{}
	}
	allGoods := make([]string, 0, len(allGoodsSet))
	for g := range allGoodsSet {
		allGoods = append(allGoods, g)
	}
	sort.Strings(allGoods)

	m := make(map[string]*Planet, len(names))
	for _, n := range names {
		goods := make(map[string]int)
		prices := make(map[string]int)
		prod := make(map[string]int)
		trend := make(map[string]int)

		for _, g := range standardGoods {
			goods[g] = 18 + rand.Intn(28)
			if r, ok := ranges[g]; ok {
				prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
			} else {
				prices[g] = 12 + rand.Intn(18)
			}
			prod[g] = 2 + rand.Intn(4)
			trend[g] = 0
		}

		uniqueCount := 2 + rand.Intn(2) // 2-3 unique goods per location
		for _, g := range pickUniqueGoods(uniqueGoodsPool, uniqueCount) {
			goods[g] = 8 + rand.Intn(18)
			prod[g] = 1 + rand.Intn(3)
			trend[g] = 0
			if r, ok := ranges[g]; ok {
				prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
			} else {
				prices[g] = 14 + rand.Intn(20)
			}
		}

		for _, g := range allGoods {
			if _, ok := prices[g]; !ok {
				if r, ok := ranges[g]; ok {
					prices[g] = r[0] + rand.Intn(r[1]-r[0]+1)
				} else {
					prices[g] = 10 + rand.Intn(22)
				}
			}
			if _, ok := trend[g]; !ok {
				trend[g] = 0
			}
		}

		basePrices := make(map[string]int, len(prices))
		for k, v := range prices {
			basePrices[k] = v
		}
		baseProd := make(map[string]int, len(prod))
		for k, v := range prod {
			baseProd[k] = v
		}

		fp := 7 + rand.Intn(7) // 7..13
		m[n] = &Planet{
			Name:          n,
			Goods:         goods,
			Prices:        prices,
			Prod:          prod,
			BasePrices:    basePrices,
			BaseProd:      baseProd,
			PriceTrend:    trend,
			FuelPrice:     fp,
			BaseFuelPrice: fp,
			Facilities:    []*Facility{},
		}
	}

	return m
}

// defaultPriceRanges returns a static min/max range for each good.
// Standard goods have a slightly lower range; unique goods are a bit higher.
func defaultPriceRanges() map[string][2]int {
	m := make(map[string][2]int)
	combined := make([]string, 0, len(standardGoods)+len(uniqueGoodsPool))
	combined = append(combined, standardGoods...)
	combined = append(combined, uniqueGoodsPool...)
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

func cloneMarketMemory(in map[string]*MarketSnapshot) map[string]*MarketSnapshot {
	if in == nil {
		return nil
	}
	out := make(map[string]*MarketSnapshot, len(in))
	for planet, snap := range in {
		if snap == nil {
			continue
		}
		copySnap := &MarketSnapshot{
			Turn:      snap.Turn,
			UpdatedAt: snap.UpdatedAt,
			FuelPrice: snap.FuelPrice,
		}
		if snap.Goods != nil {
			copySnap.Goods = cloneIntMap(snap.Goods)
		}
		if snap.Prices != nil {
			copySnap.Prices = cloneIntMap(snap.Prices)
		}
		if snap.PriceRanges != nil {
			ranges := make(map[string][2]int, len(snap.PriceRanges))
			for g, rng := range snap.PriceRanges {
				ranges[g] = rng
			}
			copySnap.PriceRanges = ranges
		}
		out[planet] = copySnap
	}
	return out
}

func buildMarketPayload(memory map[string]*MarketSnapshot, known map[string]bool) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})
	if len(memory) == 0 {
		return result
	}
	for planet, snapshot := range memory {
		if known != nil && !known[planet] {
			continue
		}
		if snapshot == nil {
			continue
		}
		entry := map[string]interface{}{
			"turn":      snapshot.Turn,
			"updatedAt": snapshot.UpdatedAt,
			"fuelPrice": snapshot.FuelPrice,
		}
		if snapshot.Goods != nil {
			entry["goods"] = cloneIntMap(snapshot.Goods)
		} else {
			entry["goods"] = map[string]int{}
		}
		if snapshot.Prices != nil {
			entry["prices"] = cloneIntMap(snapshot.Prices)
		} else {
			entry["prices"] = map[string]int{}
		}
		if snapshot.PriceRanges != nil {
			ranges := make(map[string][2]int, len(snapshot.PriceRanges))
			for g, rng := range snapshot.PriceRanges {
				ranges[g] = rng
			}
			entry["priceRanges"] = ranges
		} else {
			entry["priceRanges"] = map[string][2]int{}
		}
		result[planet] = entry
	}
	return result
}

func cloneModals(in []ModalItem) []ModalItem {
	if in == nil {
		return nil
	}
	out := make([]ModalItem, len(in))
	copy(out, in)
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sampleNames(names []string, limit int) string {
	if len(names) == 0 {
		return ""
	}
	if limit <= 0 {
		limit = 5
	}
	if len(names) > limit {
		names = names[:limit]
	}
	return strings.Join(names, ", ")
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

func inventoryValue(inv map[string]int, avg map[string]int) int {
	if inv == nil {
		return 0
	}
	value := 0
	for good, qty := range inv {
		if qty <= 0 {
			continue
		}
		price := 0
		if avg != nil {
			price = avg[good]
		}
		value += qty * price
	}
	return value
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

func playerHasModalOfKind(p *Player, kind string) bool {
	if p == nil {
		return false
	}
	for _, m := range p.Modals {
		if m.Kind == kind {
			return true
		}
	}
	return false
}

func roomHasPendingBlackOps(room *Room, instigator PlayerID) bool {
	if room == nil {
		return false
	}
	for _, contract := range room.PendingBlackOps {
		if contract == nil {
			continue
		}
		if contract.Instigator == instigator {
			return true
		}
	}
	return false
}

func (gs *GameServer) applyBlackOpsHit(room *Room, contract *BlackOpsContract, target *Player) {
	if room == nil || contract == nil || target == nil {
		return
	}
	if contract.Applied == nil {
		contract.Applied = make(map[PlayerID]bool)
	}
	if contract.Applied[target.ID] {
		return
	}

	var (
		desc    string
		title   string
		body    string
		applied bool
	)

	tryCargo := func() bool {
		if len(target.Inventory) == 0 {
			return false
		}
		goods := make([]string, 0, len(target.Inventory))
		for g, qty := range target.Inventory {
			if qty > 0 {
				goods = append(goods, g)
			}
		}
		if len(goods) == 0 {
			return false
		}
		good := goods[rand.Intn(len(goods))]
		qty := target.Inventory[good]
		if qty <= 0 {
			return false
		}
		maxLoss := minInt(qty, 6)
		if maxLoss <= 0 {
			return false
		}
		lost := 1 + rand.Intn(maxLoss)
		target.Inventory[good] -= lost
		if target.Inventory[good] <= 0 {
			delete(target.Inventory, good)
			delete(target.InventoryAvgCost, good)
		}
		desc = fmt.Sprintf("lost %d %s cargo", lost, good)
		title = "Cargo Ransacked"
		body = fmt.Sprintf("Dock crews report %d units of %s vanished overnight. No witnesses were found.", lost, good)
		return true
	}

	tryFuel := func() bool {
		if target.Fuel <= 10 {
			return false
		}
		maxLoss := target.Fuel - 5
		if maxLoss <= 0 {
			return false
		}
		loss := 10 + rand.Intn(16) // 10-25 units
		if loss > maxLoss {
			loss = maxLoss
		}
		if loss <= 0 {
			return false
		}
		target.Fuel -= loss
		desc = fmt.Sprintf("lost %d fuel units", loss)
		title = "Fuel Sabotage"
		body = fmt.Sprintf("Maintenance crews discover %d units of fuel contaminated overnight. Sabotage is suspected, but no culprit was identified.", loss)
		return true
	}

	tryCredits := func() bool {
		loss := 800 + rand.Intn(1601) // 800-2400 credits
		target.Money -= loss
		desc = fmt.Sprintf("bled %d credits to mysterious mishaps", loss)
		title = "Costly Mishap"
		body = fmt.Sprintf("A cascade of unfortunate incidents drains %d credits from your accounts. Authorities have no leads.", loss)
		return true
	}

	for _, choice := range rand.Perm(3) {
		switch choice {
		case 0:
			applied = tryCargo()
		case 1:
			applied = tryFuel()
		case 2:
			applied = tryCredits()
		}
		if applied {
			break
		}
	}
	if !applied {
		applied = tryCredits()
	}
	if !applied {
		return
	}

	contract.Applied[target.ID] = true
	gs.logAction(room, target, fmt.Sprintf("Mysterious setback: %s", desc))
	if !target.IsBot {
		gs.enqueueModal(target, title, body)
	}
	if inst := room.Players[contract.Instigator]; inst != nil {
		gs.logAction(room, inst, fmt.Sprintf("Black ops impacted %s (%s)", target.Name, desc))
	}
}

// generatePlanetPositions returns normalized positions in [0,1]x[0,1] with a minimal spacing
func generatePlanetPositions(names []string) map[string][2]float64 {
	m := make(map[string][2]float64, len(names))
	if len(names) == 0 {
		return m
	}
	placed := make([][2]float64, 0, len(names))
	for _, n := range names {
		var x, y float64
		ok := false
		for tries := 0; tries < 400; tries++ {
			x = rand.Float64() * worldWidth
			y = rand.Float64() * worldHeight
			good := true
			for _, p := range placed {
				if math.Hypot(p[0]-x, p[1]-y) < minPlanetSpacingUnits {
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
			x = rand.Float64() * worldWidth
			y = rand.Float64() * worldHeight
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
	if distanceUnitScale <= 0 {
		return int(math.Ceil(d))
	}
	units := int(math.Ceil(d / distanceUnitScale))
	if units < 1 {
		units = 1
	}
	return units
}

func chooseStartingPlanet(room *Room) string {
	if room == nil || len(room.Planets) == 0 {
		return ""
	}
	occupied := make(map[string]bool)
	for _, pl := range room.Players {
		if pl.CurrentPlanet != "" {
			occupied[pl.CurrentPlanet] = true
		}
		if pl.DestinationPlanet != "" {
			occupied[pl.DestinationPlanet] = true
		}
	}
	candidates := make([]string, 0, len(room.Planets))
	for name := range room.Planets {
		if !occupied[name] {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		candidates = planetNames(room.Planets)
	}
	return candidates[rand.Intn(len(candidates))]
}

func discoverAroundPosition(room *Room, p *Player, center [2]float64, radius float64) []string {
	if room == nil || p == nil || radius <= 0 {
		return nil
	}
	if p.KnownPlanets == nil {
		p.KnownPlanets = make(map[string]bool)
	}
	positions := room.PlanetPositions
	if len(positions) == 0 {
		return nil
	}
	r2 := radius * radius
	added := make([]string, 0)
	for name, pos := range positions {
		dx := pos[0] - center[0]
		dy := pos[1] - center[1]
		if dx*dx+dy*dy <= r2 {
			if !p.KnownPlanets[name] {
				p.KnownPlanets[name] = true
				added = append(added, name)
			}
		}
	}
	if len(added) > 1 {
		sort.Strings(added)
	}
	return added
}

func (gs *GameServer) updateDiscoveryForPlayer(room *Room, p *Player) {
	if room == nil || p == nil {
		return
	}
	if p.KnownPlanets == nil {
		p.KnownPlanets = make(map[string]bool)
	}
	positions := room.PlanetPositions
	if len(positions) == 0 {
		return
	}
	newly := make([]string, 0, 4)
	if pos, ok := positions[p.CurrentPlanet]; ok {
		newly = append(newly, discoverAroundPosition(room, p, pos, discoveryRadiusStation)...)
	}
	if p.InTransit && p.TransitTotal > 0 && p.TransitFrom != "" && p.DestinationPlanet != "" {
		start, okStart := positions[p.TransitFrom]
		dest, okDest := positions[p.DestinationPlanet]
		if okStart && okDest {
			progress := 1 - float64(p.TransitRemaining)/math.Max(float64(p.TransitTotal), 1)
			if progress < 0 {
				progress = 0
			}
			if progress > 1 {
				progress = 1
			}
			mid := [2]float64{
				start[0] + (dest[0]-start[0])*progress,
				start[1] + (dest[1]-start[1])*progress,
			}
			newly = append(newly, discoverAroundPosition(room, p, mid, discoveryRadiusTransit)...)
		}
	}
	if len(newly) == 0 {
		return
	}
	sort.Strings(newly)
	unique := newly[:0]
	var last string
	for _, name := range newly {
		if name == last {
			continue
		}
		unique = append(unique, name)
		last = name
	}
	if len(unique) == 0 {
		return
	}
	var message string
	if len(unique) == 1 {
		message = "Charted new location: " + unique[0]
	} else if len(unique) <= 3 {
		message = "Charted new locations: " + strings.Join(unique, ", ")
	} else {
		message = fmt.Sprintf("Charted %d new locations", len(unique))
	}
	gs.logAction(room, p, message)
	if !p.IsBot {
		gs.enqueueModal(p, "New Sector Charted", message)
	}
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

// handleFederationAuctions manages facility auctions across all turns
func (gs *GameServer) handleFederationAuctions(room *Room) {
	// Check if an auction is ending
	if room.ActiveAuction != nil {
		log.Printf("Room %s: Processing auction %s, turns left: %d", room.ID, room.ActiveAuction.ID, room.ActiveAuction.TurnsLeft)
		room.ActiveAuction.TurnsLeft--

		if room.ActiveAuction.TurnsLeft <= 0 {
			log.Printf("Room %s: Auction %s ending with %d bids", room.ID, room.ActiveAuction.ID, len(room.ActiveAuction.Bids))
			// Auction ends - determine winner and create facility
			gs.endFederationAuction(room)
			room.ActiveAuction = nil
		}
	}

	// Randomly start new auctions (~2% chance per turn)
	if room.ActiveAuction == nil && rand.Intn(50) == 0 {
		log.Printf("Room %s: Starting new federation auction", room.ID)
		gs.startFederationAuction(room)
	}
}

// startFederationAuction creates a new facility auction
func (gs *GameServer) startFederationAuction(room *Room) {
	// Select random planet that still has capacity for additional facilities
	availablePlanets := []string{}
	minFacilities := math.MaxInt
	for name, planet := range room.Planets {
		if planet == nil {
			continue
		}
		count := len(planet.Facilities)
		if count >= maxFacilitiesPerPlanet {
			continue
		}
		if count < minFacilities {
			minFacilities = count
			availablePlanets = []string{name}
		} else if count == minFacilities {
			availablePlanets = append(availablePlanets, name)
		}
	}

	if len(availablePlanets) == 0 {
		return // All planets reached facility capacity
	}

	planet := availablePlanets[rand.Intn(len(availablePlanets))]

	// Select random facility type with usage charge
	facilityTypes := []struct {
		name   string
		charge int
	}{
		{"Mining Station", 25 + rand.Intn(26)}, // 25-50 per turn
		{"Trade Hub", 15 + rand.Intn(21)},      // 15-35 per turn
		{"Refinery", 20 + rand.Intn(31)},       // 20-50 per turn
		{"Research Lab", 30 + rand.Intn(21)},   // 30-50 per turn
		{"Repair Dock", 10 + rand.Intn(16)},    // 10-25 per turn
		{"Fuel Depot", 8 + rand.Intn(13)},      // 8-20 per turn
	}

	facility := facilityTypes[rand.Intn(len(facilityTypes))]
	suggestedBid := facility.charge * 10

	// Create auction
	auctionID := fmt.Sprintf("auction_%d_%d", room.Turn, rand.Intn(1000))
	room.ActiveAuction = &FederationAuction{
		ID:           auctionID,
		FacilityType: facility.name,
		Planet:       planet,
		UsageCharge:  facility.charge,
		SuggestedBid: suggestedBid,
		Bids:         make(map[PlayerID]int),
		TurnsLeft:    1, // Auction lasts for the next turn
	}

	log.Printf("Room %s: Created auction %s for %s on %s (charge: %d, suggested bid: %d)",
		room.ID, auctionID, facility.name, planet, facility.charge, suggestedBid)

	// Send auction modal to all players
	for _, p := range room.Players {
		gs.enqueueModal(p, "Federation Facility Auction",
			fmt.Sprintf("The Galactic Federation is auctioning a %s on %s. Non-owners will pay %d credits per turn when docking. Enter your bid below.",
				facility.name, planet, facility.charge),
		)

		// Set modal with auction details
		if len(p.Modals) > 0 {
			lastModal := &p.Modals[len(p.Modals)-1]
			lastModal.Kind = "auction"
			lastModal.AuctionID = auctionID
			lastModal.FacilityType = facility.name
			lastModal.Planet = planet
			lastModal.UsageCharge = facility.charge
			lastModal.SuggestedBid = suggestedBid
		}

		// Auto-bid for bots
		if p.IsBot && !p.Bankrupt {
			// Bots bid around the suggested amount but never more than they can afford
			maxBid := p.Money - 200 // Keep 200 credits as buffer
			if maxBid > 0 {
				// Bid 80-120% of suggested, capped at what they can afford
				bidVariation := suggestedBid + rand.Intn(suggestedBid/2) - suggestedBid/4
				bid := minInt(bidVariation, maxBid)
				if bid > 0 {
					room.ActiveAuction.Bids[p.ID] = bid
					gs.logAction(room, p, fmt.Sprintf("Auto-placed auction bid of $%d for %s on %s", bid, facility.name, planet))
				}
			}
		}
	}

	gs.logGeneral(room, fmt.Sprintf("Federation auction started: %s on %s", facility.name, planet))
}

// endFederationAuction determines winner and creates the facility
func (gs *GameServer) endFederationAuction(room *Room) {
	if room.ActiveAuction == nil {
		return
	}

	auction := room.ActiveAuction

	// Find highest bidder
	var winner PlayerID
	highestBid := 0

	for playerID, bid := range auction.Bids {
		if bid > highestBid {
			winner = playerID
			highestBid = bid
		}
	}

	// Create facility and charge winner
	if winner != "" && highestBid > 0 {
		winnerPlayer := room.Players[winner]
		if winnerPlayer != nil && winnerPlayer.Money >= highestBid {
			// Charge the winner
			winnerPlayer.Money -= highestBid
			winnerPlayer.FacilityInvestment += highestBid

			// Create the facility
			planet := room.Planets[auction.Planet]
			if planet != nil {
				if planet.Facilities == nil {
					planet.Facilities = []*Facility{}
				}
				facID := fmt.Sprintf("facility_%s_%d_%d", strings.ReplaceAll(strings.ToLower(auction.Planet), " ", "_"), room.Turn, rand.Intn(1000))
				newFacility := &Facility{
					ID:            facID,
					Type:          auction.FacilityType,
					Owner:         winner,
					OwnerName:     winnerPlayer.Name,
					UsageCharge:   auction.UsageCharge,
					AccruedMoney:  0,
					PurchasePrice: highestBid,
				}
				planet.Facilities = append(planet.Facilities, newFacility)
				// Determine second-highest bid (if any)
				var secondID PlayerID
				secondBid := 0
				for playerID, bid := range auction.Bids {
					if playerID == winner {
						continue
					}
					if bid > secondBid {
						secondBid = bid
						secondID = playerID
					}
				}
				secondName := "another bidder"
				if secondID != "" {
					if sp := room.Players[secondID]; sp != nil {
						secondName = sp.Name
					}
				} else {
					secondName = "no competing bids"
				}

				// Announce winner to all players
				for _, p := range room.Players {
					if p.ID == winner {
						msg := fmt.Sprintf("Congratulations! You won the %s on %s for %d credits. You'll collect %d credits per turn from other players who dock there.",
							auction.FacilityType, auction.Planet, highestBid, auction.UsageCharge)
						if secondBid > 0 {
							msg += fmt.Sprintf(" The next highest bid was %d credits from %s.", secondBid, secondName)
						} else {
							msg += " There were no competing bids."
						}
						gs.enqueueModal(p, "Auction Won!", msg)
					} else {
						msg := fmt.Sprintf("%s won the %s on %s for %d credits.",
							winnerPlayer.Name, auction.FacilityType, auction.Planet, highestBid)
						if secondBid > 0 {
							msg += fmt.Sprintf(" The next highest bid was %d credits from %s.", secondBid, secondName)
						} else {
							msg += " No other bids were placed."
						}
						gs.enqueueModal(p, "Auction Results", msg)
					}
				}

				detail := "no other bids"
				if secondBid > 0 {
					detail = fmt.Sprintf("next highest: %s at $%d", secondName, secondBid)
				}
				gs.logGeneral(room, fmt.Sprintf("%s won %s on %s for $%d (%s)", winnerPlayer.Name, auction.FacilityType, auction.Planet, highestBid, detail))
			}
		}
	} else {
		// No valid bids
		for _, p := range room.Players {
			gs.enqueueModal(p, "Auction Failed",
				fmt.Sprintf("No valid bids were received for the %s on %s. The facility remains under Federation control.",
					auction.FacilityType, auction.Planet))
		}

		gs.logGeneral(room, fmt.Sprintf("Federation auction failed: no valid bids for %s on %s", auction.FacilityType, auction.Planet))
	}
}

// handleFacilities processes facility usage charges and collection
func (gs *GameServer) handleFacilities(room *Room) {
	chargesByPlayer := make(map[PlayerID]map[string]int)

	for planetName, planet := range room.Planets {
		if planet == nil || len(planet.Facilities) == 0 {
			continue
		}

		for _, facility := range planet.Facilities {
			if facility == nil {
				continue
			}

			// Charge all players at this location who don't own the facility
			for _, p := range room.Players {
				if p.Bankrupt || p.InTransit {
					continue
				}

				if p.CurrentPlanet == planetName && p.ID != facility.Owner {
					charge := facility.UsageCharge
					p.Money -= charge
					facility.AccruedMoney += charge

					gs.logAction(room, p, fmt.Sprintf("Facility charge: $%d at %s (%s)", charge, planetName, facility.Type))
					if !p.IsBot {
						if _, ok := chargesByPlayer[p.ID]; !ok {
							chargesByPlayer[p.ID] = make(map[string]int)
						}
						chargesByPlayer[p.ID][planetName] += charge
					}

					if p.Money < -500 && !p.Bankrupt {
						if !p.IsBot {
							gs.enqueueModal(p, "Game Over", "Your ship was impounded for unpaid facility fees. You may continue watching.")
						}
						p.Bankrupt = true
						room.News = append(room.News, NewsItem{
							Headline:       p.Name + " bankrupted by facility fees at " + planetName,
							Planet:         planetName,
							TurnsRemaining: 3,
						})
					}
				}

				if p.CurrentPlanet == planetName && p.ID == facility.Owner && facility.AccruedMoney > 0 {
					collected := facility.AccruedMoney
					p.Money += collected
					facility.AccruedMoney = 0

					gs.logAction(room, p, fmt.Sprintf("Facility revenue collected: $%d from %s", collected, facility.Type))
					if !p.IsBot {
						gs.enqueueModal(p, "Facility Revenue",
							fmt.Sprintf("You collected %d credits in revenue from your %s on %s.",
								collected, facility.Type, planetName))
					}
				}

				for playerID, planetCharges := range chargesByPlayer {
					player := room.Players[playerID]
					if player == nil || player.IsBot {
						continue
					}
					for planetName, total := range planetCharges {
						if total <= 0 {
							continue
						}
						if len(player.Modals) > 0 {
							filtered := player.Modals[:0]
							for _, modal := range player.Modals {
								if modal.Title == "Facility Usage Fee" && strings.Contains(modal.Body, planetName) {
									continue
								}
								filtered = append(filtered, modal)
							}
							player.Modals = append([]ModalItem(nil), filtered...)
						}
						gs.enqueueModal(player, "Facility Usage Fee",
							fmt.Sprintf("The facilities at %s have charged you $%d for your visit.", planetName, total))
					}
				}
			}
		}
	}
}

// logGeneral adds a general room-wide log entry (could be used for system messages)
func (gs *GameServer) logGeneral(room *Room, text string) {
	// For now, just add it as news
	room.News = append(room.News, NewsItem{
		Headline:       text,
		Planet:         "",
		TurnsRemaining: 2,
	})
}
