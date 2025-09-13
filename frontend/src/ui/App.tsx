import { useEffect, useMemo, useRef, useState } from 'react'

// Simple client that manages ws and state machine: title -> lobby -> room -> game

type LobbyRoom = { id: string; name: string; playerCount: number; started: boolean }

type RoomState = {
  room: { id: string; name: string; started: boolean; tick: number; players: any[]; planets: string[] }
  you: { id: string; name: string; money: number; inventory: Record<string, number>; inventoryAvgCost: Record<string, number>; currentPlanet: string; destinationPlanet: string }
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
    setUrl(`ws://localhost:8080/ws`)
  }
  useEffect(() => { if (ready) send('connect', { name: name || undefined }) }, [ready])

  const createRoom = () => send('createRoom')
  const joinRoom = (roomId: string) => send('joinRoom', { roomId })
  const startGame = () => send('startGame')

  const selectPlanet = (planet: string) => send('selectPlanet', { planet })
  const buy = (good: string, amount: number) => send('buy', { good, amount })
  const sell = (good: string, amount: number) => send('sell', { good, amount })

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
                {r.name} — {r.playerCount} players {r.started ? '(Started)' : ''}
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
    <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr 280px', gap: 16, padding: 16, fontFamily: 'system-ui' }}>
      <div>
        <h3>Planets</h3>
        <ul>
          {r.room.planets.map(p => {
            const onPlanet = (r.room.players as any[]).filter(pl => pl.currentPlanet === p)
            return (
              <li key={p} style={{ display:'flex', alignItems:'center', gap:8 }}>
                <button disabled={p===r.you.currentPlanet} onClick={()=>selectPlanet(p)}>{p}</button>
                <div style={{ display:'flex', gap:4 }}>
                  {onPlanet.map((pl:any) => (
                    <span key={pl.id} title={pl.name} style={{ width:14, height:14, borderRadius:7, background:'#6cf', color:'#003', display:'inline-flex', alignItems:'center', justifyContent:'center', fontSize:10 }}>
                      {String(pl.name||'P').slice(0,1).toUpperCase()}
                    </span>
                  ))}
                </div>
              </li>
            )
          })}
        </ul>
      </div>
      <div>
        <h2>Room: {r.room.name} {r.room.started ? '(Started)' : ''}</h2>
        {!r.room.started && <button onClick={startGame}>Start Game</button>}
        <div>Tick: {r.room.tick}</div>
        <h3>Players</h3>
        <ul>
          {r.room.players.map((pl:any)=> (
            <li key={pl.id}>{pl.name} — ${pl.money} — on {pl.currentPlanet}{pl.destinationPlanet?`→${pl.destinationPlanet}`:''}</li>
          ))}
        </ul>
      </div>
      <div>
        <h3>Market — {visible.name || r.you.currentPlanet}</h3>
        <div>Your money: ${r.you.money}</div>
        <ul>
          {Object.keys(goods).map(g => {
            const price = prices[g]
            const available = goods[g]
            const owned = r.you.inventory[g] || 0
            const youPaid = r.you.inventoryAvgCost?.[g]
            const maxBuy = price > 0 ? Math.min(available, Math.floor(r.you.money / price)) : 0
            const amt = (amountsByGood[g] ?? maxBuy)
            return (
              <li key={g} style={{ marginBottom: 8 }}>
                <b>{g}</b>: {available} @ ${price} {owned>0 && youPaid ? <span style={{color:'#666'}}>(you paid ${youPaid})</span> : null}
                <div style={{ display:'flex', gap: 6, alignItems:'center' }}>
                  <input style={{ width: 64 }} type="number" value={amt} min={0} max={999}
                    onChange={e=>{
                      const v = Number(e.target.value)
                      setAmountsByGood(s => ({ ...s, [g]: isNaN(v) ? 0 : v }))
                    }} />
                  <button disabled={amt<=0} onClick={()=>buy(g, amt)}>Buy</button>
                  <span>Owned: {owned}</span>
                  <button disabled={owned<=0} onClick={()=>sell(g, Math.min(owned, amt||owned))}>Sell</button>
                </div>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}
