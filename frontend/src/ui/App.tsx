import { useEffect, useMemo, useRef, useState } from 'react'

// Simple client that manages ws and state machine: title -> lobby -> room -> game

type LobbyRoom = { id: string; name: string; playerCount: number; started: boolean; turn?: number }

type RoomPlayer = { id: string; name: string; money: number; currentPlanet: string; destinationPlanet: string; ready?: boolean }
type RoomState = {
  room: { id: string; name: string; started: boolean; turn: number; players: RoomPlayer[]; planets: string[]; allReady?: boolean }
  you: { id: string; name: string; money: number; inventory: Record<string, number>; inventoryAvgCost: Record<string, number>; currentPlanet: string; destinationPlanet: string; ready?: boolean }
  visiblePlanet: { name: string; goods: Record<string, number>; prices: Record<string, number> } | {}
}

type LobbyState = { rooms: LobbyRoom[] }

type WSOut = { type: string; payload?: any }

function useWS(url: string | null) {
  const wsRef = useRef<WebSocket | null>(null)
  const [ready, setReady] = useState(false)
  const [messages, setMessages] = useState<WSOut[]>([])

  useEffect(() => {
    if (!url) return
    const ws = new WebSocket(url)
    wsRef.current = ws
    ws.onopen = () => setReady(true)
    ws.onclose = () => { setReady(false) }
    ws.onmessage = (ev) => {
      try { setMessages(m => [...m, JSON.parse(ev.data)]) } catch {}
    }
    return () => { ws.close(); wsRef.current = null }
  }, [url])

  const send = useMemo(() => (type: string, payload?: any) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return
    wsRef.current.send(JSON.stringify({ type, payload }))
  }, [])

  return { ready, messages, send }
}

export function App() {
  const [stage, setStage] = useState<'title'|'lobby'|'room'>('title')
  const [name, setName] = useState('')
  const [url, setUrl] = useState<string | null>(null)
  const { ready, messages, send } = useWS(url)

  const [lobby, setLobby] = useState<LobbyState>({ rooms: [] })
  const [room, setRoom] = useState<RoomState | null>(null)
  const [amountsByGood, setAmountsByGood] = useState<Record<string, number>>({})
  const planetsContainerRef = useRef<HTMLDivElement | null>(null)
  const planetRefs = useRef<Record<string, HTMLLIElement | null>>({})
  const [planetPos, setPlanetPos] = useState<Record<string, { x: number; y: number }>>({})
  const [containerSize, setContainerSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 })
  const [playersOpen, setPlayersOpen] = useState(false)

  useEffect(() => {
    const last = messages[messages.length-1]
    if (!last) return
    if (last.type === 'lobbyState') {
      setLobby(last.payload)
      setStage('lobby')
    }
    if (last.type === 'roomState') {
      setRoom(last.payload)
      setStage('room')
    }
  }, [messages])

  // Actions
  const onConnect = () => {
  const host = typeof window !== 'undefined' ? window.location.hostname : 'localhost'
  setUrl(`ws://${host}:8080/ws`)
  }
  useEffect(() => { if (ready) send('connect', { name: name || undefined }) }, [ready])

  // While in the lobby, periodically refresh the room list so new rooms show up
  useEffect(() => {
    if (!ready || stage !== 'lobby') return
    send('listRooms')
    const t = setInterval(() => send('listRooms'), 3000)
    return () => clearInterval(t)
  }, [ready, stage])

  const createRoom = () => send('createRoom')
  const joinRoom = (roomId: string) => send('joinRoom', { roomId })
  const startGame = () => send('startGame')
  const addBot = () => send('addBot')
  const exitRoom = () => send('exitRoom')

  const selectPlanet = (planet: string) => send('selectPlanet', { planet })
  const buy = (good: string, amount: number) => send('buy', { good, amount })
  const sell = (good: string, amount: number) => send('sell', { good, amount })

  // Track positions of planet list items for drawing arrows
  useEffect(() => {
    if (!room) return
    const container = planetsContainerRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()
    setContainerSize({ width: rect.width, height: rect.height })
    const next: Record<string, { x: number; y: number }> = {}
    for (const p of room.room.planets) {
      const el = planetRefs.current[p]
      if (!el) continue
      const r = el.getBoundingClientRect()
      next[p] = { x: Math.min(rect.width - 60, 180), y: r.top + r.height / 2 - rect.top }
    }
    setPlanetPos(next)
  }, [room?.room.planets, room?.room.players, stage])

  // Recompute on resize
  useEffect(() => {
    const onResize = () => {
      if (!room) return
      const container = planetsContainerRef.current
      if (!container) return
      const rect = container.getBoundingClientRect()
      setContainerSize({ width: rect.width, height: rect.height })
      const next: Record<string, { x: number; y: number }> = {}
      for (const p of room.room.planets) {
        const el = planetRefs.current[p]
        if (!el) continue
        const r = el.getBoundingClientRect()
        next[p] = { x: Math.min(rect.width - 60, 180), y: r.top + r.height / 2 - rect.top }
      }
      setPlanetPos(next)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [room?.room.planets, stage])

  const colorFor = (id: string) => {
    let h = 0
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360
    return `hsl(${h},70%,45%)`
  }

  // UI
  if (stage === 'title') {
    return (
      <div style={{ padding: 24, fontFamily: 'system-ui' }}>
        <h1>Space Trader</h1>
        <input placeholder="Your name" value={name} onChange={e=>setName(e.target.value)} />
        <button onClick={onConnect} style={{ marginLeft: 8 }}>Connect</button>
      </div>
    )
  }

  if (stage === 'lobby') {
    return (
      <div style={{ padding: 24, fontFamily: 'system-ui' }}>
        <h2>Lobby</h2>
        <button onClick={createRoom}>Create Game</button>
        <h3 style={{ marginTop: 16 }}>Active Rooms</h3>
        <ul>
      {lobby.rooms.map(r => (
            <li key={r.id}>
              <button onClick={() => joinRoom(r.id)}>
                {r.name} — {r.playerCount} players {r.started ? `(Started · Turn ${r.turn ?? 0})` : ''}
              </button>
            </li>
          ))}
        </ul>
      </div>
    )
  }

  const r = room!
  const visible = (r.visiblePlanet || {}) as any
  const goods: Record<string, number> = visible.goods || {}
  const prices: Record<string, number> = visible.prices || {}

  return (
    <div style={{ fontFamily: 'system-ui' }}>
      <div style={{ display:'flex', alignItems:'center', gap:12, justifyContent:'space-between', padding:'10px 16px', borderBottom:'1px solid #e5e7eb' }}>
  <div style={{ display:'flex', gap:12, alignItems:'center', position:'relative' }}>
          <strong>{r.room.name}</strong>
          <span style={{ color:'#666' }}>Turn: {r.room.turn}</span>
          <div
            onMouseEnter={() => setPlayersOpen(true)}
            onMouseLeave={() => setPlayersOpen(false)}
            style={{ position:'relative' }}
          >
            <button onClick={() => setPlayersOpen(v=>!v)}>Players ▾</button>
            {playersOpen && (
              <div style={{ position:'absolute', top:'100%', left:0, marginTop:6, background:'#fff', border:'1px solid #e5e7eb', borderRadius:8, boxShadow:'0 8px 24px rgba(0,0,0,0.12)', padding:8, zIndex:1000, minWidth:280 }}>
                <ul style={{ listStyle:'none', padding:0, margin:0 }}>
                  {r.room.players.map((pl)=> (
                    <li key={pl.id} style={{ display:'flex', alignItems:'center', gap:8, fontSize:12, lineHeight:1.2, padding:'6px 8px', borderRadius:6 }}>
                      <span title={pl.ready ? 'Ready' : 'Not Ready'} style={{ width:8, height:8, borderRadius:4, background: pl.ready ? '#10b981' : '#ef4444' }} />
                      <span style={{ width:10, height:10, borderRadius:5, background: colorFor(String(pl.id)), boxShadow:'0 0 0 1px rgba(0,0,0,0.15)' }} />
                      <span style={{ flex:1, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{pl.name}</span>
                      <span style={{ color:'#111' }}>${pl.money}</span>
                      <span style={{ color:'#666' }}>@ {pl.currentPlanet}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </div>
        <div style={{ display:'flex', gap:12, alignItems:'center' }}>
          <button
            onClick={() => send('setReady', { ready: !Boolean(r.you.ready) })}
            style={{ padding:'4px 10px', borderRadius:6, border:'1px solid #e5e7eb', background: r.you.ready ? '#10b98122' : '#ef444422', color: r.you.ready ? '#065f46' : '#7f1d1d' }}
            title={r.you.ready ? 'Ready' : 'Not Ready'}
          >
            Ready
          </button>
          <span><strong>${r.you.money}</strong></span>
          {!r.room.started && (
            <>
              <button onClick={startGame} disabled={!r.room.allReady} title={r.room.allReady ? 'All players are ready' : 'Waiting for all players to be ready'}>Start Game</button>
              <button onClick={addBot}>Add Bot</button>
              <button onClick={exitRoom}>Exit</button>
            </>
          )}
          {r.room.started && (
            <button onClick={exitRoom}>Exit</button>
          )}
        </div>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px 240px', gap: 16, padding: 16 }}>
      {/* Planets column (first, wide to host arrows) */}
      <div ref={planetsContainerRef} style={{ position: 'relative' }}>
        <h3>Planets</h3>
        <ul>
          {r.room.planets.map(p => {
            const onPlanet = (r.room.players as any[]).filter(pl => pl.currentPlanet === p)
            return (
              <li key={p} ref={el => (planetRefs.current[p] = el)} style={{ display:'flex', alignItems:'center', gap:8 }}>
                <button disabled={p===r.you.currentPlanet} onClick={()=>selectPlanet(p)}>{p}</button>
                <div style={{ display:'flex', gap:4 }}>
                  {onPlanet.map((pl:any) => (
                    <span
                      key={pl.id}
                      title={pl.name}
                      style={{
                        width:14,
                        height:14,
                        borderRadius:7,
                        background: colorFor(String(pl.id)),
                        color:'#fff',
                        display:'inline-flex',
                        alignItems:'center',
                        justifyContent:'center',
                        fontSize:10,
                        boxShadow:'0 0 0 1px rgba(0,0,0,0.15)'
                      }}
                    >
                      {String(pl.name||'P').slice(0,1).toUpperCase()}
                    </span>
                  ))}
                </div>
              </li>
            )
          })}
        </ul>
        {/* Destination arrows overlay */}
        <svg width={containerSize.width} height={containerSize.height} style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
          <defs>
            {(r.room.players as any[]).map(pl => (
              <marker key={pl.id} id={`arrow-head-${pl.id}`} markerWidth="10" markerHeight="10" refX="10" refY="5" orient="auto">
                <path d="M0,0 L10,5 L0,10 z" fill={colorFor(String(pl.id))} />
              </marker>
            ))}
          </defs>
          {(r.room.players as any[]).map(pl => {
            const from = planetPos[pl.currentPlanet]
            const to = pl.destinationPlanet ? planetPos[pl.destinationPlanet] : undefined
            if (!from || !to) return null
            if (pl.destinationPlanet === pl.currentPlanet) return null
            const x1 = from.x, y1 = from.y
            const x2 = to.x, y2 = to.y
            const dx = Math.max(80, Math.abs(y2 - y1) * 0.3 + 80)
            const c1x = x1 + dx, c1y = y1
            const c2x = x2 + dx, c2y = y2
            const d = `M ${x1},${y1} C ${c1x},${c1y} ${c2x},${c2y} ${x2},${y2}`
            return (
              <path key={pl.id}
                d={d}
                fill="none"
                stroke={colorFor(String(pl.id))}
                strokeWidth={2}
                markerEnd={`url(#arrow-head-${pl.id})`}
                opacity={0.9}
              />
            )
          })}
        </svg>
  </div>
  <div>
    <h3>Market — {visible.name || r.you.currentPlanet}</h3>
        <ul>
          {Object.keys(goods).map(g => {
            const price = prices[g]
            const available = goods[g]
            const owned = r.you.inventory[g] || 0
            const youPaid = r.you.inventoryAvgCost?.[g]
            const maxBuy = price > 0 ? Math.min(available, Math.floor(r.you.money / price)) : 0
            const amt = (amountsByGood[g] ?? maxBuy)
            return (
              <li key={g} style={{ marginBottom: 8, padding: 8, borderRadius: 6, border: owned>0 ? '2px solid #3b82f6' : undefined }}>
                <b>{g}</b>: {available} @ ${price} {owned>0 && youPaid ? <span style={{color:'#666'}}>(you paid ${youPaid})</span> : null}
                <div style={{ display:'flex', gap: 6, alignItems:'center' }}>
                  <input style={{ width: 64 }} type="number" value={amt} min={0} max={999}
                    onChange={e=>{
                      const v = Number(e.target.value)
                      setAmountsByGood(s => ({ ...s, [g]: isNaN(v) ? 0 : v }))
                    }} />
                  <button disabled={amt<=0} onClick={()=>buy(g, amt)}>Buy</button>
                  <span>Owned: {owned}</span>
                  <button disabled={owned<=0} onClick={()=>sell(g, owned)}>Sell</button>
                </div>
              </li>
            )
          })}
        </ul>
      </div>
      <div>
        <h3>Ship Inventory</h3>
        {Object.keys(r.you.inventory).length === 0 ? (
          <div>Empty</div>
        ) : (
          <ul>
            {Object.keys(r.you.inventory).sort().map(g => {
              const qty = r.you.inventory[g]
              const avg = r.you.inventoryAvgCost?.[g]
              return (
                <li key={g}>
                  {g}: {qty}{typeof avg === 'number' ? ` (avg $${avg})` : ''}
                </li>
              )
            })}
          </ul>
        )}
      </div>
      </div>
    </div>
  )
}
