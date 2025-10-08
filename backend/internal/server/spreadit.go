package server

import (
	"fmt"
	"math/rand"
	"time"
)

type spreadSources map[PlayerID]map[int]struct{}

type SpreaditTile struct {
	Owner           PlayerID
	CoreOf          PlayerID
	HasResource     bool
	ResourceSpawner bool
	Wall            bool
}

type SpreaditPlayerState struct {
	Color     string
	CoreIndex int
	Resources int
}

type SpreaditState struct {
	Rows        int
	Cols        int
	CreatedAt   time.Time
	StartedAt   time.Time
	Tick        int
	Tiles       []SpreaditTile
	Players     map[PlayerID]*SpreaditPlayerState
	LastSources []spreadSources
}

const (
	spreaditDefaultRows      = 12
	spreaditDefaultCols      = 20
	spreaditResourceDensity  = 12
	spreaditTickerInterval   = time.Second
	spreaditResourceInterval = 10
	spreaditWallChance       = 0.14
	spreaditWallDestroyCost  = 20
)

func newSpreaditState(rows, cols int) *SpreaditState {
	if rows <= 0 {
		rows = spreaditDefaultRows
	}
	if cols <= 0 {
		cols = spreaditDefaultCols
	}
	total := rows * cols
	tiles := make([]SpreaditTile, total)
	last := make([]spreadSources, total)

	state := &SpreaditState{
		Rows:        rows,
		Cols:        cols,
		CreatedAt:   time.Now(),
		Tiles:       tiles,
		Players:     make(map[PlayerID]*SpreaditPlayerState),
		LastSources: last,
	}

	resourceTargets := spreaditResourceDensity
	if resourceTargets <= 0 {
		resourceTargets = 1
	}
	used := map[int]struct{}{}
	attempts := 0
	for len(used) < resourceTargets && attempts < total*2 {
		idx := rand.Intn(total)
		if _, ok := used[idx]; ok {
			attempts++
			continue
		}
		tiles[idx].ResourceSpawner = true
		used[idx] = struct{}{}
		attempts++
	}

	for idx := range tiles {
		if tiles[idx].ResourceSpawner {
			continue
		}
		if rand.Float64() < spreaditWallChance {
			tiles[idx].Wall = true
		}
	}

	return state
}

func (s *SpreaditState) index(row, col int) int {
	return row*s.Cols + col
}

func (s *SpreaditState) coord(idx int) (int, int) {
	if idx < 0 {
		return 0, 0
	}
	row := idx / s.Cols
	col := idx % s.Cols
	return row, col
}

func (s *SpreaditState) neighbors(idx int) []int {
	if idx < 0 {
		return nil
	}
	row, col := s.coord(idx)
	result := make([]int, 0, 8)
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			if dr == 0 && dc == 0 {
				continue
			}
			nr := row + dr
			nc := col + dc
			if nr < 0 || nr >= s.Rows || nc < 0 || nc >= s.Cols {
				continue
			}
			result = append(result, s.index(nr, nc))
		}
	}
	return result
}

func (s *SpreaditState) stepTowards(fromIdx, targetIdx int) int {
	if fromIdx < 0 || targetIdx < 0 || fromIdx == targetIdx {
		return fromIdx
	}
	fr, fc := s.coord(fromIdx)
	tr, tc := s.coord(targetIdx)
	dr := 0
	if fr < tr {
		dr = 1
	} else if fr > tr {
		dr = -1
	}
	dc := 0
	if fc < tc {
		dc = 1
	} else if fc > tc {
		dc = -1
	}
	nr := fr + dr
	nc := fc + dc
	if nr < 0 || nr >= s.Rows || nc < 0 || nc >= s.Cols {
		return fromIdx
	}
	return s.index(nr, nc)
}

func (s *SpreaditState) chebyshevDistance(fromIdx, targetIdx int) int {
	if fromIdx < 0 || targetIdx < 0 {
		return 0
	}
	fr, fc := s.coord(fromIdx)
	tr, tc := s.coord(targetIdx)
	dr := absInt(fr - tr)
	dc := absInt(fc - tc)
	if dr > dc {
		return dr
	}
	return dc
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (s *SpreaditState) randomAvailableCoreTile() (int, bool) {
	preferred := []int{}
	fallback := []int{}
	for idx, tile := range s.Tiles {
		if tile.CoreOf != "" {
			continue
		}
		if tile.Wall {
			continue
		}
		fallback = append(fallback, idx)
		if !tile.ResourceSpawner {
			preferred = append(preferred, idx)
		}
	}
	candidates := preferred
	if len(candidates) == 0 {
		candidates = fallback
	}
	if len(candidates) == 0 {
		return -1, false
	}
	return candidates[rand.Intn(len(candidates))], true
}

func randomSpreaditColor(used map[string]struct{}) string {
	for i := 0; i < 16; i++ {
		r := 70 + rand.Intn(156)
		g := 70 + rand.Intn(156)
		b := 70 + rand.Intn(156)
		color := fmt.Sprintf("#%02X%02X%02X", r, g, b)
		if _, exists := used[color]; !exists {
			return color
		}
	}
	// fallback even if duplicate
	r := 70 + rand.Intn(156)
	g := 70 + rand.Intn(156)
	b := 70 + rand.Intn(156)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func ensureSpreaditPlayerLocked(room *Room, player *Player) {
	if room.Spreadit == nil {
		room.Spreadit = newSpreaditState(spreaditDefaultRows, spreaditDefaultCols)
	}
	spreadit := room.Spreadit
	if spreadit.Players == nil {
		spreadit.Players = make(map[PlayerID]*SpreaditPlayerState)
	}
	if _, exists := spreadit.Players[player.ID]; exists {
		return
	}
	usedColors := make(map[string]struct{})
	for _, ps := range spreadit.Players {
		if ps != nil && ps.Color != "" {
			usedColors[ps.Color] = struct{}{}
		}
	}
	color := randomSpreaditColor(usedColors)
	idx, ok := spreadit.randomAvailableCoreTile()
	if !ok {
		idx = 0
	}
	if idx < 0 || idx >= len(spreadit.Tiles) {
		idx = 0
	}
	tile := &spreadit.Tiles[idx]
	tile.Wall = false
	tile.CoreOf = player.ID
	tile.Owner = player.ID
	tile.HasResource = false
	spreadit.Players[player.ID] = &SpreaditPlayerState{
		Color:     color,
		CoreIndex: idx,
		Resources: 0,
	}
}

func (gs *GameServer) runSpreaditTicker(room *Room) {
	ticker := time.NewTicker(spreaditTickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-room.closeCh:
			return
		case <-ticker.C:
			if gs.advanceSpreadit(room) {
				gs.broadcastRoom(room)
			}
		case <-room.stateCh:
			// allow immediate reactions to pause/resume
		}
	}
}

func (gs *GameServer) advanceSpreadit(room *Room) bool {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.GameType != "spreadit" || !room.Started || room.Paused {
		return false
	}
	if room.Spreadit == nil {
		room.Spreadit = newSpreaditState(spreaditDefaultRows, spreaditDefaultCols)
	}
	spreadit := room.Spreadit
	total := spreadit.Rows * spreadit.Cols
	newSources := make([]spreadSources, total)
	spreadCounts := make([]map[PlayerID]int, total)
	addEffect := func(pid PlayerID, fromIdx, toIdx int) {
		if toIdx < 0 || toIdx >= total {
			return
		}
		if newSources[toIdx] == nil {
			newSources[toIdx] = make(spreadSources)
		}
		sourcesByPlayer := newSources[toIdx][pid]
		if sourcesByPlayer == nil {
			sourcesByPlayer = make(map[int]struct{})
			newSources[toIdx][pid] = sourcesByPlayer
		}
		if _, exists := sourcesByPlayer[fromIdx]; exists {
			return
		}
		sourcesByPlayer[fromIdx] = struct{}{}
		if spreadCounts[toIdx] == nil {
			spreadCounts[toIdx] = make(map[PlayerID]int)
		}
		spreadCounts[toIdx][pid]++
	}

	for pid, ps := range spreadit.Players {
		if ps == nil || ps.CoreIndex < 0 {
			continue
		}
		for _, neighbor := range spreadit.neighbors(ps.CoreIndex) {
			if neighbor < 0 || neighbor >= total {
				continue
			}
			if spreadit.Tiles[neighbor].Wall {
				continue
			}
			addEffect(pid, ps.CoreIndex, neighbor)
		}
	}

	for idx, sourceByPlayer := range spreadit.LastSources {
		if len(sourceByPlayer) == 0 {
			continue
		}
		tile := &spreadit.Tiles[idx]
		if tile.Wall {
			continue
		}
		for pid, origins := range sourceByPlayer {
			if tile.Owner != pid {
				continue
			}
			for origin := range origins {
				for _, neighbor := range spreadit.neighbors(idx) {
					if neighbor == origin {
						continue
					}
					if neighbor < 0 || neighbor >= total {
						continue
					}
					if spreadit.Tiles[neighbor].Wall {
						continue
					}
					addEffect(pid, idx, neighbor)
				}
			}
		}
	}

	for idx := 0; idx < total; idx++ {
		tile := &spreadit.Tiles[idx]
		if tile.Wall {
			tile.Owner = ""
			continue
		}
		counts := spreadCounts[idx]
		var best PlayerID
		bestCount := 0
		secondCount := 0
		for pid, count := range counts {
			if count > bestCount {
				secondCount = bestCount
				bestCount = count
				best = pid
			} else if count > secondCount {
				secondCount = count
			}
		}
		if tile.CoreOf != "" {
			tile.Owner = tile.CoreOf
			continue
		}
		if bestCount > 0 && bestCount > secondCount {
			tile.Owner = best
		} else {
			tile.Owner = ""
		}
	}

	spawnResources := spreadit.Tick == 0 || (spreadit.Tick+1)%spreaditResourceInterval == 0
	if spawnResources {
		for idx := range spreadit.Tiles {
			tile := &spreadit.Tiles[idx]
			if tile.Wall {
				continue
			}
			if tile.ResourceSpawner && !tile.HasResource {
				tile.HasResource = true
			}
		}
	}

	type resourceMove struct {
		from   int
		to     int
		player PlayerID
	}
	moves := make([]resourceMove, 0)
	occupied := make(map[int]bool)
	for idx := range spreadit.Tiles {
		tile := &spreadit.Tiles[idx]
		if tile.Wall {
			tile.HasResource = false
			continue
		}
		if !tile.HasResource {
			continue
		}
		owner := tile.Owner
		if owner == "" {
			continue
		}
		playerState := spreadit.Players[owner]
		if playerState == nil || playerState.CoreIndex < 0 {
			continue
		}
		if idx == playerState.CoreIndex {
			tile.HasResource = false
			playerState.Resources++
			continue
		}
		currentDist := spreadit.chebyshevDistance(idx, playerState.CoreIndex)
		dest := spreadit.stepTowards(idx, playerState.CoreIndex)
		if dest == idx {
			continue
		}
		validateDest := func(candidate int) (bool, bool) {
			if candidate < 0 || candidate >= len(spreadit.Tiles) {
				return false, false
			}
			t := &spreadit.Tiles[candidate]
			if t.Wall {
				return false, false
			}
			isCore := t.CoreOf == owner
			if t.HasResource && !isCore {
				return false, false
			}
			if occupied[candidate] && !isCore {
				return false, false
			}
			return true, isCore
		}
		chosen := -1
		isCoreDestination := false
		if ok, isCore := validateDest(dest); ok {
			chosen = dest
			isCoreDestination = isCore
		} else {
			fr, fc := spreadit.coord(idx)
			tr, tc := spreadit.coord(playerState.CoreIndex)
			desiredDr := 0
			if tr > fr {
				desiredDr = 1
			} else if tr < fr {
				desiredDr = -1
			}
			desiredDc := 0
			if tc > fc {
				desiredDc = 1
			} else if tc < fc {
				desiredDc = -1
			}
			bestDist := currentDist
			bestPenalty := 1 << 30
			bestInline := 1 << 30
			for _, candidate := range spreadit.neighbors(idx) {
				if candidate == idx {
					continue
				}
				if ok, candIsCore := validateDest(candidate); ok {
					dist := spreadit.chebyshevDistance(candidate, playerState.CoreIndex)
					if dist >= currentDist {
						continue
					}
					cr, cc := spreadit.coord(candidate)
					dr := cr - fr
					dc := cc - fc
					penalty := absInt(dr-desiredDr)*2 + absInt(dc-desiredDc)
					inline := absInt(cr-tr) + absInt(cc-tc)
					if dist < bestDist || (dist == bestDist && penalty < bestPenalty) || (dist == bestDist && penalty == bestPenalty && inline < bestInline) {
						bestDist = dist
						bestPenalty = penalty
						bestInline = inline
						chosen = candidate
						isCoreDestination = candIsCore
					}
				}
			}
		}
		if chosen == -1 {
			continue
		}
		moves = append(moves, resourceMove{from: idx, to: chosen, player: owner})
		if !isCoreDestination {
			occupied[chosen] = true
		}
	}

	for _, mv := range moves {
		fromTile := &spreadit.Tiles[mv.from]
		if !fromTile.HasResource {
			continue
		}
		fromTile.HasResource = false
		destTile := &spreadit.Tiles[mv.to]
		destTile.HasResource = true
		if destTile.CoreOf == mv.player {
			destTile.HasResource = false
			if ps := spreadit.Players[mv.player]; ps != nil {
				ps.Resources++
			}
		}
	}

	spreadit.LastSources = newSources
	room.Turn++
	spreadit.Tick = room.Turn
	return true
}

func (gs *GameServer) handleSpreaditDestroyWall(p *Player, row, col int) {
	room := gs.getRoom(p.roomID)
	if room == nil {
		return
	}
	room.mu.Lock()
	if room.GameType != "spreadit" || !room.Started {
		room.mu.Unlock()
		return
	}
	if room.Spreadit == nil {
		room.Spreadit = newSpreaditState(spreaditDefaultRows, spreaditDefaultCols)
	}
	spreadit := room.Spreadit
	if row < 0 || row >= spreadit.Rows || col < 0 || col >= spreadit.Cols {
		room.mu.Unlock()
		return
	}
	idx := spreadit.index(row, col)
	if idx < 0 || idx >= len(spreadit.Tiles) {
		room.mu.Unlock()
		return
	}
	tile := &spreadit.Tiles[idx]
	if !tile.Wall {
		room.mu.Unlock()
		return
	}
	ps := spreadit.Players[p.ID]
	if ps == nil || ps.Resources < spreaditWallDestroyCost {
		room.mu.Unlock()
		return
	}

	ps.Resources -= spreaditWallDestroyCost
	tile.Wall = false
	tile.HasResource = false
	tile.Owner = ""
	if idx >= 0 && idx < len(spreadit.LastSources) {
		spreadit.LastSources[idx] = nil
	}

	room.mu.Unlock()
	gs.broadcastRoom(room)
}
