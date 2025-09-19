import { useEffect, useMemo, useRef, useState } from 'react'

// Simple client that manages ws and state machine: title -> lobby -> room -> game

type LobbyRoom = { id: string; name: string; playerCount: number; started: boolean; turn?: number }

type RoomPlayer = { id: string; name: string; money: number; currentPlanet: string; destinationPlanet: string; ready?: boolean }
type RoomState = {
  room: { id: string; name: string; started: boolean; turn: number; players: RoomPlayer[]; planets: string[]; planetPositions?: Record<string, { x: number; y: number }>; allReady?: boolean; turnEndsAt?: number; news?: { headline: string; planet: string; turnsRemaining: number }[] }
  you: { id: string; name: string; money: number; fuel: number; inventory: Record<string, number>; inventoryAvgCost: Record<string, number>; currentPlanet: string; destinationPlanet: string; ready?: boolean; modal?: { id: string; title: string; body: string }; inTransit?: boolean; transitFrom?: string; transitRemaining?: number; transitTotal?: number }
  visiblePlanet: { name: string; goods: Record<string, number>; prices: Record<string, number>; priceRanges?: Record<string, [number, number]>; fuelPrice?: number } | {}
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

function NewsTicker({ items }: { items: string[] }) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const contentRef = useRef<HTMLSpanElement | null>(null)
  const [repeat, setRepeat] = useState(1)
  const [anim, setAnim] = useState<{ name: string; duration: number } | null>(null)
  const sep = '   •   '
  // Measure and configure
  useEffect(() => {
    const container = containerRef.current
    const content = contentRef.current
    if (!container || !content) return
    const containerWidth = container.getBoundingClientRect().width
    const baseWidth = content.scrollWidth || content.getBoundingClientRect().width
    // Ensure total track width >= container + one full segment, so as we animate by one segment there is no gap
    const neededRepeats = Math.max(1, Math.ceil((containerWidth + baseWidth) / Math.max(baseWidth, 1)) - 1)
    setRepeat(neededRepeats)
    const distance = Math.max(1, baseWidth)
    const pxPerSec = 80 // speed
    const duration = distance / pxPerSec
    const name = `newsTickerMove_${Math.random().toString(36).slice(2)}`
    // Create dynamic keyframes
    const styleEl = document.createElement('style')
    styleEl.dataset.ticker = name
    styleEl.textContent = `@keyframes ${name} { from { transform: translateX(0); } to { transform: translateX(-${distance}px); } }`
    document.head.appendChild(styleEl)
    setAnim({ name, duration })
    return () => {
      const el = document.head.querySelector(`style[data-ticker="${name}"]`)
      if (el) el.remove()
    }
  }, [items.join('|')])
  // Recompute on resize
  useEffect(() => {
    const onResize = () => {
      if (!containerRef.current || !contentRef.current) return
      const containerWidth = containerRef.current.getBoundingClientRect().width
      const baseWidth = contentRef.current.scrollWidth || contentRef.current.getBoundingClientRect().width
      const neededRepeats = Math.max(1, Math.ceil((containerWidth + baseWidth) / Math.max(baseWidth, 1)) - 1)
      setRepeat(neededRepeats)
      const distance = Math.max(1, baseWidth)
      const pxPerSec = 80
      const duration = distance / pxPerSec
      const name = `newsTickerMove_${Math.random().toString(36).slice(2)}`
      const styleEl = document.createElement('style')
      styleEl.dataset.ticker = name
      styleEl.textContent = `@keyframes ${name} { from { transform: translateX(0); } to { transform: translateX(-${distance}px); } }`
      document.head.appendChild(styleEl)
      setAnim({ name, duration })
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])
  const text = items && items.length > 0 ? items.join(sep) : 'No news yet.'
  return (
    <div style={{ background:'#e0f2fe', color:'#075985', borderTop:'1px solid #93c5fd', borderBottom:'1px solid #93c5fd', padding:'6px 0' }}>
      <div ref={containerRef} style={{ position:'relative', overflow:'hidden' }}>
        <div style={{ display:'inline-flex', whiteSpace:'nowrap', willChange:'transform', animation: anim ? `${anim.name} ${anim.duration}s linear infinite` : undefined }}>
          <span ref={contentRef} style={{ paddingRight: 24 }}>{text}</span>
          {/* Duplicate segments to ensure seamless scroll (at least one duplicate) */}
          {Array.from({ length: Math.max(1, repeat) }).map((_, i) => (
            <span key={i} style={{ paddingRight: 24 }}>{text}</span>
          ))}
        </div>
      </div>
    </div>
  )
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
  const [now, setNow] = useState<number>(() => Date.now())

  // Tick local time for countdown
  useEffect(() => {
    const i = setInterval(() => setNow(Date.now()), 250)
    return () => clearInterval(i)
  }, [])

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
  const ackModal = (id?: string) => send('ackModal', { id })
  const refuel = (amount?: number) => send('refuel', { amount: amount ?? 0 })

  // Compute planet center positions from server data, with a stable fallback
  useEffect(() => {
    if (!room) return
    const container = planetsContainerRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()
    setContainerSize({ width: rect.width, height: rect.height })
    const next: Record<string, { x: number; y: number }> = {}
    const serverPos = (room.room as any).planetPositions as Record<string, { x: number; y: number }> | undefined
    // Fallback: place planets around a circle with small deterministic jitter
    const names = room.room.planets
    const N = Math.max(1, names.length)
    const fallback: Record<string, { x: number; y: number }> = {}
    names.forEach((name, i) => {
      const angle = (i / N) * Math.PI * 2
      let h = 0
      for (let k = 0; k < name.length; k++) h = (h * 31 + name.charCodeAt(k)) >>> 0
      const jitter = ((h % 1000) / 1000 - 0.5) * 0.08 // +-0.04
      const radius = 0.42 + (((h >> 4) % 1000) / 1000 - 0.5) * 0.06
      const x = 0.5 + (radius + jitter) * Math.cos(angle)
      const y = 0.5 + (radius - jitter) * Math.sin(angle)
      // clamp away from edges
      const cx = Math.min(0.92, Math.max(0.08, x)) * rect.width
      const cy = Math.min(0.92, Math.max(0.08, y)) * rect.height
      fallback[name] = { x: cx, y: cy }
    })
    for (const p of names) {
      const pos = serverPos?.[p]
      if (pos) {
        next[p] = { x: pos.x * rect.width, y: pos.y * rect.height }
      } else {
        next[p] = fallback[p]
      }
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
      const positions = (room.room as any).planetPositions as Record<string, { x: number; y: number }> | undefined
      const next: Record<string, { x: number; y: number }> = {}
      const names = room.room.planets
      const N = Math.max(1, names.length)
      const fallback: Record<string, { x: number; y: number }> = {}
      names.forEach((name, i) => {
        const angle = (i / N) * Math.PI * 2
        let h = 0
        for (let k = 0; k < name.length; k++) h = (h * 31 + name.charCodeAt(k)) >>> 0
        const jitter = ((h % 1000) / 1000 - 0.5) * 0.08
        const radius = 0.42 + (((h >> 4) % 1000) / 1000 - 0.5) * 0.06
        const x = 0.5 + (radius + jitter) * Math.cos(angle)
        const y = 0.5 + (radius - jitter) * Math.sin(angle)
        fallback[name] = { x: Math.min(0.92, Math.max(0.08, x)) * rect.width, y: Math.min(0.92, Math.max(0.08, y)) * rect.height }
      })
      for (const p of names) {
        const pos = positions?.[p]
        if (pos) next[p] = { x: pos.x * rect.width, y: pos.y * rect.height }
        else next[p] = fallback[p]
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
  const priceRanges: Record<string, [number, number]> = (visible.priceRanges as any) || {}
  const fuelPrice: number = typeof visible.fuelPrice === 'number' ? visible.fuelPrice : 10
  const capacity = 200
  const usedSlots = Object.values(r.you.inventory || {}).reduce((a, b) => a + (b || 0), 0)
  const freeSlots = Math.max(0, capacity - usedSlots)
  // Compute map distance between current and selected destination using normalized positions
  const getNormPos = (name?: string): { x: number; y: number } | undefined => {
    if (!name) return undefined
    const serverPos = (r.room as any).planetPositions?.[name] as { x: number; y: number } | undefined
    if (serverPos) return serverPos
    const px = planetPos[name]
    if (px && containerSize.width > 0 && containerSize.height > 0) {
      return { x: px.x / containerSize.width, y: px.y / containerSize.height }
    }
    return undefined
  }
  const destName = r.you.destinationPlanet
  const inTransit = Boolean((r.you as any).inTransit)
  let mapTitle = 'Map'
  if (destName && destName !== r.you.currentPlanet) {
    const a = getNormPos(r.you.currentPlanet)
    const b = getNormPos(destName)
    if (a && b) {
      const dx = a.x - b.x
      const dy = a.y - b.y
      const d = Math.sqrt(dx*dx + dy*dy)
      const units = Math.max(1, Math.ceil(d * 40)) // match server scaling
      mapTitle = `Map — ${units} units`
    }
  }
  // Helper to compute fuel cost between two planets (server-aligned scaling)
  const travelUnits = (from?: string, to?: string) => {
    if (!from || !to || from === to) return 0
    const a = getNormPos(from)
    const b = getNormPos(to)
    if (!a || !b) return 0
    const dx = a.x - b.x
    const dy = a.y - b.y
    const d = Math.sqrt(dx*dx + dy*dy)
    return Math.max(1, Math.ceil(d * 40))
  }
  // Interpolate your ship position if in transit
  const yourTransitPos = (() => {
    if (!inTransit) return undefined as undefined | { x: number; y: number }
    const from = (r.you as any).transitFrom || r.you.currentPlanet
    const to = r.you.destinationPlanet
    const rem: number = (r.you as any).transitRemaining ?? 0
    const total: number = (r.you as any).transitTotal ?? 0
    if (!from || !to || total <= 0) return undefined
    const a = getNormPos(from)
    const b = getNormPos(to)
    if (!a || !b) return undefined
    const progressed = Math.max(0, Math.min(1, (total - rem) / total))
    const x = (a.x + (b.x - a.x) * progressed) * containerSize.width
    const y = (a.y + (b.y - a.y) * progressed) * containerSize.height
    return { x, y }
  })()

  return (
    <div style={{ fontFamily: 'system-ui' }}>
  {/* News ticker below header (blue-hued) */}
      {r.you.modal && r.you.modal.id && (
        <div style={{ position:'fixed', inset:0, background:'rgba(0,0,0,0.45)', display:'flex', alignItems:'center', justifyContent:'center', zIndex:2000 }}>
          <div style={{ background:'#fff', padding:16, borderRadius:8, width:360, boxShadow:'0 10px 30px rgba(0,0,0,0.2)' }}>
            <div style={{ fontWeight:700, marginBottom:8 }}>{r.you.modal.title}</div>
            <div style={{ whiteSpace:'pre-wrap', marginBottom:12 }}>{r.you.modal.body}</div>
            <div style={{ display:'flex', justifyContent:'flex-end' }}>
              <button onClick={()=>ackModal(r.you.modal?.id)}>OK</button>
            </div>
          </div>
        </div>
      )}
      <div style={{ display:'flex', alignItems:'center', gap:12, justifyContent:'space-between', padding:'10px 16px', borderBottom:'1px solid #e5e7eb' }}>
  <div style={{ display:'flex', gap:12, alignItems:'center', position:'relative' }}>
          <strong>{r.room.name}</strong>
          <span style={{ color:'#666' }}>Turn: {r.room.turn}</span>
          {typeof r.room.turnEndsAt === 'number' && (
            <span style={{ color:'#666' }}>
              · {Math.max(0, Math.ceil((r.room.turnEndsAt - now) / 1000))}s
            </span>
          )}
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
          <div title="Ship fuel (price varies by planet)">
            <span style={{ marginLeft: 8 }}>Fuel: <strong>{r.you.fuel}</strong>/100</span>
            <span style={{ marginLeft: 8, color:'#666' }}>@ ${ fuelPrice }/unit</span>
            <button onClick={() => refuel(0)} style={{ marginLeft: 6 }} disabled={inTransit || (r.you.fuel ?? 0) >= 100 || (r.you.money ?? 0) < fuelPrice} title={inTransit ? 'Unavailable while in transit' : ((r.you.fuel ?? 0) >= 100) ? 'Tank full' : ((r.you.money ?? 0) < fuelPrice ? 'Not enough credits' : 'Fill to max')}>Fill</button>
          </div>
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
  <NewsTicker items={(r.room.news && r.room.news.length>0) ? r.room.news.map(n=>n.headline) : []} />
  <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px 240px', gap: 16, padding: 16 }}>
      {/* Map column (first) */}
      <div>
        <h3>{mapTitle}</h3>
        <div ref={planetsContainerRef} style={{ position:'relative', height: 380 }}>
        <ul style={{ listStyle:'none', padding:0, margin:0, position:'absolute', inset:0 }}>
          {r.room.planets.map(p => {
            const onPlanet = (r.room.players as any[]).filter(pl => pl.currentPlanet === p)
            const center = planetPos[p]
            const left = center ? center.x : 0
            const top = center ? center.y : 0
            const need = travelUnits(r.you.currentPlanet, p)
            const canReach = !inTransit && (p === r.you.currentPlanet || need <= (r.you.fuel ?? 0))
            return (
              <li key={p} ref={el => (planetRefs.current[p] = el)} style={{ position:'absolute', left, top, transform:'translate(-50%, -50%)', display:'flex', alignItems:'center', gap:8, padding:8, border:'1px solid #e5e7eb', borderRadius:8, background:'#fff' }}>
                <button
                  disabled={p===r.you.currentPlanet || !canReach}
                  onClick={()=>selectPlanet(p)}
                  style={{ textAlign:'left' }}
                  title={inTransit ? 'Unavailable while in transit' : (p===r.you.currentPlanet ? 'You are here' : (!canReach ? `Need ${need} units (have ${r.you.fuel ?? 0})` : undefined))}
                >
                  {p}
                </button>
                <div style={{ display:'flex', gap:4 }}>
                  {onPlanet.filter((pl:any)=> !(pl.id===r.you.id && inTransit)).map((pl:any) => (
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
            const d = `M ${x1},${y1} L ${x2},${y2}`
            return (
              <path key={pl.id}
                d={d}
                fill="none"
                stroke={colorFor(String(pl.id))}
                strokeWidth={2}
                strokeLinecap="round"
                markerEnd={`url(#arrow-head-${pl.id})`}
                opacity={0.95}
              />
            )
          })}
          {/* Your in-transit position marker */}
          {inTransit && yourTransitPos && (
            <circle cx={yourTransitPos.x} cy={yourTransitPos.y} r={7} fill={colorFor(String(r.you.id))} stroke="#111" strokeOpacity={0.15} />
          )}
        </svg>
        </div>
      </div>
  <div>
  <h3>Market — {visible.name || r.you.currentPlanet}</h3>
    <ul style={{ listStyle:'none', padding: 0, margin: 0 }}>
          {Object.keys(goods).map(g => {
            const price = prices[g]
            const range = priceRanges[g]
            const available = goods[g]
            const owned = r.you.inventory[g] || 0
            const youPaid = r.you.inventoryAvgCost?.[g]
            const maxByMoney = price > 0 ? Math.floor(r.you.money / price) : 0
            const maxBuy = price > 0 ? Math.max(0, Math.min(available, maxByMoney, freeSlots)) : 0
            const amt = Math.max(0, Math.min(maxBuy, (amountsByGood[g] ?? maxBuy)))
            const sellStyle: React.CSSProperties | undefined = typeof youPaid === 'number' && owned > 0
              ? (price > youPaid
                  ? { background:'#10b98122', color:'#065f46', border:'1px solid #10b98155' }
                  : price < youPaid
                    ? { background:'#ef444422', color:'#7f1d1d', border:'1px solid #ef444455' }
                    : { background:'#f3f4f6', color:'#111', border:'1px solid #e5e7eb' })
              : undefined
            const disabledTrade = inTransit
            return (
              <li key={g} style={{ marginBottom: 8, padding: 8, borderRadius: 6, border: owned>0 ? '2px solid #3b82f6' : undefined }}>
                <b>{g}</b>: {available} @ ${price} {range ? <span style={{ color:'#666' }}> (${range[0]}–${range[1]})</span> : null} {owned>0 && youPaid ? <span style={{color:'#666'}}>(you paid ${youPaid})</span> : null}
                <div style={{ display:'flex', gap: 6, alignItems:'center' }}>
          <input style={{ width: 64 }} type="number" value={amt} min={0} max={maxBuy} disabled={disabledTrade}
                    onChange={e=>{
                      const v = Number(e.target.value)
            const capped = Math.max(0, Math.min(maxBuy, isNaN(v) ? 0 : v))
            setAmountsByGood(s => ({ ...s, [g]: capped }))
                    }} />
          <button disabled={disabledTrade || amt<=0} onClick={()=>buy(g, amt)} title={disabledTrade ? 'Unavailable while in transit' : (freeSlots<=0 ? 'Cargo full' : undefined)}>Buy</button>
                  <span>Owned: {owned}</span>
                  <button disabled={disabledTrade || owned<=0} onClick={()=>sell(g, owned)} style={sellStyle} title={disabledTrade ? 'Unavailable while in transit' : undefined}>Sell</button>
                </div>
              </li>
            )
          })}
        </ul>
      </div>
      <div>
        <h3>Ship Inventory <span title="Ship capacity" style={{ color:'#666', fontWeight: 500, marginLeft: 6 }}>{usedSlots}/{capacity}</span></h3>
        {Object.keys(r.you.inventory).length === 0 ? (
          <div>Empty</div>
        ) : (
          <ul style={{ listStyle:'none', padding: 0, margin: 0 }}>
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
