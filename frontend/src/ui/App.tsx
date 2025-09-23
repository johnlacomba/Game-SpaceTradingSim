import { useEffect, useMemo, useRef, useState } from 'react'

// Mobile detection hook
function useIsMobile() {
  const [isMobile, setIsMobile] = useState(false)
  
  useEffect(() => {
    const checkMobile = () => {
      const width = window.innerWidth
      const userAgent = navigator.userAgent
      const mobileRegex = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i
      const isMobileDevice = width <= 768 || mobileRegex.test(userAgent)
      setIsMobile(isMobileDevice)
    }
    
    checkMobile()
    window.addEventListener('resize', checkMobile)
    return () => window.removeEventListener('resize', checkMobile)
  }, [])
  
  return isMobile
}

// Simple client that manages ws and state machine: title -> lobby -> room -> game

type LobbyRoom = { id: string; name: string; playerCount: number; started: boolean; turn?: number }

type RoomPlayer = { id: string; name: string; money: number; currentPlanet: string; destinationPlanet: string; ready?: boolean; endGame?: boolean }
type RoomState = {
  room: { id: string; name: string; started: boolean; turn: number; players: RoomPlayer[]; planets: string[]; planetPositions?: Record<string, { x: number; y: number }>; allReady?: boolean; turnEndsAt?: number; news?: { headline: string; planet: string; turnsRemaining: number }[] }
  you: { id: string; name: string; money: number; fuel: number; inventory: Record<string, number>; inventoryAvgCost: Record<string, number>; currentPlanet: string; destinationPlanet: string; ready?: boolean; endGame?: boolean; modal?: { id: string; title: string; body: string }; inTransit?: boolean; transitFrom?: string; transitRemaining?: number; transitTotal?: number }
  visiblePlanet: { name: string; goods: Record<string, number>; prices: Record<string, number>; priceRanges?: Record<string, [number, number]>; fuelPrice?: number } | {}
}

type LobbyState = { rooms: LobbyRoom[] }

type WSOut = { type: string; payload?: any }

function useWS(url: string | null) {
  const wsRef = useRef<WebSocket | null>(null)
  const [ready, setReady] = useState(false)
  const [messages, setMessages] = useState<WSOut[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!url) return
    
    console.log('Attempting WebSocket connection to:', url)
    setError(null)
    
    const ws = new WebSocket(url)
    wsRef.current = ws
    
    ws.onopen = () => {
      console.log('WebSocket connected successfully')
      setReady(true)
      setError(null)
    }
    
    ws.onclose = (event) => {
      console.log('WebSocket closed:', event.code, event.reason)
      setReady(false)
      if (event.code !== 1000) { // Not a normal closure
        setError(`Connection closed: ${event.reason || 'Unknown reason'}`)
      }
    }
    
    ws.onerror = (event) => {
      console.error('WebSocket error:', event)
      setError('WebSocket connection failed. Check if the server is running and certificates are valid.')
    }
    
    ws.onmessage = (ev) => {
      try { 
        setMessages(m => [...m, JSON.parse(ev.data)]) 
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err)
      }
    }
    
    return () => { 
      ws.close(); 
      wsRef.current = null 
    }
  }, [url])

  const send = useMemo(() => (type: string, payload?: any) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return
    wsRef.current.send(JSON.stringify({ type, payload }))
  }, [])

  return { ready, messages, send, error }
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
    <div style={{ padding:'6px 0' }}>
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

// Wealth charts: visualize player wealth over turns and recent shifts
type WealthHistory = { roomId?: string; series: Record<string, { name: string; color: string; points: { turn: number; money: number }[] }> }
function WealthCharts({ history }: { history: WealthHistory }) {
  const entries = Object.entries(history.series)
  const allPoints = entries.flatMap(([, s]) => s.points)
  if (entries.length === 0 || allPoints.length === 0) {
    return <div>No data yet. Play a few turns to see graphs.</div>
  }
  let minTurn = Infinity, maxTurn = -Infinity, minMoney = Infinity, maxMoney = -Infinity
  entries.forEach(([, s]) => {
    s.points.forEach(p => {
      if (p.turn < minTurn) minTurn = p.turn
      if (p.turn > maxTurn) maxTurn = p.turn
      if (p.money < minMoney) minMoney = p.money
      if (p.money > maxMoney) maxMoney = p.money
    })
  })
  if (!isFinite(minTurn) || !isFinite(maxTurn)) return <div>No data.</div>
  if (minTurn === maxTurn) { maxTurn = minTurn + 1 }
  if (!isFinite(minMoney) || !isFinite(maxMoney)) return <div>No data.</div>
  if (minMoney === maxMoney) { minMoney -= 1; maxMoney += 1 }
  // add small paddings
  const moneyPad = Math.max(1, Math.round((maxMoney - minMoney) * 0.08))
  minMoney -= moneyPad; maxMoney += moneyPad
  const vbW = 900, vbH = 300
  const padL = 60, padR = 20, padT = 20, padB = 30
  const plotW = vbW - padL - padR
  const plotH = vbH - padT - padB
  const xFor = (t: number) => padL + ((t - minTurn) / (maxTurn - minTurn)) * plotW
  const yFor = (m: number) => padT + (1 - (m - minMoney) / (maxMoney - minMoney)) * plotH

  // Compute last-turn deltas per player
  type DeltaRow = { id: string; name: string; color: string; delta: number }
  const lastDeltas: DeltaRow[] = entries.map(([id, s]) => {
    const pts = s.points
    const n = pts.length
    const delta = n >= 2 ? (pts[n-1].money - pts[n-2].money) : 0
    return { id, name: s.name, color: s.color, delta }
  })
  const maxAbsDelta = Math.max(1, ...lastDeltas.map(d => Math.abs(d.delta)))

  // Recent swings (last up to 5 intervals): list top 3 by max abs delta
  const swings = entries.map(([id, s]) => {
    const pts = s.points
    const n = pts.length
    let maxAbs = 0
    let last = 0
    for (let i = Math.max(1, n-5); i < n; i++) {
      const d = pts[i].money - pts[i-1].money
      if (Math.abs(d) > maxAbs) maxAbs = Math.abs(d)
      if (i === n-1) last = d
    }
    return { id, name: s.name, color: s.color, maxAbs, last }
  }).sort((a,b)=> b.maxAbs - a.maxAbs).slice(0,3)

  return (
    <div>
      {/* Legend */}
      <div style={{ display:'flex', flexWrap:'wrap', gap:12, marginBottom:8 }}>
        {entries.map(([id, s]) => (
          <span key={id} style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
            <span style={{ width:10, height:10, borderRadius:5, background:s.color, boxShadow:'0 0 0 1px rgba(0,0,0,0.15)' }} />
            <span>{s.name}</span>
          </span>
        ))}
      </div>
      {/* Line chart */}
      <svg viewBox={`0 0 ${vbW} ${vbH}`} style={{ width:'100%', maxWidth:900, background:'var(--panel)', border:'1px solid var(--border)', borderRadius:8 }}>
        {/* Axes */}
        <rect x={padL} y={padT} width={plotW} height={plotH} fill="rgba(255,255,255,0.02)" stroke="var(--border)" />
        {/* Y ticks */}
        {Array.from({ length:4 }).map((_,i)=>{
          const yVal = minMoney + ((i+1)/5)*(maxMoney - minMoney)
          const y = yFor(yVal)
          return <g key={i}>
            <line x1={padL} y1={y} x2={padL+plotW} y2={y} stroke="rgba(255,255,255,0.05)" />
            <text x={padL-6} y={y+4} textAnchor="end" fontSize={11} fill="var(--muted)">${Math.round(yVal)}</text>
          </g>
        })}
        {/* Lines */}
        {entries.map(([id, s]) => {
          const pts = [...s.points].sort((a,b)=>a.turn-b.turn)
          const d = pts.map((p, idx) => `${idx===0?'M':'L'} ${xFor(p.turn).toFixed(1)} ${yFor(p.money).toFixed(1)}`).join(' ')
          return <path key={id} d={d} fill="none" stroke={s.color} strokeWidth={2} opacity={0.95} />
        })}
      </svg>

      {/* Last turn deltas bar chart (centered baseline) */}
      <h4 style={{ marginTop:16 }}>Last Turn Change</h4>
  <svg viewBox="0 0 900 220" style={{ width:'100%', maxWidth:900, background:'var(--panel)', border:'1px solid var(--border)', borderRadius:8 }}>
        {(() => {
          const W = 900, H = 220
          const pad = 20
          const zeroY = H/2
          const barW = Math.max(20, Math.min(60, (W - pad*2) / Math.max(1, lastDeltas.length) * 0.6))
          const step = (W - pad*2) / Math.max(1, lastDeltas.length)
          return (
            <g>
              <line x1={pad} y1={zeroY} x2={W-pad} y2={zeroY} stroke="var(--border)" />
              {lastDeltas.map((d, i) => {
                const cx = pad + step * (i + 0.5)
                const h = Math.round(Math.abs(d.delta) / maxAbsDelta * (H/2 - pad))
                const y = d.delta >= 0 ? zeroY - h : zeroY
                return (
                  <g key={d.id}>
                    <rect x={cx - barW/2} y={y} width={barW} height={Math.max(1, h)} fill={d.color} opacity={0.8} />
                    <text x={cx} y={d.delta>=0 ? y - 6 : y + h + 14} textAnchor="middle" fontSize={11} fill="var(--text)">{d.delta>=0? '+':''}{d.delta}</text>
                    <text x={cx} y={H-6} textAnchor="middle" fontSize={11} fill="var(--muted)">{history.series[d.id]?.name || d.id}</text>
                  </g>
                )
              })}
            </g>
          )
        })()}
      </svg>
      {/* Recent biggest swings (last 5 turns) */}
      <div style={{ marginTop:12 }}>
        <strong>Recent biggest swings (last 5 turns):</strong>
        <ul style={{ margin:'6px 0 0 18px', color:'var(--text)' }}>
          {swings.map(s => (
            <li key={s.id}>
              <span style={{ display:'inline-block', width:10, height:10, borderRadius:5, background:s.color, marginRight:6, boxShadow:'0 0 0 1px rgba(0,0,0,0.15)' }} />
              {s.name}: max ±{s.maxAbs} (last {s.last>=0?'+':''}{s.last})
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

export function App() {
  const isMobile = useIsMobile()
  const [stage, setStage] = useState<'title'|'lobby'|'room'>('title')
  const [name, setName] = useState('')
  const [url, setUrl] = useState<string | null>(null)
  const { ready, messages, send, error } = useWS(url)

  const [lobby, setLobby] = useState<LobbyState>({ rooms: [] })
  const [room, setRoom] = useState<RoomState | null>(null)
  const [amountsByGood, setAmountsByGood] = useState<Record<string, number>>({})
  const planetsContainerRef = useRef<HTMLDivElement | null>(null)
  const planetRefs = useRef<Record<string, HTMLLIElement | null>>({})
  const playersMenuRef = useRef<HTMLDivElement | null>(null)
  const inventoryMenuRef = useRef<HTMLDivElement | null>(null)
  const [planetPos, setPlanetPos] = useState<Record<string, { x: number; y: number }>>({})
  const [containerSize, setContainerSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 })
  const [playersOpen, setPlayersOpen] = useState(false)
  const [inventoryOpen, setInventoryOpen] = useState(false)
  const [now, setNow] = useState<number>(() => Date.now())
  // Tabs: map | market | graphs
  const [activeTab, setActiveTab] = useState<'map'|'market'|'graphs'>('map')
  // Wealth history per room: per-player series of {turn, money}
  const [wealthHistory, setWealthHistory] = useState<{ roomId?: string; series: Record<string, { name: string; color: string; points: { turn: number; money: number }[] }> }>({ roomId: undefined, series: {} })
  // Local floating notifications (e.g., Dock Tax)
  const [toasts, setToasts] = useState<{ id: string; text: string; at: number }[]>([])
  const lastDockHandled = useRef<string | null>(null)

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

  // Intercept Dock Tax modal and convert it into a floating toast instead of blocking modal
  useEffect(() => {
    if (!room) return
    const modal: any = (room.you as any)?.modal
    if (!modal || !modal.id) return
    const title = (modal as any).title
    if (title === 'Dock Tax') {
      if (lastDockHandled.current !== modal.id) {
        lastDockHandled.current = modal.id
        setToasts(ts => [...ts, { id: modal.id, text: modal.body || 'Docking fee charged.', at: Date.now() }])
        // Immediately ack so it doesn't block queue
        send('ackModal', { id: modal.id })
      }
    }
  }, [room?.you && (room.you as any).modal?.id])

  // Auto-remove toasts after ~2.5s
  useEffect(() => {
    if (toasts.length === 0) return
    const t = setInterval(() => {
      const now = Date.now()
      setToasts(ts => ts.filter(x => now - x.at < 2500))
    }, 500)
    return () => clearInterval(t)
  }, [toasts.length])

  // Track wealth history per turn (numeric money only); reset on room change
  useEffect(() => {
    if (!room) return
    const currRoomId = (room.room as any)?.id
    const turn = (room.room as any)?.turn
    if (currRoomId == null || typeof turn !== 'number') return
    setWealthHistory(prev => {
      let next = prev
      if (prev.roomId !== currRoomId) {
        next = { roomId: currRoomId, series: {} }
      } else {
        // shallow clone for immutability
        next = { roomId: prev.roomId, series: { ...prev.series } }
      }
      const playersArr: Array<{ id: string; name: string; money: any }> = (room.room as any)?.players || []
      playersArr.forEach(pl => {
        const num = typeof pl.money === 'number' ? pl.money : null
        if (num == null) return
        const key = String(pl.id)
        const color = colorFor(key)
        const entry = next.series[key] || { name: pl.name, color, points: [] }
        // avoid duplicate for same turn
        const last = entry.points[entry.points.length - 1]
        if (!last || last.turn !== turn) {
          entry.points = [...entry.points, { turn, money: num }]
          // cap history length to last 200 points
          if (entry.points.length > 200) entry.points = entry.points.slice(-200)
        }
        // update display name/color
        entry.name = pl.name
        entry.color = color
        next.series[key] = entry
      })
      return next
    })
  }, [room?.room?.id, room?.room?.turn, room?.room?.players])

  // Close Players menu on outside click
  useEffect(() => {
    if (!playersOpen) return
    const onDocDown = (e: MouseEvent) => {
      const el = playersMenuRef.current
      if (!el) return
      if (e.target instanceof Node && !el.contains(e.target)) {
        setPlayersOpen(false)
      }
    }
    document.addEventListener('mousedown', onDocDown)
    return () => document.removeEventListener('mousedown', onDocDown)
  }, [playersOpen])

  // Close Ship Inventory menu on outside click
  useEffect(() => {
    if (!inventoryOpen) return
    const onDocDown = (e: MouseEvent) => {
      const el = inventoryMenuRef.current
      if (!el) return
      if (e.target instanceof Node && !el.contains(e.target)) {
        setInventoryOpen(false)
      }
    }
    document.addEventListener('mousedown', onDocDown)
    return () => document.removeEventListener('mousedown', onDocDown)
  }, [inventoryOpen])

  // Actions
  const onConnect = () => {
    const host = typeof window !== 'undefined' ? window.location.hostname : 'localhost'
    const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:'
    const wsUrl = isHttps ? `wss://${host}/ws` : `ws://${host}:8080/ws`
    setUrl(wsUrl)
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
  const setEndGame = (end: boolean) => send('setEndGame', { endGame: end })

  const selectPlanet = (planet: string) => send('selectPlanet', { planet })
  const buy = (good: string, amount: number) => send('buy', { good, amount })
  const sell = (good: string, amount: number) => send('sell', { good, amount })
  const ackModal = (id?: string) => send('ackModal', { id })
  const refuel = (amount?: number) => send('refuel', { amount: amount ?? 0 })
  // Player info modal state
  const [playerInfo, setPlayerInfo] = useState<null | { id: string; name: string; inventory: Record<string, number>; inventoryAvgCost: Record<string, number>; usedSlots: number; capacity: number; history?: { turn: number; text: string }[] }>(null)
  useEffect(() => {
    const last = messages[messages.length-1]
    if (!last) return
    if (last.type === 'playerInfo') {
      setPlayerInfo(last.payload)
    }
  }, [messages])
  const requestPlayerInfo = (pid: string) => send('getPlayer', { playerId: pid })

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

  // Generate a starfield background as a data-URL SVG sized to the map container
  const starfieldUrl = useMemo(() => {
    const w = Math.max(1, Math.floor(containerSize.width || 800))
    const h = Math.max(1, Math.floor(containerSize.height || 380))
    const count = Math.round((w * h) / 5000) // a bit denser for visibility
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    const ctx = canvas.getContext('2d')
    if (!ctx) return ''
    // black base
    ctx.fillStyle = '#000'
    ctx.fillRect(0, 0, w, h)
    const colors = [
      'rgba(255,255,255,0.95)',
      'rgba(255,255,255,0.8)',
      'rgba(190,210,255,0.9)',
      'rgba(210,190,255,0.9)'
    ]
    for (let i = 0; i < count; i++) {
      const x = Math.floor(Math.random() * w)
      const y = Math.floor(Math.random() * h)
      ctx.fillStyle = colors[Math.floor(Math.random() * colors.length)]
      // draw a 1px star, some slightly brighter (2px)
      if (Math.random() < 0.15) {
        ctx.fillRect(x, y, 2, 2)
      } else {
        ctx.fillRect(x, y, 1, 1)
      }
    }
    return canvas.toDataURL('image/png')
  }, [containerSize.width, containerSize.height])

  const colorFor = (id: string) => {
    let h = 0
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360
    return `hsl(${h},70%,45%)`
  }

  // UI
  if (stage === 'title') {
    return (
      <div style={{ 
        padding: isMobile ? 16 : 24,
        maxWidth: isMobile ? '100vw' : 'none',
        boxSizing: 'border-box'
      }}>
        <h1 className="glow" style={{ 
          fontSize: isMobile ? '2rem' : '2.5rem',
          textAlign: 'center'
        }}>Space Trader</h1>
        
        {import.meta.env.VITE_DEV_MODE === 'true' && (
          <div style={{ 
            marginBottom: 16, 
            padding: isMobile ? 8 : 12, 
            backgroundColor: '#4444ff', 
            color: 'white', 
            borderRadius: 4, 
            fontSize: isMobile ? '0.8em' : '0.9em'
          }}>
            <strong>Development Mode:</strong> If you see certificate warnings, you may need to:
            <br />• Visit <a href="https://localhost:8443/rooms" target="_blank" style={{ color: '#ccddff' }}>https://localhost:8443/rooms</a> and accept the self-signed certificate
            <br />• Or run the servers with HTTP fallback for testing
          </div>
        )}
        
        <div style={{ 
          display: 'flex', 
          flexDirection: isMobile ? 'column' : 'row',
          gap: isMobile ? 12 : 8,
          alignItems: isMobile ? 'stretch' : 'center'
        }}>
          <input 
            placeholder="Your name" 
            value={name} 
            onChange={e=>setName(e.target.value)}
            style={{
              padding: isMobile ? '12px' : '8px',
              fontSize: isMobile ? '16px' : '14px',
              flex: 1
            }}
          />
          <button 
            onClick={onConnect} 
            style={{ 
              padding: isMobile ? '12px 20px' : '8px 16px',
              fontSize: isMobile ? '16px' : '14px',
              minHeight: isMobile ? '48px' : 'auto'
            }}
          >
            Connect
          </button>
        </div>
        
        {error && (
          <div style={{ 
            marginTop: 16, 
            padding: isMobile ? 8 : 12, 
            backgroundColor: '#ff4444', 
            color: 'white', 
            borderRadius: 4,
            fontSize: isMobile ? '0.8em' : '1em'
          }}>
            <strong>Connection Error:</strong> {error}
          </div>
        )}
        
        {url && (
          <div style={{ 
            marginTop: 8, 
            fontSize: isMobile ? '0.7em' : '0.8em', 
            color: '#666',
            wordBreak: 'break-all'
          }}>
            Connecting to: {url}
          </div>
        )}
      </div>
    )
  }

  if (stage === 'lobby') {
    return (
      <div style={{ 
        padding: isMobile ? 16 : 24,
        maxWidth: isMobile ? '100vw' : 'none',
        boxSizing: 'border-box'
      }}>
        <h2 className="glow" style={{ 
          fontSize: isMobile ? '1.5rem' : '2rem',
          textAlign: 'center'
        }}>Lobby</h2>
        <button 
          onClick={createRoom}
          style={{ 
            padding: isMobile ? '12px 20px' : '8px 16px',
            fontSize: isMobile ? '16px' : '14px',
            minHeight: isMobile ? '48px' : 'auto',
            width: isMobile ? '100%' : 'auto'
          }}
        >
          Create Game
        </button>
        <h3 style={{ 
          marginTop: 16,
          fontSize: isMobile ? '1.2rem' : '1.5rem'
        }}>Active Rooms</h3>
        <ul style={{ 
          listStyle: 'none',
          padding: 0,
          margin: 0
        }}>
          {lobby.rooms.map(r => (
            <li key={r.id} style={{ marginBottom: isMobile ? 12 : 8 }}>
              <button 
                onClick={() => joinRoom(r.id)}
                style={{ 
                  padding: isMobile ? '12px 16px' : '8px 12px',
                  fontSize: isMobile ? '16px' : '14px',
                  minHeight: isMobile ? '48px' : 'auto',
                  width: isMobile ? '100%' : 'auto',
                  textAlign: 'left'
                }}
              >
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
  const capacity = (r.you as any).capacity ?? 200
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
    <div style={{ overflowX: 'hidden', display:'flex', flexDirection:'column', minHeight:'100vh' }}>
  {/* News ticker below header (blue-hued) */}
  {r.you.modal && r.you.modal.id && (r.you as any).modal?.title !== 'Dock Tax' && (
        <div style={{ position:'fixed', inset:0, background:'rgba(0,0,0,0.55)', display:'flex', alignItems:'center', justifyContent:'center', zIndex:2000, padding: isMobile ? 16 : 0 }}>
          <div className="panel" style={{ 
            padding: isMobile ? 20 : 16, 
            width: isMobile ? '100%' : 360, 
            maxWidth: isMobile ? '90vw' : 'none',
            maxHeight: isMobile ? '80vh' : 'none',
            overflow: isMobile ? 'auto' : 'visible'
          }}>
            <div style={{ 
              fontWeight: 700, 
              marginBottom: isMobile ? 12 : 8,
              fontSize: isMobile ? 18 : 'inherit'
            }}>{r.you.modal.title}</div>
            <div style={{ 
              whiteSpace: 'pre-wrap', 
              marginBottom: isMobile ? 16 : 12,
              fontSize: isMobile ? 16 : 'inherit',
              lineHeight: isMobile ? 1.5 : 'inherit'
            }}>{r.you.modal.body}</div>
            { (r.you.modal as any).kind ? (
              <div style={{ display:'flex', justifyContent:'flex-end', gap: isMobile ? 12 : 8, flexDirection: isMobile ? 'column' : 'row' }}>
                <button 
                  onClick={()=>send('respondModal', { id: r.you.modal?.id, accept: false })}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? 16 : 'inherit',
                    minHeight: isMobile ? 48 : 'auto',
                    order: isMobile ? 2 : 'unset'
                  }}
                >
                  Decline
                </button>
                <button 
                  onClick={()=>send('respondModal', { id: r.you.modal?.id, accept: true })}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? 16 : 'inherit',
                    minHeight: isMobile ? 48 : 'auto',
                    order: isMobile ? 1 : 'unset'
                  }}
                >
                  Accept
                </button>
              </div>
            ) : (
              <div style={{ display:'flex', justifyContent:'flex-end' }}>
                <button 
                  onClick={()=>ackModal(r.you.modal?.id)}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? 16 : 'inherit',
                    minHeight: isMobile ? 48 : 'auto'
                  }}
                >
                  OK
                </button>
              </div>
            )}
          </div>
        </div>
      )}
  <div style={{ 
    display: 'flex', 
    alignItems: 'center', 
    gap: isMobile ? 8 : 12, 
    justifyContent: 'space-between', 
    padding: isMobile ? '8px 12px' : '10px 16px', 
    borderBottom: '1px solid var(--border)', 
    position: 'relative',
    flexWrap: isMobile ? 'wrap' : 'nowrap'
  }}>
  <div style={{ 
    display: 'flex', 
    gap: isMobile ? 8 : 12, 
    alignItems: 'center', 
    position: 'relative',
    flexWrap: isMobile ? 'wrap' : 'nowrap',
    width: isMobile ? '100%' : 'auto'
  }}>
          <strong className="glow" style={{ fontSize: isMobile ? 16 : 'inherit' }}>{r.room.name}</strong>
          <span className="muted" style={{ fontSize: isMobile ? 14 : 'inherit' }}>Turn: {r.room.turn}</span>
          {typeof r.room.turnEndsAt === 'number' && (
            <span className="muted" style={{ fontSize: isMobile ? 14 : 'inherit' }}>
              · {Math.max(0, Math.ceil((r.room.turnEndsAt - now) / 1000))}s
            </span>
          )}
          {/* Tabs */}
          <div style={{ 
            marginLeft: isMobile ? 0 : 8, 
            display: 'inline-flex', 
            border: '1px solid var(--border)', 
            borderRadius: 8, 
            overflow: 'hidden',
            width: isMobile ? '100%' : 'auto',
            order: isMobile ? 1 : 'unset'
          }}>
            <button onClick={()=>setActiveTab('map')} style={{ 
              padding: isMobile ? '12px 16px' : '4px 8px', 
              background: activeTab==='map' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              border: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? 16 : 'inherit',
              minHeight: isMobile ? 48 : 'auto'
            }}>Map</button>
            <button onClick={()=>setActiveTab('market')} style={{ 
              padding: isMobile ? '12px 16px' : '4px 8px', 
              background: activeTab==='market' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? 16 : 'inherit',
              minHeight: isMobile ? 48 : 'auto'
            }}>Market</button>
            <button onClick={()=>setActiveTab('graphs')} style={{ 
              padding: isMobile ? '12px 16px' : '4px 8px', 
              background: activeTab==='graphs' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? 16 : 'inherit',
              minHeight: isMobile ? 48 : 'auto'
            }}>Graphs</button>
          </div>
          <div ref={playersMenuRef} style={{ position: 'relative' }}>
            <button 
              onClick={() => setPlayersOpen(v=>!v)} 
              aria-expanded={playersOpen} 
              aria-haspopup="menu"
              style={{
                padding: isMobile ? '12px 16px' : '6px 12px',
                fontSize: isMobile ? 16 : 'inherit',
                minHeight: isMobile ? 48 : 'auto'
              }}
            >
              Players ▾
            </button>
            {playersOpen && (
              <div className="panel" style={{ 
                position: 'absolute', 
                top: '100%', 
                left: isMobile ? -200 : 0, 
                marginTop: 6, 
                padding: 8, 
                zIndex: 1000, 
                minWidth: isMobile ? 320 : 280,
                maxWidth: isMobile ? '90vw' : 'none'
              }}>
                <ul style={{ listStyle:'none', padding:0, margin:0 }}>
          {r.room.players.map((pl)=> (
                    <li key={pl.id} style={{ 
                      display: 'flex', 
                      alignItems: 'center', 
                      gap: isMobile ? 12 : 8, 
                      fontSize: isMobile ? 14 : 12, 
                      lineHeight: 1.2, 
                      padding: isMobile ? '10px 12px' : '6px 8px', 
                      borderRadius: 6 
                    }}>
                      <span title={pl.ready ? 'Ready' : 'Not Ready'} style={{ 
                        width: isMobile ? 10 : 8, 
                        height: isMobile ? 10 : 8, 
                        borderRadius: isMobile ? 5 : 4, 
                        background: pl.ready ? 'var(--good)' : 'var(--bad)' 
                      }} />
                      <span style={{ 
                        width: isMobile ? 12 : 10, 
                        height: isMobile ? 12 : 10, 
                        borderRadius: isMobile ? 6 : 5, 
                        background: colorFor(String(pl.id)), 
                        boxShadow: '0 0 0 1px rgba(0,0,0,0.15)' 
                      }} />
            <button 
              onClick={()=>requestPlayerInfo(pl.id)} 
              style={{ 
                flex: 1, 
                overflow: 'hidden', 
                textOverflow: 'ellipsis', 
                whiteSpace: 'nowrap', 
                textAlign: 'left', 
                background: 'transparent', 
                border: 'none', 
                padding: isMobile ? '8px 0' : 0, 
                cursor: 'pointer', 
                color: 'var(--accent2)',
                fontSize: isMobile ? 16 : 'inherit',
                minHeight: isMobile ? 44 : 'auto'
              }} 
              title="View inventory"
            >
              {pl.name}
            </button>
                      <span style={{ fontSize: isMobile ? 14 : 'inherit' }}>${pl.money}</span>
                      <span className="muted" style={{ fontSize: isMobile ? 12 : 'inherit' }}>@ {pl.currentPlanet}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
          <div ref={inventoryMenuRef} style={{ position: 'relative' }}>
            <button 
              onClick={() => setInventoryOpen(v=>!v)} 
              aria-expanded={inventoryOpen} 
              aria-haspopup="menu"
              style={{
                padding: isMobile ? '12px 16px' : '6px 12px',
                fontSize: isMobile ? 16 : 'inherit',
                minHeight: isMobile ? 48 : 'auto'
              }}
            >
              Ship Inventory [{usedSlots}/{capacity}] ▾
            </button>
            {inventoryOpen && (
              <div className="panel" style={{ 
                position: 'absolute', 
                top: '100%', 
                right: isMobile ? 0 : 'auto',
                left: isMobile ? 'auto' : 0, 
                marginTop: 6, 
                padding: 12, 
                zIndex: 1000, 
                minWidth: isMobile ? 280 : 300, 
                maxHeight: isMobile ? '60vh' : 360, 
                overflow: 'auto',
                maxWidth: isMobile ? '85vw' : 'none'
              }}>
                {Object.keys(r.you.inventory).length === 0 ? (
                  <div style={{ fontSize: isMobile ? 16 : 'inherit' }}>Empty</div>
                ) : (
                  <ul style={{ listStyle:'none', padding:0, margin:0 }}>
                    {Object.keys(r.you.inventory).sort().map(g => {
                      const qty = r.you.inventory[g]
                      const avg = r.you.inventoryAvgCost?.[g]
                      return (
                        <li key={g} style={{ 
                          padding: isMobile ? '8px 0' : '4px 0',
                          fontSize: isMobile ? 16 : 'inherit'
                        }}>
                          {g}: {qty}{typeof avg === 'number' ? ` (avg $${avg})` : ''}
                        </li>
                      )
                    })}
                  </ul>
                )}
              </div>
            )}
          </div>
        </div>
    <div style={{ 
      display: 'flex', 
      gap: isMobile ? 8 : 12, 
      alignItems: 'center',
      flexWrap: isMobile ? 'wrap' : 'nowrap'
    }}>
          <button
      onClick={() => send('setReady', { ready: !Boolean(r.you.ready) })}
      style={{ 
        padding: isMobile ? '12px 20px' : '4px 10px', 
        borderRadius: 6, 
        border: '1px solid var(--border)', 
        background: r.you.ready ? 'rgba(52,211,153,0.18)' : 'rgba(248,113,113,0.18)', 
        color: r.you.ready ? 'var(--good)' : 'var(--bad)',
        fontSize: isMobile ? 16 : 'inherit',
        fontWeight: isMobile ? 600 : 'normal',
        minHeight: isMobile ? 48 : 'auto'
      }}
            title={r.you.ready ? 'Ready' : 'Not Ready'}
          >
            Ready
          </button>
          <span style={{ fontSize: isMobile ? 18 : 'inherit' }}><strong>${r.you.money}</strong></span>
          <div title="Ship fuel (price varies by planet)" style={{ 
            display: isMobile ? 'flex' : 'block',
            flexDirection: isMobile ? 'column' : 'row',
            gap: isMobile ? 4 : 0,
            alignItems: isMobile ? 'flex-start' : 'center'
          }}>
            <span style={{ 
              marginLeft: isMobile ? 0 : 8,
              fontSize: isMobile ? 14 : 'inherit'
            }}>
              Fuel: <strong>{r.you.fuel}</strong>/{(r.you as any).fuelCapacity ?? 100}
            </span>
      <span className="muted" style={{ 
        marginLeft: isMobile ? 0 : 8,
        fontSize: isMobile ? 12 : 'inherit'
      }}>@ ${ fuelPrice }/unit</span>
            <button 
              onClick={() => refuel(0)} 
              style={{ 
                marginLeft: isMobile ? 0 : 6,
                marginTop: isMobile ? 4 : 0,
                padding: isMobile ? '8px 16px' : '4px 8px',
                fontSize: isMobile ? 14 : 'inherit',
                minHeight: isMobile ? 40 : 'auto'
              }} 
              disabled={inTransit || (r.you.fuel ?? 0) >= ((r.you as any).fuelCapacity ?? 100) || (r.you.money ?? 0) < fuelPrice} 
              title={inTransit ? 'Unavailable while in transit' : ((r.you.fuel ?? 0) >= ((r.you as any).fuelCapacity ?? 100)) ? 'Tank full' : ((r.you.money ?? 0) < fuelPrice ? 'Not enough credits' : 'Fill to max')}
            >
              Fill
            </button>
          </div>
          {!r.room.started && (
            <div style={{ 
              display: 'flex', 
              gap: isMobile ? 12 : 8,
              flexDirection: isMobile ? 'row' : 'row',
              width: isMobile ? '100%' : 'auto',
              order: isMobile ? 1 : 'unset'
            }}>
              <button 
                onClick={startGame} 
                disabled={!r.room.allReady} 
                title={r.room.allReady ? 'All players are ready' : 'Waiting for all players to be ready'}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? 16 : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none'
                }}
              >
                Start Game
              </button>
              <button 
                onClick={addBot}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? 16 : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none'
                }}
              >
                Add Bot
              </button>
              <button 
                onClick={exitRoom}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? 16 : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none'
                }}
              >
                Exit
              </button>
            </div>
          )}
          {playerInfo && (
            <div style={{ position:'fixed', inset:0, background:'rgba(0,0,0,0.55)', display:'flex', alignItems:'center', justifyContent:'center', zIndex:2100, padding: isMobile ? 16 : 0 }}>
              <div className="panel" style={{ 
                padding: isMobile ? 20 : 16, 
                width: isMobile ? '100%' : 380, 
                maxWidth: isMobile ? '90vw' : 'none',
                maxHeight: isMobile ? '80vh' : '80vh', 
                overflow: 'auto' 
              }}>
                <div style={{ display:'flex', alignItems:'center', justifyContent:'space-between', marginBottom: isMobile ? 12 : 8 }}>
                  <div style={{ 
                    fontWeight: 700,
                    fontSize: isMobile ? 18 : 'inherit'
                  }}>Ship Inventory — {playerInfo.name}</div>
                  <button 
                    onClick={()=>setPlayerInfo(null)}
                    style={{
                      padding: isMobile ? '8px 16px' : '4px 8px',
                      fontSize: isMobile ? 16 : 'inherit',
                      minHeight: isMobile ? 40 : 'auto'
                    }}
                  >
                    Close
                  </button>
                </div>
                <div className="muted" style={{ marginBottom:8 }}>Capacity: {playerInfo.usedSlots}/{playerInfo.capacity}</div>
                {Object.keys(playerInfo.inventory).length === 0 ? (
                  <div>Empty</div>
                ) : (
                  <ul style={{ listStyle:'none', padding:0, margin:0 }}>
                    {Object.keys(playerInfo.inventory).sort().map(g => (
                      <li key={g} style={{ padding:'4px 0' }}>
                        {g}: {playerInfo.inventory[g]}{typeof playerInfo.inventoryAvgCost[g] === 'number' ? ` (avg $${playerInfo.inventoryAvgCost[g]})` : ''}
                      </li>
                    ))}
                  </ul>
                )}
                <div style={{ marginTop:12 }}>
                  <div style={{ fontWeight:700, marginBottom:6 }}>Recent Actions</div>
      {playerInfo.history && playerInfo.history.length>0 ? (
    <div style={{ maxHeight:200, overflowY:'auto', border:'1px solid var(--border)', borderRadius:6, padding:'6px 8px' }}>
          <ul style={{ listStyle:'none', padding:0, margin:0, fontSize:13, color:'var(--text)' }}>
            {playerInfo.history.slice(-100).reverse().map((h, idx) => (
                          <li key={idx} style={{ padding:'2px 0' }}>Turn {h.turn}: {h.text}</li>
                        ))}
                      </ul>
                    </div>
                  ) : (
                    <div style={{ color:'#6b7280' }}>No recent actions.</div>
                  )}
                </div>
              </div>
            </div>
          )}
          {r.room.started && (
            <div style={{ 
              display: 'flex', 
              gap: isMobile ? 12 : 8,
              width: isMobile ? '100%' : 'auto',
              order: isMobile ? 1 : 'unset'
            }}>
              <button 
                onClick={()=>setEndGame(!Boolean(r.you.endGame))} 
                title="Toggle End Game for this room"
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? 16 : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none',
                  background: r.you.endGame ? 'rgba(248,113,113,0.18)' : 'rgba(52,211,153,0.18)',
                  color: r.you.endGame ? 'var(--bad)' : 'var(--good)',
                  border: '1px solid var(--border)'
                }}
              >
                {r.you.endGame ? 'Cancel End Game' : 'End Game'}
              </button>
              <button 
                onClick={exitRoom}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? 16 : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none'
                }}
              >
                Exit
              </button>
            </div>
          )}
        </div>
        {/* Floating toasts under money (top-right). No impact on news ticker */}
        <style>
          {`
            @keyframes slideDownFade {
              0% { opacity: 1; transform: translateY(0); }
              100% { opacity: 0; transform: translateY(20vh); }
            }
          `}
        </style>
        <div aria-live="polite" style={{ position:'absolute', top: 42, right: 16, pointerEvents:'none', width: 280, height: 0 }}>
          {toasts.map((t, i) => (
            <div key={t.id}
                 style={{
                   position:'absolute', top:0, right:0,
                   background:'linear-gradient(180deg, rgba(167,139,250,0.2), rgba(34,211,238,0.2))', color:'var(--text)', padding:'8px 12px', borderRadius:8,
                   border:'1px solid var(--border)', boxShadow:'0 6px 16px rgba(0,0,0,0.35), 0 0 24px rgba(167,139,250,0.25)',
                   animation: 'slideDownFade 2.2s ease-out forwards',
                   fontSize: 13, maxWidth: '100%', zIndex: 1000 + i,
                 }}>
              {t.text}
            </div>
          ))}
        </div>
      </div>
  <div className="ticker">
    <NewsTicker items={(r.room.news && r.room.news.length>0) ? r.room.news.map(n=>n.headline) : []} />
  </div>
  {/* Content area fills window below header/ticker */}
  <div style={{ flex:1, minHeight:0, display:'flex', flexDirection:'column' }}>
    {activeTab==='map' && (
      <div style={{ flex:1, minHeight:0, display:'flex', flexDirection:'column', padding: isMobile ? 12 : 16 }}>
        <h3 className="glow" style={{ fontSize: isMobile ? 18 : 'inherit' }}>{mapTitle}</h3>
        <div ref={planetsContainerRef} className="panel" style={{ 
          position:'relative', 
          flex:1, 
          minHeight: isMobile ? 300 : 0, 
          overflow:'hidden', 
          backgroundColor:'#000', 
          backgroundImage: `url(${starfieldUrl})`, 
          backgroundSize:'cover', 
          backgroundPosition:'center', 
          backgroundRepeat:'no-repeat',
          touchAction: isMobile ? 'pan-x pan-y' : 'auto'
        }}>
          <ul style={{ listStyle:'none', padding:0, margin:0, position:'absolute', inset:0 }}>
            {r.room.planets.map(p => {
              const onPlanet = (r.room.players as any[]).filter(pl => pl.currentPlanet === p && !(pl as any).bankrupt)
              const center = planetPos[p]
              const left = center ? center.x : 0
              const top = center ? center.y : 0
              const need = travelUnits(r.you.currentPlanet, p)
              const canReach = !inTransit && (p === r.you.currentPlanet || need <= (r.you.fuel ?? 0))
              const isHere = p === r.you.currentPlanet
              return (
                <li key={p} ref={el => (planetRefs.current[p] = el)} style={{ 
                  position:'absolute', 
                  left, 
                  top, 
                  transform:'translate(-50%, -50%)', 
                  display:'flex', 
                  flexDirection:'column', 
                  alignItems:'center', 
                  gap: isMobile ? 6 : 4, 
                  padding: isMobile ? 12 : 8, 
                  border: isHere ? '2px solid transparent' : '1px solid transparent', 
                  borderRadius: isMobile ? 12 : 8, 
                  background:'transparent'
                }}>
                  <button
                    disabled={p===r.you.currentPlanet || !canReach}
                    onClick={()=>selectPlanet(p)}
                    style={{ 
                      textAlign:'center', 
                      background:'var(--panelElevated)', 
                      border:'1px solid var(--border)',
                      padding: isMobile ? '12px 16px' : '8px 12px',
                      fontSize: isMobile ? 16 : 'inherit',
                      minHeight: isMobile ? 48 : 'auto',
                      minWidth: isMobile ? 80 : 'auto',
                      borderRadius: isMobile ? 8 : 4,
                      cursor: 'pointer',
                      touchAction: 'manipulation'
                    }}
                    title={inTransit ? 'Unavailable while in transit' : (p===r.you.currentPlanet ? 'You are here' : (!canReach ? `Need ${need} units (have ${r.you.fuel ?? 0})` : undefined))}
                  >
                    {p}
                  </button>
                  <div style={{ 
                    display:'flex', 
                    gap: isMobile ? 6 : 4, 
                    marginTop: isMobile ? 6 : 4, 
                    justifyContent:'center',
                    flexWrap: 'wrap'
                  }}>
                    {onPlanet.filter((pl:any)=> !(pl.id===r.you.id && inTransit)).map((pl:any) => (
                      <span
                        key={pl.id}
                        title={pl.name}
                        style={{ 
                          width: isMobile ? 18 : 14, 
                          height: isMobile ? 18 : 14, 
                          borderRadius: isMobile ? 9 : 7, 
                          background: colorFor(String(pl.id)), 
                          color:'#fff', 
                          display:'inline-flex', 
                          alignItems:'center', 
                          justifyContent:'center', 
                          fontSize: isMobile ? 12 : 10, 
                          boxShadow:'0 0 0 1px rgba(0,0,0,0.15)',
                          fontWeight: isMobile ? 600 : 'normal'
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
            {(r.room.players as any[]).filter(pl => !(pl as any).bankrupt).map(pl => {
              const from = planetPos[pl.currentPlanet]
              const to = pl.destinationPlanet ? planetPos[pl.destinationPlanet] : undefined
              if (!from || !to) return null
              if (pl.destinationPlanet === pl.currentPlanet) return null
              const x1 = from.x, y1 = from.y
              const x2 = to.x, y2 = to.y
              const d = `M ${x1},${y1} L ${x2},${y2}`
              return (
                <path key={pl.id} d={d} fill="none" stroke={colorFor(String(pl.id))} strokeWidth={isMobile ? 3 : 2} strokeLinecap="round" markerEnd={`url(#arrow-head-${pl.id})`} opacity={0.95} />
              )
            })}
            {inTransit && yourTransitPos && (
              <circle cx={yourTransitPos.x} cy={yourTransitPos.y} r={isMobile ? 9 : 7} fill={colorFor(String(r.you.id))} stroke="#111" strokeOpacity={0.15} />
            )}
          </svg>
        </div>
      </div>
    )}

    {activeTab==='market' && (
      <div style={{ padding: isMobile ? 12 : 16 }}>
        <h3 className="glow" style={{ fontSize: isMobile ? 18 : 'inherit' }}>Market — {visible.name || r.you.currentPlanet}</h3>
        <div className="panel" style={{ overflowX: 'auto' }}>
          {isMobile ? (
            // Mobile card layout for better usability
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
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
                      ? { background:'rgba(52,211,153,0.18)', color:'var(--good)', border:'1px solid rgba(52,211,153,0.35)' }
                      : price < youPaid
                        ? { background:'rgba(248,113,113,0.18)', color:'var(--bad)', border:'1px solid rgba(248,113,113,0.35)' }
                        : { background:'rgba(255,255,255,0.06)', color:'var(--text)', border:'1px solid var(--border)' })
                  : undefined
                const disabledTrade = inTransit
                const rangeText = range ? `${range[0]}–${range[1]}` : '—'
                const pctText = range ? (()=>{ const max=range[1]; const pct=max>0? Math.max(0, Math.min(100, Math.round((price/max)*100))) : 0; return `${pct}%` })() : '—'
                
                return (
                  <div key={g} style={{ 
                    padding: 16, 
                    border: '1px solid var(--border)', 
                    borderRadius: 8,
                    background: 'rgba(255,255,255,0.02)'
                  }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                      <h4 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>{g}</h4>
                      <div style={{ fontSize: 16, fontWeight: 600 }}>${price}</div>
                    </div>
                    
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 12, fontSize: 14 }}>
                      <div>Available: {available}</div>
                      <div>Range: {rangeText}</div>
                      <div>Owned: {owned}{owned>0 && typeof youPaid === 'number' ? ` (avg $${youPaid})` : ''}</div>
                      <div>% of Max: {pctText}</div>
                    </div>
                    
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', marginBottom: 8, fontSize: 14, fontWeight: 500 }}>Buy Quantity:</label>
                      <input 
                        style={{ 
                          width: '100%', 
                          padding: '12px 16px',
                          fontSize: 16,
                          minHeight: 48,
                          borderRadius: 6,
                          border: '1px solid var(--border)',
                          background: 'var(--bg)'
                        }} 
                        type="number" 
                        value={amt} 
                        min={0} 
                        max={maxBuy} 
                        disabled={disabledTrade}
                        onChange={e=>{
                          const v = Number(e.target.value)
                          const capped = Math.max(0, Math.min(maxBuy, isNaN(v) ? 0 : v))
                          setAmountsByGood(s => ({ ...s, [g]: capped }))
                        }} 
                      />
                    </div>
                    
                    <div style={{ display: 'flex', gap: 12 }}>
                      <button 
                        disabled={disabledTrade || amt<=0} 
                        onClick={()=>buy(g, amt)} 
                        title={disabledTrade ? 'Unavailable while in transit' : (freeSlots<=0 ? 'Cargo full' : undefined)} 
                        style={{ 
                          flex: 1,
                          padding: '16px 24px',
                          fontSize: 18,
                          fontWeight: 600,
                          minHeight: 56,
                          borderRadius: 8,
                          background: 'var(--accent)',
                          color: 'white',
                          border: 'none',
                          cursor: 'pointer'
                        }}
                      >
                        Buy
                      </button>
                      <button 
                        disabled={disabledTrade || owned<=0} 
                        onClick={()=>sell(g, owned)} 
                        style={{ 
                          flex: 1,
                          padding: '16px 24px',
                          fontSize: 18,
                          fontWeight: 600,
                          minHeight: 56,
                          borderRadius: 8,
                          cursor: 'pointer',
                          ...sellStyle
                        }} 
                        title={disabledTrade ? 'Unavailable while in transit' : undefined}
                      >
                        Sell All
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          ) : (
            // Desktop table layout
            <table style={{ width:'100%', borderCollapse:'collapse' }}>
              <thead>
                <tr style={{ textAlign:'left', borderBottom:'1px solid var(--border)' }}>
                  <th style={{ padding:'6px 8px' }}>Good</th>
                  <th style={{ padding:'6px 8px' }}>Available</th>
                  <th style={{ padding:'6px 8px' }}>Price</th>
                  <th style={{ padding:'6px 8px' }}>Range</th>
                  <th style={{ padding:'6px 8px' }}>% of Max</th>
                  <th style={{ padding:'6px 8px' }}>Owned</th>
                  <th style={{ padding:'6px 8px' }}>Buy Qty</th>
                  <th style={{ padding:'6px 8px' }}>Actions</th>
                </tr>
              </thead>
              <tbody>
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
                        ? { background:'rgba(52,211,153,0.18)', color:'var(--good)', border:'1px solid rgba(52,211,153,0.35)' }
                        : price < youPaid
                          ? { background:'rgba(248,113,113,0.18)', color:'var(--bad)', border:'1px solid rgba(248,113,113,0.35)' }
                          : { background:'rgba(255,255,255,0.06)', color:'var(--text)', border:'1px solid var(--border)' })
                    : undefined
                  const disabledTrade = inTransit
                  const rangeText = range ? `${range[0]}–${range[1]}` : '—'
                  const pctText = range ? (()=>{ const max=range[1]; const pct=max>0? Math.max(0, Math.min(100, Math.round((price/max)*100))) : 0; return `${pct}%` })() : '—'
                  return (
                    <tr key={g} style={{ borderBottom:'1px solid var(--border)' }}>
                      <td style={{ padding:'6px 8px', fontWeight:700 }}>{g}</td>
                      <td style={{ padding:'6px 8px' }}>{available}</td>
                      <td style={{ padding:'6px 8px' }}>${price}</td>
                      <td style={{ padding:'6px 8px' }} className="muted">{rangeText}</td>
                      <td style={{ padding:'6px 8px' }} className="muted">{pctText}</td>
                      <td style={{ padding:'6px 8px' }}>
                        {owned}
                        {owned>0 && typeof youPaid === 'number' ? <span className="muted" style={{ marginLeft:6 }}>(avg ${youPaid})</span> : null}
                      </td>
                      <td style={{ padding:'6px 8px' }}>
                        <input style={{ width: 72 }} type="number" value={amt} min={0} max={maxBuy} disabled={disabledTrade}
                          onChange={e=>{
                            const v = Number(e.target.value)
                            const capped = Math.max(0, Math.min(maxBuy, isNaN(v) ? 0 : v))
                            setAmountsByGood(s => ({ ...s, [g]: capped }))
                          }} />
                      </td>
                      <td style={{ padding:'6px 8px', whiteSpace:'nowrap' }}>
                        <button disabled={disabledTrade || amt<=0} onClick={()=>buy(g, amt)} title={disabledTrade ? 'Unavailable while in transit' : (freeSlots<=0 ? 'Cargo full' : undefined)} style={{ marginRight:6 }}>Buy</button>
                        <button disabled={disabledTrade || owned<=0} onClick={()=>sell(g, owned)} style={sellStyle} title={disabledTrade ? 'Unavailable while in transit' : undefined}>Sell</button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    )}

    {activeTab==='graphs' && (
      <div style={{ padding: isMobile ? 12 : 16 }}>
        <h3 className="glow" style={{ fontSize: isMobile ? 18 : 'inherit' }}>Wealth Over Time</h3>
        <div style={{ overflowX: isMobile ? 'auto' : 'visible' }}>
          <WealthCharts history={wealthHistory} />
        </div>
      </div>
    )}
  </div>
    </div>
  )
}
