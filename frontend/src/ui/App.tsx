import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { signInWithRedirect } from 'aws-amplify/auth'
import { createPortal } from 'react-dom'
import { useAuth } from '../contexts/AuthContext.jsx'
import LoginForm from '../components/LoginForm.jsx'
import awsConfig from '../aws-config.js'
import {
  listSingleplayerSaves,
  recordSingleplayerTurn,
  removeSingleplayerSave,
  SingleplayerSaveSummary,
} from '../utils/singleplayerSaves'

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

const shrinkFont = (size: number) => Math.max(10, size - 4)

const sanitizeAlphanumeric = (value: string) => value.replace(/[^a-zA-Z0-9]/g, '')
const sanitizeNumeric = (value: string) => value.replace(/[^0-9]/g, '')

const clampNumber = (value: number, min: number, max: number) => {
  if (Number.isNaN(value)) return min
  return Math.min(max, Math.max(min, value))
}

type CSSVarKey = `--${string}`
type CSSPropertiesWithVars = CSSProperties & Partial<Record<CSSVarKey, string | number>>

const formatRelativeTime = (timestamp: number) => {
  const diff = Date.now() - timestamp
  const minutes = Math.round(diff / 60000)
  if (minutes <= 0) return 'just now'
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  return `${days}d ago`
}

// Simple client that manages ws and state machine: title -> lobby -> room -> game

type LobbyRoom = {
  id: string
  name: string
  playerCount: number
  started: boolean
  turn?: number
  private?: boolean
  paused?: boolean
  creatorId?: string
}

type RoomPlayer = {
  id: string
  name: string
  money: number | string
  cashValue?: number
  currentPlanet: string
  destinationPlanet: string
  ready?: boolean
  endGame?: boolean
  bankrupt?: boolean
  cargoValue?: number
  upgradeValue?: number
  facilityInvestment?: number
}

type FacilitySummary = {
  id?: string
  type: string
  ownerId: string
  ownerName?: string
  usageCharge: number
  accruedMoney?: number
  purchasePrice?: number
}

type MarketSnapshot = {
  turn: number
  updatedAt?: number
  goods: Record<string, number>
  prices: Record<string, number>
  priceRanges?: Record<string, [number, number]>
  fuelPrice?: number
}

type MapViewState = {
  centerX: number
  centerY: number
  zoom: number
}

type RoomState = {
  room: {
    id: string
    name: string
    started: boolean
    turn: number
    players: RoomPlayer[]
    planets: string[]
    private?: boolean
    paused?: boolean
    creatorId?: string
    facilities?: Record<string, FacilitySummary[]>
    planetPositions?: Record<string, { x: number; y: number }>
    world?: { width: number; height: number; unitScale?: number }
    allReady?: boolean
    turnEndsAt?: number
    news?: { headline: string; planet: string; turnsRemaining: number }[]
  }
  you: {
    id: string
    name: string
    money: number
    cashValue?: number
    fuel: number
    inventory: Record<string, number>
    inventoryAvgCost: Record<string, number>
    currentPlanet: string
    destinationPlanet: string
    ready?: boolean
    endGame?: boolean
    modal?: {
      id: string
      title: string
      body: string
      kind?: string
      auctionId?: string
      facilityType?: string
      planet?: string
      usageCharge?: number
      suggestedBid?: number
    }
    inTransit?: boolean
    transitFrom?: string
    transitRemaining?: number
    transitTotal?: number
    facilityInvestment?: number
    upgradeInvestment?: number
    upgradeValue?: number
    cargoValue?: number
    marketMemory?: Record<string, MarketSnapshot>
    knownPlanets?: string[]
  }
  visiblePlanet: { name: string; goods: Record<string, number>; prices: Record<string, number>; priceRanges?: Record<string, [number, number]>; fuelPrice?: number } | {}
}

type LobbyState = { rooms: LobbyRoom[] }

type WSOut = { type: string; payload?: any }

function useWS(url: string | null) {
  // WebSocket and connection management refs/state
  const wsRef = useRef<WebSocket | null>(null)
  const heartbeatIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const heartbeatTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const shouldReconnectRef = useRef<boolean>(true)
  const reconnectAttemptsRef = useRef<number>(0)
  const lastMessageTimeRef = useRef<number>(0)
  const maxReconnectAttempts = 10

  const [ready, setReady] = useState(false)
  const [messages, setMessages] = useState<any[]>([])
  const [error, setError] = useState<string | null>(null)
  const [connectionState, setConnectionState] = useState<'connecting'|'connected'|'reconnecting'|'disconnected'>('disconnected')

  const stopHeartbeat = () => {
    if (heartbeatIntervalRef.current) {
      clearInterval(heartbeatIntervalRef.current)
      heartbeatIntervalRef.current = null
    }
    if (heartbeatTimeoutRef.current) {
      clearTimeout(heartbeatTimeoutRef.current)
      heartbeatTimeoutRef.current = null
    }
  }

  const startHeartbeat = () => {
    // Send a ping every 20s; if no pong within 10s, trigger reconnect
    stopHeartbeat()
  heartbeatIntervalRef.current = setInterval(() => {
      try {
        wsRef.current?.send(JSON.stringify({ type: 'ping' }))
  heartbeatTimeoutRef.current = setTimeout(() => {
          // If no pong resets the timeout, consider the connection dead
          try { wsRef.current?.close(4000, 'Heartbeat timeout') } catch {}
        }, 10000)
      } catch (e) {
        // Ignore send errors; onerror/onclose will handle
      }
    }, 20000)
  }

  const calculateReconnectDelay = () => {
    const base = 1000 // 1s
    const max = 15000 // 15s
    const expo = Math.min(max, base * Math.pow(2, reconnectAttemptsRef.current))
    const jitter = Math.random() * 500
    return expo + jitter
  }
  
  const connect = useCallback(() => {
    if (!url || !shouldReconnectRef.current) return
    
    if (wsRef.current?.readyState === WebSocket.CONNECTING || 
        wsRef.current?.readyState === WebSocket.OPEN) {
      return // Already connecting or connected
    }
    
  // Attempt WebSocket connection
    
    setConnectionState('connecting')
    setError(null)
    
    const ws = new WebSocket(url)
    wsRef.current = ws
    
    ws.onopen = () => {
  // WebSocket connected
      setReady(true)
      setError(null)
      setConnectionState('connected')
      reconnectAttemptsRef.current = 0 // Reset on successful connection
      lastMessageTimeRef.current = Date.now()
      startHeartbeat()
    }
    
    ws.onclose = (event) => {
  // WebSocket closed
      setReady(false)
      setConnectionState('disconnected')
      stopHeartbeat()
      
      if (shouldReconnectRef.current && event.code !== 1000) { // Not a normal closure
        if (reconnectAttemptsRef.current < maxReconnectAttempts) {
          const delay = calculateReconnectDelay()
          // Reconnecting
          
          setConnectionState('reconnecting')
          setError(`Connection lost. Reconnecting in ${Math.round(delay/1000)}s... (${reconnectAttemptsRef.current + 1}/${maxReconnectAttempts})`)
          
          reconnectTimeoutRef.current = setTimeout(() => {
            reconnectAttemptsRef.current++
            connect()
          }, delay)
        } else {
          setError('Connection failed after multiple attempts. Please refresh the page.')
          setConnectionState('disconnected')
        }
      } else if (event.code !== 1000) {
        setError(`Connection closed: ${event.reason || 'Unknown reason'}`)
      }
    }
    
    ws.onerror = (event) => {
  // WebSocket error
      if (reconnectAttemptsRef.current === 0) {
        setError('WebSocket connection failed. Check if the server is running and certificates are valid.')
      }
    }
    
    ws.onmessage = (ev) => {
      try {
        const message = JSON.parse(ev.data)
        lastMessageTimeRef.current = Date.now()
        
        // Handle pong response
        if (message.type === 'pong') {
          if (heartbeatTimeoutRef.current) {
            clearTimeout(heartbeatTimeoutRef.current)
            heartbeatTimeoutRef.current = null
          }
          return
        }
        
        setMessages(m => [...m, message])
      } catch (err) {
  // Failed to parse WebSocket message
      }
    }
  }, [url])
  
  // Initial connection and cleanup
  useEffect(() => {
    shouldReconnectRef.current = true
    connect()
    
    return () => {
      shouldReconnectRef.current = false
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }
      stopHeartbeat()
      if (wsRef.current) {
        wsRef.current.close(1000, 'Component unmounting')
        wsRef.current = null
      }
    }
  }, [connect])

  const send = useMemo(() => (type: string, payload?: any) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
  // Cannot send message - WebSocket not ready
      return false
    }
    wsRef.current.send(JSON.stringify({ type, payload }))
    return true
  }, [])

  // Manual reconnect function
  const reconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    reconnectAttemptsRef.current = 0
    shouldReconnectRef.current = true
    
    if (wsRef.current) {
      wsRef.current.close()
    }
    
    setTimeout(connect, 100)
  }, [connect])

  return { 
    ready, 
    messages, 
    send, 
    error, 
    connectionState,
    reconnect,
    isReconnecting: connectionState === 'reconnecting'
  }
}

// Connection Status Component
function ConnectionStatus({ 
  connectionState, 
  isReconnecting, 
  error, 
  reconnect, 
  isMobile 
}: { 
  connectionState: string
  isReconnecting: boolean
  error: string | null
  reconnect: () => void
  isMobile: boolean
}) {
  if (connectionState === 'connected') return null
  
  const getStatusInfo = () => {
    switch (connectionState) {
      case 'connecting':
        return { icon: '🔄', text: 'Connecting...', color: '#3b82f6' }
      case 'reconnecting':
        return { icon: '🔄', text: 'Reconnecting...', color: '#f59e0b' }
      case 'disconnected':
        return { icon: '🔴', text: 'Disconnected', color: '#ef4444' }
      default:
        return { icon: '❓', text: 'Unknown', color: '#6b7280' }
    }
  }
  
  const status = getStatusInfo()
  
  return (
    <div style={{
      position: 'fixed',
      top: isMobile ? 16 : 20,
      right: isMobile ? 16 : 20,
      background: 'rgba(0, 0, 0, 0.8)',
      backdropFilter: 'blur(10px)',
      border: `1px solid ${status.color}`,
      borderRadius: isMobile ? 12 : 8,
      padding: isMobile ? '12px 16px' : '8px 12px',
      zIndex: 9999,
      display: 'flex',
      alignItems: 'center',
  gap: 8,
  fontSize: isMobile ? `${shrinkFont(14)}px` : '12px',
      color: 'white',
      boxShadow: '0 4px 12px rgba(0, 0, 0, 0.3)'
    }}>
      <span style={{ 
        fontSize: isMobile ? `${shrinkFont(16)}px` : '14px',
        animation: isReconnecting ? 'spin 1s linear infinite' : 'none'
      }}>
        {status.icon}
      </span>
      <span style={{ color: status.color, fontWeight: 500 }}>
        {status.text}
      </span>
      {error && connectionState === 'disconnected' && (
        <button
          onClick={reconnect}
          style={{
            marginLeft: 8,
            padding: isMobile ? '6px 12px' : '4px 8px',
            fontSize: isMobile ? `${shrinkFont(12)}px` : '11px',
            background: status.color,
            border: 'none',
            borderRadius: 4,
            color: 'white',
            cursor: 'pointer',
            fontWeight: 500
          }}
        >
          Retry
        </button>
      )}
      <style>
        {`
          @keyframes spin {
            from { transform: rotate(0deg); }
            to { transform: rotate(360deg); }
          }
        `}
      </style>
    </div>
  )
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

// Calculate total wealth for a player including cash, cargo, upgrades, and facilities
function calculatePlayerWealth(player: any): { cash: number; inventoryValue: number; upgradeValue: number; facilityValue: number; total: number } {
  const cash = typeof player.money === 'number' ? player.money : (player.cashValue || 0)

  const inventoryValue = player.cargoValue != null
    ? player.cargoValue
    : (() => {
        if (player.inventory && player.inventoryAvgCost) {
          return Object.keys(player.inventory).reduce((sum, good) => {
            const quantity = player.inventory[good] || 0
            const avgCost = player.inventoryAvgCost[good] || 0
            return sum + quantity * avgCost
          }, 0)
        }
        return 0
      })()

  const upgradeValue = player.upgradeValue != null
    ? player.upgradeValue
    : player.upgradeInvestment != null
      ? player.upgradeInvestment
      : 0

  const facilityValue = player.facilityInvestment != null ? player.facilityInvestment : 0

  const total = cash + inventoryValue + upgradeValue + facilityValue

  return { cash, inventoryValue, upgradeValue, facilityValue, total }
}

// Pie Chart Component for wealth distribution
function WealthPieChart({ players, isMobile }: { players: any[], isMobile: boolean }) {
  if (!players || players.length === 0) {
    return <div>No player data available.</div>
  }

  // Calculate wealth for all players
  const playerWealth = players.map(player => {
    const wealth = calculatePlayerWealth(player)
    return {
      id: player.id,
      name: player.name,
      ...wealth,
      color: (() => {
        let h = 0
        for (let i = 0; i < player.id.length; i++) h = (h * 31 + player.id.charCodeAt(i)) % 360
        return `hsl(${h},70%,45%)`
      })()
    }
  }).filter(p => p.total > 0) // Only include players with wealth

  if (playerWealth.length === 0) {
    return <div>No wealth data available.</div>
  }

  const totalWealth = playerWealth.reduce((sum, p) => sum + p.total, 0)
  
  // Calculate pie chart segments
  let currentAngle = 0
  const segments = playerWealth.map(player => {
    const percentage = (player.total / totalWealth) * 100
    const angle = (player.total / totalWealth) * 360
    const startAngle = currentAngle
    const endAngle = currentAngle + angle
    currentAngle = endAngle

    // Calculate path for pie slice
    const centerX = 150
    const centerY = 150
    const radius = 120
    
    const startAngleRad = (startAngle * Math.PI) / 180
    const endAngleRad = (endAngle * Math.PI) / 180
    
    const x1 = centerX + radius * Math.cos(startAngleRad)
    const y1 = centerY + radius * Math.sin(startAngleRad)
    const x2 = centerX + radius * Math.cos(endAngleRad)
    const y2 = centerY + radius * Math.sin(endAngleRad)
    
    const largeArcFlag = angle > 180 ? 1 : 0
    
    const pathData = [
      `M ${centerX} ${centerY}`,
      `L ${x1} ${y1}`,
      `A ${radius} ${radius} 0 ${largeArcFlag} 1 ${x2} ${y2}`,
      'Z'
    ].join(' ')

    return {
      ...player,
      percentage,
      angle,
      pathData,
      startAngle,
      endAngle
    }
  })

  const [hoveredId, setHoveredId] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const activeId = selectedId ?? hoveredId
  const activeSegment = activeId ? segments.find(seg => seg.id === activeId) || null : null

  return (
    <div style={{ 
      display: 'flex', 
      flexDirection: isMobile ? 'column' : 'row',
      gap: isMobile ? 16 : 24,
      alignItems: isMobile ? 'center' : 'flex-start'
    }}>
      {/* Pie Chart */}
      <div style={{ flex: 'none', position: 'relative' }}>
        <svg
          width={isMobile ? 280 : 300}
          height={isMobile ? 280 : 300}
          viewBox="0 0 300 300"
          onClick={() => setSelectedId(null)}
        >
          {segments.map(segment => {
            const isActive = activeId === segment.id
            return (
              <g key={segment.id}>
                <path
                  d={segment.pathData}
                  fill={segment.color}
                  stroke="rgba(255, 255, 255, 0.1)"
                  strokeWidth="2"
                  style={{
                    filter: isActive
                      ? 'drop-shadow(0 6px 12px rgba(0, 0, 0, 0.4))'
                      : 'drop-shadow(0 2px 4px rgba(0, 0, 0, 0.2))',
                    transform: `scale(${isActive ? 1.045 : 1})`,
                    transformOrigin: '150px 150px',
                    transformBox: 'fill-box',
                    transition: 'transform 0.18s ease, filter 0.18s ease',
                    cursor: 'pointer'
                  }}
                  role="button"
                  tabIndex={0}
                  aria-label={`${segment.name || 'Player'} wealth share ${segment.percentage.toFixed(1)} percent`}
                  onMouseEnter={() => setHoveredId(segment.id)}
                  onMouseLeave={() => setHoveredId(null)}
                  onFocus={() => setHoveredId(segment.id)}
                  onBlur={() => setHoveredId(null)}
                  onClick={(e) => {
                    e.stopPropagation()
                    setSelectedId(prev => prev === segment.id ? null : segment.id)
                  }}
                  onTouchStart={(e) => {
                    e.stopPropagation()
                    setSelectedId(prev => prev === segment.id ? null : segment.id)
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setSelectedId(prev => prev === segment.id ? null : segment.id)
                    }
                  }}
                />
                {/* Percentage label */}
                {segment.percentage > 5 && (
                  <text
                    x={150 + 80 * Math.cos(((segment.startAngle + segment.endAngle) / 2) * Math.PI / 180)}
                    y={150 + 80 * Math.sin(((segment.startAngle + segment.endAngle) / 2) * Math.PI / 180)}
                    textAnchor="middle"
                    dominantBaseline="middle"
                    fill="white"
                    fontSize={isMobile ? "12" : "14"}
                    fontWeight="600"
                    style={{ textShadow: '0 1px 2px rgba(0, 0, 0, 0.8)' }}
                  >
                    {segment.percentage.toFixed(1)}%
                  </text>
                )}
              </g>
            )
          })}
          
          {/* Center title */}
          {!activeSegment && (
            <>
              <text
                x="150"
                y="145"
                textAnchor="middle"
                dominantBaseline="middle"
                fill="rgba(255, 255, 255, 0.8)"
                fontSize={isMobile ? "12" : "14"}
                fontWeight="600"
              >
                Total Wealth
              </text>
              <text
                x="150"
                y="160"
                textAnchor="middle"
                dominantBaseline="middle"
                fill="rgba(255, 255, 255, 0.6)"
                fontSize={isMobile ? "11" : "12"}
              >
                ${totalWealth.toLocaleString()}
              </text>
            </>
          )}
        </svg>
        {activeSegment && (
          <div
            style={{
              position: 'absolute',
              top: '50%',
              left: '50%',
              transform: 'translate(-50%, -50%)',
              background: 'rgba(10, 21, 42, 0.92)',
              border: '1px solid rgba(148, 163, 184, 0.45)',
              borderRadius: 10,
              padding: '10px 14px',
              minWidth: isMobile ? 130 : 160,
              textAlign: 'center',
              color: '#E0F2FE',
              boxShadow: '0 10px 26px rgba(2, 6, 23, 0.55)',
              pointerEvents: 'none'
            }}
          >
            <div style={{ fontWeight: 700, fontSize: isMobile ? '0.95rem' : '1.05rem', marginBottom: 4 }}>
              {activeSegment.name || 'Unknown Player'}
            </div>
            <div style={{ fontSize: isMobile ? '0.75rem' : '0.8rem', color: 'rgba(226, 232, 240, 0.8)' }}>
              {activeSegment.percentage.toFixed(1)}% • ${activeSegment.total.toLocaleString()}
            </div>
          </div>
        )}
      </div>

      {/* Legend and Details */}
      <div style={{ 
        flex: 1,
        minWidth: isMobile ? '100%' : 300
      }}>
        <h4 style={{
          margin: '0 0 16px 0',
          fontSize: isMobile ? '1.1rem' : '1.2rem',
          color: 'white',
          fontWeight: 600
        }}>
          Wealth Distribution
        </h4>
        
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 12
        }}>
          {[...segments]
            .sort((a, b) => b.total - a.total)
            .map((player, index) => (
            <div
              key={player.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                padding: isMobile ? 12 : 16,
                background: 'rgba(255, 255, 255, 0.05)',
                borderRadius: 8,
                border: '1px solid rgba(255, 255, 255, 0.1)'
              }}
            >
              {/* Color indicator */}
              <div
                style={{
                  width: 16,
                  height: 16,
                  borderRadius: '50%',
                  backgroundColor: player.color,
                  flexShrink: 0
                }}
              />
              
              {/* Player info */}
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  marginBottom: 4
                }}>
                  <span style={{
                    fontWeight: 600,
                    color: 'white',
                    fontSize: isMobile ? '0.9rem' : '1rem'
                  }}>
                    {index + 1}. {player.name}
                  </span>
                  <span style={{
                    fontWeight: 700,
                    color: player.color,
                    fontSize: isMobile ? '0.9rem' : '1rem'
                  }}>
                    ${player.total.toLocaleString()}
                  </span>
                </div>
                
                {/* Wealth breakdown */}
                <div style={{
                  display: 'grid',
                  gridTemplateColumns: isMobile ? '1fr 1fr' : 'repeat(4, minmax(0, 1fr))',
                  gap: 8,
                  fontSize: isMobile ? '0.75rem' : '0.8rem',
                  color: 'rgba(255, 255, 255, 0.7)'
                }}>
                  <div>💰 Cash: ${player.cash.toLocaleString()}</div>
                  <div>📦 Cargo: ${player.inventoryValue.toLocaleString()}</div>
                  {!isMobile && <div>⚡ Upgrades: ${player.upgradeValue.toLocaleString()}</div>}
                  {!isMobile && <div>🏭 Facilities: ${player.facilityValue.toLocaleString()}</div>}
                </div>

                {isMobile && (
                  <div style={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 8,
                    fontSize: '0.75rem',
                    color: 'rgba(255, 255, 255, 0.7)',
                    marginTop: 4
                  }}>
                    {player.upgradeValue > 0 && <div>⚡ Upgrades: ${player.upgradeValue.toLocaleString()}</div>}
                    {player.facilityValue > 0 && <div>🏭 Facilities: ${player.facilityValue.toLocaleString()}</div>}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
        
        {/* Summary stats */}
        <div style={{
          marginTop: 16,
          padding: isMobile ? 12 : 16,
          background: 'rgba(255, 255, 255, 0.03)',
          borderRadius: 8,
          border: '1px solid rgba(255, 255, 255, 0.1)'
        }}>
          <div style={{
            fontSize: isMobile ? '0.8rem' : '0.9rem',
            color: 'rgba(255, 255, 255, 0.8)',
            lineHeight: 1.4
          }}>
            <div><strong>Wealth Leaders:</strong> {segments[0]?.name} (${segments[0]?.total.toLocaleString()})</div>
            <div><strong>Average Wealth:</strong> ${Math.round(totalWealth / segments.length).toLocaleString()}</div>
            <div><strong>Wealth Gap:</strong> {segments.length > 1 ? 
              `${((segments[0]?.total / segments[segments.length - 1]?.total) || 1).toFixed(1)}x` : 'N/A'}</div>
          </div>
        </div>
      </div>
    </div>
  )
}

function WealthCharts({ history, players, isMobile }: { history: WealthHistory, players?: any[], isMobile: boolean }) {
  const entries = Object.entries(history.series)
  const allPoints = entries.flatMap(([, s]) => s.points)
  
  // If we have current players, show wealth pie chart
  if (players && players.length > 0) {
    return (
      <div>
        <WealthPieChart players={players} isMobile={isMobile} />
        {/* Show line chart below pie chart if we have historical data */}
        {entries.length > 0 && allPoints.length > 0 && (
          <div style={{ marginTop: 24 }}>
            <h3 style={{ 
              margin: '0 0 16px 0', 
              color: 'white',
              fontSize: isMobile ? '1.1rem' : '1.3rem'
            }}>
              Wealth History
            </h3>
            <WealthLineChart history={history} isMobile={isMobile} />
          </div>
        )}
      </div>
    )
  }
  
  // Fallback to line chart only if no current players
  if (entries.length === 0 || allPoints.length === 0) {
    return <div>No data yet. Play a few turns to see graphs.</div>
  }
  
  return <WealthLineChart history={history} isMobile={isMobile} />
}

function WealthLineChart({ history, isMobile }: { history: WealthHistory, isMobile: boolean }) {
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
  const [showLogin, setShowLogin] = useState(false)
  const [newRoomName, setNewRoomName] = useState('')
  const [singleplayerMode, setSingleplayerMode] = useState(false)
  const [lobbyNotice, setLobbyNotice] = useState<string | null>(null)
  const [singleplayerSaves, setSingleplayerSaves] = useState<SingleplayerSaveSummary[]>([])
  const pendingRestoreRef = useRef<{ summary: SingleplayerSaveSummary; sent: boolean } | null>(null)
  const restoreTargetRoomRef = useRef<string | null>(null)
  const { ready, messages, send, error, connectionState, reconnect, isReconnecting } = useWS(url)
  const { user, loading: authLoading, signOut, getAccessToken } = useAuth()
  const currentUserId = useMemo(() => user?.sub || user?.username || '', [user?.sub, user?.username])
  const nameTouchedRef = useRef(false)
  const lastUserIdRef = useRef<string | null>(null)
  
  // Debug auth state
  //

  const [lobby, setLobby] = useState<LobbyState>({ rooms: [] })
  const [room, setRoom] = useState<RoomState | null>(null)
  const [amountsByGood, setAmountsByGood] = useState<Record<string, number>>({})
  const planetsContainerRef = useRef<HTMLDivElement | null>(null)
  const planetRefs = useRef<Record<string, HTMLLIElement | null>>({})
  const [planetWorldPos, setPlanetWorldPos] = useState<Record<string, { x: number; y: number }>>({})
  const [containerSize, setContainerSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 })
  const [worldBounds, setWorldBounds] = useState<{ width: number; height: number; unitScale: number }>({ width: 1, height: 1, unitScale: 160 })
  const defaultZoomRef = useRef(4)
  const [mapView, setMapView] = useState<{ centerX: number; centerY: number; zoom: number }>({ centerX: 0, centerY: 0, zoom: defaultZoomRef.current })
  const mapViewRef = useRef(mapView)
  const scaleRef = useRef(1)
  const [isDraggingMap, setIsDraggingMap] = useState(false)
  const panStateRef = useRef<{
    active: boolean
    pointerId: number | null
    startX: number
    startY: number
    lastX: number
    lastY: number
    moved: boolean
    hasCapture: boolean
    interactiveTarget: boolean
    pointerType: string | null
    fallbackActive: boolean
    fallbackMove: ((event: PointerEvent) => void) | null
    fallbackUp: ((event: PointerEvent) => void) | null
  }>({
    active: false,
    pointerId: null,
    startX: 0,
    startY: 0,
    lastX: 0,
    lastY: 0,
    moved: false,
    hasCapture: false,
    interactiveTarget: false,
    pointerType: null,
    fallbackActive: false,
    fallbackMove: null,
    fallbackUp: null,
  })
  const touchStateRef = useRef<{
    pointers: Map<number, { x: number; y: number; clientX: number; clientY: number }>
    pinchActive: boolean
    lastDistance: number
    lastMidpoint: { x: number; y: number }
  }>({
    pointers: new Map(),
    pinchActive: false,
    lastDistance: 0,
    lastMidpoint: { x: 0, y: 0 }
  })
  const initialCenterRef = useRef<string | null>(null)
  const [now, setNow] = useState<number>(() => Date.now())
  const mapLocked = !Boolean(room?.room?.started)
  // Tabs: map | market | locations | players | ship | graphs
  const [activeTab, setActiveTab] = useState<'map'|'market'|'locations'|'players'|'ship'|'graphs'>('map')
  // Wealth history per room: per-player series of {turn, money}
  const [wealthHistory, setWealthHistory] = useState<{ roomId?: string; series: Record<string, { name: string; color: string; points: { turn: number; money: number }[] }> }>({ roomId: undefined, series: {} })
  // Local floating notifications (e.g., Dock Tax)
  const [toasts, setToasts] = useState<{ id: string; text: string; at: number }[]>([])
  const lastDockHandled = useRef<string | null>(null)

  useEffect(() => {
  defaultZoomRef.current = 4
  }, [isMobile])

  useEffect(() => {
    mapViewRef.current = mapView
  }, [mapView])

  const clampView = useCallback((state: MapViewState): MapViewState => {
    const minZoom = 0.45
    const maxZoom = 6
    const width = Math.max(worldBounds.width, 1)
    const height = Math.max(worldBounds.height, 1)
    const containerWidth = Math.max(containerSize.width, 0)
    const containerHeight = Math.max(containerSize.height, 0)
    const baseScale = containerWidth > 0 && containerHeight > 0
      ? Math.min(containerWidth / width, containerHeight / height)
      : 0
    const zoom = Math.min(Math.max(state.zoom, minZoom), maxZoom)

    let centerX = state.centerX
    let centerY = state.centerY

    if (baseScale > 0) {
      const visibleWidth = containerWidth / (baseScale * zoom)
      const visibleHeight = containerHeight / (baseScale * zoom)
      const halfW = visibleWidth / 2
      const halfH = visibleHeight / 2
      const minX = halfW
      const maxX = width - halfW
      const minY = halfH
      const maxY = height - halfH
      centerX = maxX < minX ? width / 2 : Math.min(Math.max(centerX, minX), maxX)
      centerY = maxY < minY ? height / 2 : Math.min(Math.max(centerY, minY), maxY)
    } else {
      centerX = Math.min(Math.max(centerX, 0), width)
      centerY = Math.min(Math.max(centerY, 0), height)
    }

    return { centerX, centerY, zoom }
  }, [containerSize.height, containerSize.width, worldBounds.height, worldBounds.width])

  const updateMapView = useCallback((updater: (prev: MapViewState) => MapViewState) => {
    setMapView(prev => {
      const next = clampView(updater(prev))
      if (next.centerX === prev.centerX && next.centerY === prev.centerY && next.zoom === prev.zoom) {
        mapViewRef.current = prev
        return prev
      }
      mapViewRef.current = next
      return next
    })
  }, [clampView])

  useEffect(() => {
    setMapView(prev => {
      const clamped = clampView(prev)
      if (clamped.centerX === prev.centerX && clamped.centerY === prev.centerY && clamped.zoom === prev.zoom) {
        mapViewRef.current = prev
        return prev
      }
      mapViewRef.current = clamped
      return clamped
    })
  }, [clampView])

  const baseScale = useMemo(() => {
    if (containerSize.width <= 0 || containerSize.height <= 0) return 0
    return Math.min(
      containerSize.width / Math.max(worldBounds.width, 1),
      containerSize.height / Math.max(worldBounds.height, 1)
    )
  }, [containerSize.height, containerSize.width, worldBounds.height, worldBounds.width])

  const effectiveScale = useMemo(() => {
    const scale = baseScale * mapView.zoom
    return scale > 0 ? scale : 1
  }, [baseScale, mapView.zoom])

  useEffect(() => {
    scaleRef.current = effectiveScale
  }, [effectiveScale])

  const locationIconScale = useMemo(() => {
    const rawScale = Math.pow(Math.max(mapView.zoom, 0.01), 0.45)
    const minScale = isMobile ? 0.8 : 0.7
    return clampNumber(rawScale, minScale, 2.4)
  }, [isMobile, mapView.zoom])

  const basePlanetIconDiameter = useMemo(() => {
    if (isMobile) {
      const mobileScale = 0.8
      const base = Math.round(58 * mobileScale)
      return Math.max(28, Math.round(base * 0.75))
    }
    return 68
  }, [isMobile])

  const zoomButtonSize = useMemo(() => (isMobile ? 44 : 48), [isMobile])
  const zoomButtonFontSize = useMemo(() => (isMobile ? 22 : 24), [isMobile])

  const worldToScreen = useCallback((pos?: { x: number; y: number }) => {
    if (!pos) return undefined
    if (containerSize.width <= 0 || containerSize.height <= 0) return undefined
    const x = containerSize.width / 2 + (pos.x - mapView.centerX) * effectiveScale
    const y = containerSize.height / 2 + (pos.y - mapView.centerY) * effectiveScale
    return { x, y }
  }, [containerSize.height, containerSize.width, effectiveScale, mapView.centerX, mapView.centerY])

  const screenToWorld = useCallback((screenX: number, screenY: number, zoomOverride?: number) => {
    if (containerSize.width <= 0 || containerSize.height <= 0 || baseScale <= 0) {
      return { x: mapViewRef.current.centerX, y: mapViewRef.current.centerY }
    }
    const zoom = zoomOverride ?? mapViewRef.current.zoom
    const scale = baseScale * zoom
    if (scale <= 0) {
      return { x: mapViewRef.current.centerX, y: mapViewRef.current.centerY }
    }
    return {
      x: mapViewRef.current.centerX + (screenX - containerSize.width / 2) / scale,
      y: mapViewRef.current.centerY + (screenY - containerSize.height / 2) / scale,
    }
  }, [baseScale, containerSize.height, containerSize.width])

  const planetScreenPos = useMemo(() => {
    const result: Record<string, { x: number; y: number }> = {}
    const planetOrder = Array.isArray(room?.room?.planets)
      ? (room?.room?.planets as string[])
      : Object.keys(planetWorldPos)

    planetOrder.forEach(name => {
      const pos = planetWorldPos[name]
      if (!pos) return
      const screen = worldToScreen(pos)
      if (!screen) return
      result[name] = screen
    })

    return result
  }, [planetWorldPos, room?.room?.planets, worldToScreen])

  const getWorldPosition = useCallback((name?: string) => {
    if (!name) return undefined
    return planetWorldPos[name]
  }, [planetWorldPos])

  useEffect(() => {
    if (stage !== 'room') {
      initialCenterRef.current = null
      return
    }
    if (!room) return
    const key = `${room.room.id}:${room.you.id}`
    const current = getWorldPosition(room.you.currentPlanet)
    if (!current) return
    if (initialCenterRef.current !== key) {
      initialCenterRef.current = key
      updateMapView(() => ({ centerX: current.x, centerY: current.y, zoom: defaultZoomRef.current }))
    }
  }, [getWorldPosition, room, stage, updateMapView])

  const zoomAtPoint = useCallback((pointerX: number, pointerY: number, factor: number) => {
    if (!Number.isFinite(factor) || factor === 0) {
      return
    }
    updateMapView(prev => {
      if (baseScale <= 0 || containerSize.width <= 0 || containerSize.height <= 0) {
        return {
          centerX: prev.centerX,
          centerY: prev.centerY,
          zoom: prev.zoom * factor,
        }
      }
      const prevScale = baseScale * prev.zoom
      if (prevScale <= 0) {
        return {
          centerX: prev.centerX,
          centerY: prev.centerY,
          zoom: prev.zoom * factor,
        }
      }
      const nextZoom = prev.zoom * factor
      const nextScale = baseScale * nextZoom
      if (nextScale <= 0) {
        return {
          centerX: prev.centerX,
          centerY: prev.centerY,
          zoom: nextZoom,
        }
      }
      const offsetX = pointerX - containerSize.width / 2
      const offsetY = pointerY - containerSize.height / 2
      const worldX = prev.centerX + offsetX / prevScale
      const worldY = prev.centerY + offsetY / prevScale
      const centerX = worldX - offsetX / nextScale
      const centerY = worldY - offsetY / nextScale
      return { centerX, centerY, zoom: nextZoom }
    })
  }, [baseScale, containerSize.height, containerSize.width, updateMapView])

  const handleWheel = useCallback((event: React.WheelEvent<HTMLDivElement>) => {
    if (mapLocked) return
    const container = planetsContainerRef.current
    if (!container) return
    event.preventDefault()
    const rect = container.getBoundingClientRect()
    const pointerX = event.clientX - rect.left
    const pointerY = event.clientY - rect.top
    const clampedDelta = Math.max(-250, Math.min(250, event.deltaY))
    const factor = Math.exp(-clampedDelta * 0.0012)
    zoomAtPoint(pointerX, pointerY, factor)
  }, [mapLocked, zoomAtPoint])

  const handleZoomButton = useCallback((direction: 'in' | 'out') => {
    if (mapLocked) return
    const factor = direction === 'in' ? 1.2 : 1 / 1.2
    const pointerX = containerSize.width / 2
    const pointerY = containerSize.height / 2
    zoomAtPoint(pointerX, pointerY, factor)
  }, [containerSize.height, containerSize.width, mapLocked, zoomAtPoint])

  const endPan = useCallback((pointerId: number) => {
    const container = planetsContainerRef.current
    const previousState = { ...panStateRef.current }
    if (previousState.fallbackActive) {
      if (previousState.fallbackMove) {
        window.removeEventListener('pointermove', previousState.fallbackMove)
      }
      if (previousState.fallbackUp) {
        window.removeEventListener('pointerup', previousState.fallbackUp)
        window.removeEventListener('pointercancel', previousState.fallbackUp)
      }
    }
    if (previousState.hasCapture && container && container.hasPointerCapture(pointerId)) {
      try {
        container.releasePointerCapture(pointerId)
      } catch {}
    }
    panStateRef.current = {
      active: false,
      pointerId: null,
      startX: 0,
      startY: 0,
      lastX: 0,
      lastY: 0,
      moved: false,
      hasCapture: false,
      interactiveTarget: false,
      pointerType: null,
      fallbackActive: false,
      fallbackMove: null,
      fallbackUp: null,
    }
    setIsDraggingMap(false)
    return previousState
  }, [])

  const handlePointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (mapLocked) return
    const container = planetsContainerRef.current
    if (!container) return

    if (event.pointerType === 'touch') {
      const rect = container.getBoundingClientRect()
      const touchState = touchStateRef.current
      touchState.pointers.set(event.pointerId, {
        clientX: event.clientX,
        clientY: event.clientY,
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      })
      if (touchState.pointers.size === 2) {
        const values = Array.from(touchState.pointers.values())
        const dx = values[0].x - values[1].x
        const dy = values[0].y - values[1].y
        const distance = Math.hypot(dx, dy)
        if (distance > 0) {
          if (panStateRef.current.active && panStateRef.current.pointerId != null) {
            endPan(panStateRef.current.pointerId)
          }
          touchState.pinchActive = true
          touchState.lastDistance = distance
          touchState.lastMidpoint = {
            x: (values[0].x + values[1].x) / 2,
            y: (values[0].y + values[1].y) / 2,
          }
        }
      }
      if (touchState.pinchActive) {
        return
      }
    }

    if (event.pointerType !== 'touch' && event.button !== 0) return
    const target = event.target as HTMLElement | null
    const clickable = target?.closest<HTMLElement>('button, a, [role="button"], input, textarea, select, [data-interactive="true"]')
    const isDisabledButton = clickable instanceof HTMLButtonElement && clickable.disabled
    panStateRef.current = {
      active: true,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      lastX: event.clientX,
      lastY: event.clientY,
      moved: false,
      hasCapture: false,
      interactiveTarget: Boolean(clickable && !isDisabledButton),
      pointerType: event.pointerType || (isMobile ? 'touch' : 'mouse'),
      fallbackActive: false,
      fallbackMove: null,
      fallbackUp: null,
    }
  }, [endPan, isMobile, mapLocked])

  const handlePointerMove = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const container = planetsContainerRef.current
    if (event.pointerType === 'touch' && container) {
      const rect = container.getBoundingClientRect()
      const touchState = touchStateRef.current
      touchState.pointers.set(event.pointerId, {
        clientX: event.clientX,
        clientY: event.clientY,
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      })
      if (touchState.pinchActive && touchState.pointers.size >= 2) {
        const iterator = touchState.pointers.values()
        const first = iterator.next().value
        const second = iterator.next().value
        if (first && second) {
          const dx = first.x - second.x
          const dy = first.y - second.y
          const distance = Math.hypot(dx, dy)
          const midpoint = {
            x: (first.x + second.x) / 2,
            y: (first.y + second.y) / 2,
          }
          if (distance > 0 && touchState.lastDistance > 0) {
            const rawFactor = distance / touchState.lastDistance
            const factor = clampNumber(rawFactor, 0.85, 1.18)
            if (Number.isFinite(factor) && factor > 0 && factor !== 1) {
              zoomAtPoint(midpoint.x, midpoint.y, factor)
            }
          }
          if (distance > 0) {
            touchState.lastDistance = distance
            touchState.lastMidpoint = midpoint
          }
        }
        event.preventDefault()
        return
      }
    }

    const state = panStateRef.current
    if (!state.active || state.pointerId !== event.pointerId) return
    const base = baseScale
    if (base <= 0) return
    if (state.fallbackActive && !state.hasCapture) {
      if (container && event.currentTarget === container) {
        return
      }
    }
    const pointerType = state.pointerType || event.pointerType || 'mouse'
    const scale = base * mapViewRef.current.zoom
    const dx = event.clientX - state.lastX
    const dy = event.clientY - state.lastY
    state.lastX = event.clientX
    state.lastY = event.clientY

    if (!state.moved) {
      const total = Math.hypot(event.clientX - state.startX, event.clientY - state.startY)
      const threshold = state.interactiveTarget ? (isMobile ? 20 : 12) : 4
      if (total > threshold) {
        state.moved = true
        if (container && !state.hasCapture) {
          try {
            container.setPointerCapture(event.pointerId)
            state.hasCapture = true
          } catch {}
        }
        if (pointerType === 'touch' && !state.hasCapture && !state.fallbackActive) {
          const moveListener = (nativeEvent: PointerEvent) => {
            if (nativeEvent.pointerId !== event.pointerId) return
            handlePointerMove(nativeEvent as unknown as React.PointerEvent<HTMLDivElement>)
          }
          const upListener = (nativeEvent: PointerEvent) => {
            if (nativeEvent.pointerId !== event.pointerId) return
            endPan(nativeEvent.pointerId)
          }
          window.addEventListener('pointermove', moveListener, { passive: false })
          window.addEventListener('pointerup', upListener)
          window.addEventListener('pointercancel', upListener)
          state.fallbackMove = moveListener
          state.fallbackUp = upListener
          state.fallbackActive = true
        }
        setIsDraggingMap(true)
      }
    }

    if (state.moved && scale > 0) {
      if (pointerType === 'touch') {
        event.preventDefault()
      }
      const dxWorld = dx / scale
      const dyWorld = dy / scale
      updateMapView(prev => ({
        centerX: prev.centerX - dxWorld,
        centerY: prev.centerY - dyWorld,
        zoom: prev.zoom,
      }))
    }
  }, [baseScale, endPan, isMobile, updateMapView, zoomAtPoint])

  const handlePointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'touch') {
      const touchState = touchStateRef.current
      touchState.pointers.delete(event.pointerId)
      if (touchState.pinchActive) {
        if (touchState.pointers.size >= 2) {
          const iterator = touchState.pointers.values()
          const first = iterator.next().value
          const second = iterator.next().value
          if (first && second) {
            const dx = first.x - second.x
            const dy = first.y - second.y
            touchState.lastDistance = Math.hypot(dx, dy)
            touchState.lastMidpoint = {
              x: (first.x + second.x) / 2,
              y: (first.y + second.y) / 2,
            }
          }
        } else {
          touchState.pinchActive = false
          touchState.lastDistance = 0
        }
      }
    }
    const state = panStateRef.current
    if (!state.active || state.pointerId !== event.pointerId) return
    endPan(event.pointerId)
  }, [endPan])

  const handlePointerLeave = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'touch') {
      const touchState = touchStateRef.current
      touchState.pointers.delete(event.pointerId)
      if (touchState.pinchActive && touchState.pointers.size < 2) {
        touchState.pinchActive = false
        touchState.lastDistance = 0
      }
    }
    const state = panStateRef.current
    if (!state.active || state.pointerId !== event.pointerId) return
    const pointerType = state.pointerType || event.pointerType || 'mouse'
    if (pointerType === 'touch') return
    endPan(event.pointerId)
  }, [endPan])

  useEffect(() => {
    return () => {
      const state = panStateRef.current
      if (state.pointerId != null) {
        endPan(state.pointerId)
      }
    }
  }, [endPan])

  useEffect(() => {
    const root = document.documentElement
    const previous = root.style.fontSize
    if (isMobile) {
      root.style.fontSize = '12px'
    } else {
      root.style.fontSize = previous || ''
    }
    return () => {
      root.style.fontSize = previous
    }
  }, [isMobile])

  // Set name from authenticated user once per login, without overriding manual edits
  useEffect(() => {
    if (!user) {
      lastUserIdRef.current = null
      nameTouchedRef.current = false
      setName('')
      return
    }

    const userId = user.username || user.name || (user as any)?.sub || ''
    if (userId && userId !== lastUserIdRef.current) {
      lastUserIdRef.current = userId
      nameTouchedRef.current = false
    }

    if (!nameTouchedRef.current) {
      const suggested = user.name || user.username || ''
      if (suggested) {
        setName(prev => (prev ? prev : suggested))
      }
    }
  }, [user])

  // Tick local time for countdown
  useEffect(() => {
    const i = setInterval(() => setNow(Date.now()), 250)
    return () => clearInterval(i)
  }, [])

  useEffect(() => {
    if (stage !== 'lobby') return
    if (!currentUserId) {
      setSingleplayerSaves([])
      return
    }
    const saves = listSingleplayerSaves(currentUserId)
    setSingleplayerSaves(saves)
  }, [stage, currentUserId])

  useEffect(() => {
    const last = messages[messages.length-1]
    if (!last) return
    if (last.type === 'lobbyState') {
      setLobby(last.payload)
      setStage('lobby')
    }
    if (last.type === 'roomState') {
      const payload = last.payload
      setRoom(payload)
      setStage('room')
      setLobbyNotice(null)

      if (
        pendingRestoreRef.current &&
        !pendingRestoreRef.current.sent &&
        currentUserId &&
        payload?.room?.private &&
        payload?.room?.creatorId === currentUserId &&
        payload?.room?.id
      ) {
        const pending = pendingRestoreRef.current
        const sent = send('restoreSingleplayer', {
          roomId: payload.room.id,
          save: pending.summary.encoded,
          ownerId: pending.summary.ownerId,
          lastTurn: pending.summary.lastTurn,
        })
        if (sent) {
          pendingRestoreRef.current = { summary: pending.summary, sent: true }
          restoreTargetRoomRef.current = payload.room.id
        } else {
          setLobbyNotice('Unable to transmit saved mission data. Please check your connection and try again.')
          setSingleplayerSaves(prev => {
            const exists = prev.some(save => save.roomId === pending.summary.roomId)
            if (exists) return prev
            return [pending.summary, ...prev].sort((a, b) => b.updatedAt - a.updatedAt)
          })
          pendingRestoreRef.current = null
          restoreTargetRoomRef.current = null
        }
      }

      if (
        !pendingRestoreRef.current &&
        currentUserId &&
        payload?.room?.private &&
        payload?.room?.creatorId === currentUserId &&
        payload?.room?.id
      ) {
        const summary = recordSingleplayerTurn({
          roomId: payload.room.id,
          roomName: payload.room.name,
          ownerId: currentUserId,
          turn: typeof payload.room.turn === 'number' ? payload.room.turn : 0,
          state: {
            room: payload.room,
            you: payload.you,
            visiblePlanet: payload.visiblePlanet,
          },
          authoritativeState: payload.singleplayerState,
        })
        if (summary) {
          setSingleplayerSaves(prev => {
            const filtered = prev.filter(save => save.roomId !== summary.roomId)
            return [summary, ...filtered].sort((a, b) => b.updatedAt - a.updatedAt)
          })
        }
      }
    }
    if (last.type === 'joinDenied') {
      const reason = last.payload?.message || last.payload?.reason || 'Unable to join room.'
      setLobbyNotice(reason)
      setStage('lobby')
    }
    if (last.type === 'restoreAck') {
      const success = Boolean(last.payload?.success)
      const roomId = last.payload?.roomId as string | undefined
      const message = last.payload?.message as string | undefined
      const pending = pendingRestoreRef.current
      if (restoreTargetRoomRef.current && roomId && roomId !== restoreTargetRoomRef.current) {
        return
      }

      if (success) {
        if (pending) {
          removeSingleplayerSave(pending.summary.roomId)
        }
        pendingRestoreRef.current = null
        restoreTargetRoomRef.current = null
        if (message) {
          setLobbyNotice(message)
        } else {
          setLobbyNotice(null)
        }
      } else {
        if (message) {
          setLobbyNotice(message)
        } else {
          setLobbyNotice('Failed to restore saved mission. You can start a new mission instead.')
        }
        if (pending) {
          setSingleplayerSaves(prev => {
            const exists = prev.some(save => save.roomId === pending.summary.roomId)
            if (exists) return prev
            return [pending.summary, ...prev].sort((a, b) => b.updatedAt - a.updatedAt)
          })
        }
        pendingRestoreRef.current = null
        restoreTargetRoomRef.current = null
      }
    }
  }, [messages, currentUserId, send])

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

  useEffect(() => {
    if (!room) return
    const hasStarted = Boolean(room.room?.started)
    if (!hasStarted && activeTab === 'market') {
      setActiveTab('map')
    }
  }, [room?.room?.started, activeTab])

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

  // Close Ship Inventory menu on outside click

  // Actions
  const onConnect = async () => {
    if (!user) {
      setShowLogin(true)
      return
    }

    try {
      const token = await getAccessToken()
      if (!token) {
        setShowLogin(true)
        return
      }

      const wsUrl = (() => {
        const host = typeof window !== 'undefined' ? window.location.hostname : 'localhost'
        const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:'
        // Prefer site’s own WS endpoint through Nginx to the backend
        return isHttps ? `wss://${host}/ws` : `ws://${host}:8080/ws`
      })()
      
      // Append token as query parameter for WebSocket authentication
      const wsUrlWithAuth = `${wsUrl}?token=${encodeURIComponent(token)}`
      setUrl(wsUrlWithAuth)
    } catch (error) {
  // Failed to get access token
      setShowLogin(true)
    }
  }
  useEffect(() => { if (ready) send('connect', { name: name || undefined }) }, [ready])

  // Handle room URL parameter for shared links
  useEffect(() => {
    if (!ready || stage !== 'lobby') return
    
    const urlParams = new URLSearchParams(window.location.search)
    const roomId = urlParams.get('room')
    
    if (roomId) {
      // Clear the URL parameter after getting it
      const newUrl = new URL(window.location.href)
      newUrl.searchParams.delete('room')
      window.history.replaceState({}, document.title, newUrl.toString())
      
      // Join the specified room
      joinRoom(roomId)
    }
  }, [ready, stage])

  // While in the lobby, periodically refresh the room list so new rooms show up
  useEffect(() => {
    if (!ready || stage !== 'lobby') return
    send('listRooms')
    const t = setInterval(() => send('listRooms'), 3000)
    return () => clearInterval(t)
  }, [ready, stage])

  const createRoom = useCallback(() => {
    const sanitizedName = sanitizeAlphanumeric(newRoomName)
    const payload: Record<string, any> = {}
    if (sanitizedName) {
      payload.name = sanitizedName
    }
    if (singleplayerMode) {
      payload.singleplayer = true
    }
    const ok = send('createRoom', Object.keys(payload).length ? payload : undefined)
    if (ok) {
      setNewRoomName('')
    }
  }, [newRoomName, singleplayerMode, send])
  const joinRoom = useCallback((roomId: string) => send('joinRoom', { roomId }), [send])
  const handleContinueSave = useCallback((save: SingleplayerSaveSummary) => {
    const activeRoom = lobby.rooms.find(r => r.id === save.roomId)
    if (activeRoom) {
      joinRoom(save.roomId)
      setLobbyNotice(null)
      return
    }

    pendingRestoreRef.current = { summary: save, sent: false }

    const sanitizedName = sanitizeAlphanumeric(save.roomName || '')
    const desiredName = sanitizedName || 'Solo Mission'
    const payload: Record<string, any> = { singleplayer: true }
    if (sanitizedName) {
      payload.name = sanitizedName
    }

    const created = send('createRoom', payload)
    if (!created) {
      pendingRestoreRef.current = null
      setLobbyNotice('Unable to relaunch singleplayer mission. Please check your connection and try again.')
      return
    }

    setSingleplayerMode(true)
    setNewRoomName('')
    setLobbyNotice(`Relaunching singleplayer mission "${desiredName}"...`)
    setSingleplayerSaves(prev => prev.filter(entry => entry.roomId !== save.roomId))
  }, [
    joinRoom,
    lobby.rooms,
    send,
    setLobbyNotice,
    setNewRoomName,
    setSingleplayerMode,
    setSingleplayerSaves,
  ])
  const handleDeleteSave = useCallback((roomId: string) => {
    removeSingleplayerSave(roomId)
    setSingleplayerSaves(prev => prev.filter(save => save.roomId !== roomId))
  }, [])
  const startGame = () => send('startGame')
  const addBot = () => send('addBot')
  const exitRoom = () => send('exitRoom')
  const setEndGame = (end: boolean) => send('setEndGame', { endGame: end })

  const selectPlanet = (planet: string) => send('selectPlanet', { planet })
  const buy = (good: string, amount: number) => send('buy', { good, amount })
  const sell = (good: string, amount: number) => send('sell', { good, amount })
  const ackModal = (id?: string) => send('ackModal', { id })
  const refuel = (amount?: number) => send('refuel', { amount: amount ?? 0 })
  const handleCommanderNameChange = useCallback((value: string) => {
    nameTouchedRef.current = true
    setName(sanitizeAlphanumeric(value))
  }, [])
  const handleRoomNameChange = useCallback((value: string) => {
    setNewRoomName(sanitizeAlphanumeric(value))
  }, [])
  
  // Auction bid state
  const [auctionBid, setAuctionBid] = useState<string>('')
  const handleAuctionBidChange = useCallback((value: string) => {
    setAuctionBid(sanitizeNumeric(value))
  }, [])
  const submitAuctionBid = () => {
    const modal = (room?.you as any)?.modal
    if (!modal || modal.kind !== 'auction') return
    
    const bid = parseInt(auctionBid)
    if (isNaN(bid) || bid <= 0) return
    
    send('auctionBid', { auctionId: modal.auctionId, bid })
    setAuctionBid('')
    ackModal(modal.id)
  }
  
  // Reset auction bid when modal changes
  useEffect(() => {
    const modal = (room?.you as any)?.modal
    if (modal?.kind === 'auction' && modal.suggestedBid) {
      setAuctionBid(modal.suggestedBid.toString())
    } else {
      setAuctionBid('')
    }
  }, [room?.you?.modal?.id])
  
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
    if (container) {
      const rect = container.getBoundingClientRect()
      setContainerSize({ width: rect.width, height: rect.height })
    }
    const world = ((room.room as any).world ?? {}) as { width?: number; height?: number; unitScale?: number }
    const width = Math.max(1, world.width ?? 5200)
    const height = Math.max(1, world.height ?? 5200)
    const unitScale = Math.max(0.1, world.unitScale ?? 160)
    setWorldBounds({ width, height, unitScale })

    const names = room.room.planets
    const count = Math.max(1, names.length)
    const fallbackNorm: Record<string, { x: number; y: number }> = {}
    names.forEach((name, i) => {
      const angle = (i / count) * Math.PI * 2
      let h = 0
      for (let k = 0; k < name.length; k++) h = (h * 31 + name.charCodeAt(k)) >>> 0
      const jitter = ((h % 1000) / 1000 - 0.5) * 0.08
      const radius = 0.42 + (((h >> 4) % 1000) / 1000 - 0.5) * 0.06
      const x = Math.min(0.92, Math.max(0.08, 0.5 + (radius + jitter) * Math.cos(angle)))
      const y = Math.min(0.92, Math.max(0.08, 0.5 + (radius - jitter) * Math.sin(angle)))
      fallbackNorm[name] = { x, y }
    })

    const serverPos = (room.room as any).planetPositions as Record<string, { x: number; y: number }> | undefined
    const next: Record<string, { x: number; y: number }> = {}
    for (const name of names) {
      const pos = serverPos?.[name]
      if (pos && typeof pos.x === 'number' && typeof pos.y === 'number') {
        next[name] = { x: pos.x, y: pos.y }
      } else {
        const fallback = fallbackNorm[name]
        next[name] = { x: fallback.x * width, y: fallback.y * height }
      }
    }
    setPlanetWorldPos(next)
  }, [room?.room.planets, room?.room.world, room?.room.id, stage])

  // Recompute on resize
  useEffect(() => {
    const onResize = () => {
      const container = planetsContainerRef.current
      if (!container) return
      const rect = container.getBoundingClientRect()
      setContainerSize({ width: rect.width, height: rect.height })
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [stage])

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

  const stationKeywords = useMemo(() => [
    'station',
    'outpost',
    'base',
    'port',
    'dock',
    'hub',
    'colony',
    'depot'
  ], [])

  const moonKeywords = useMemo(() => [
    'moon',
    'luna',
    'selene',
    'selena',
    'io',
    'ganymede',
    'callisto',
    'titania',
    'rhea',
    'dione',
    'mimas',
    'triton',
    'phoebe',
    'satellite',
    'orbiter',
    'minor'
  ], [])

  const hashString = useCallback((value: string) => {
    let hash = 0
    for (let i = 0; i < value.length; i++) {
      hash = (hash * 131 + value.charCodeAt(i)) >>> 0
    }
    return hash >>> 0
  }, [])

  const classifyLocation = useCallback((name: string) => {
    const lower = name.toLowerCase()
    if (stationKeywords.some(keyword => lower.includes(keyword))) return 'station' as const
    if (moonKeywords.some(keyword => lower.includes(keyword))) return 'moon' as const
    return 'planet' as const
  }, [moonKeywords, stationKeywords])

  const renderPlanetIcon = useCallback((name: string, size: number, shouldAnimate = true) => {
    const hash = hashString(name.toLowerCase()) || 1
    let state = hash
    const rng = () => {
      state = (state * 1664525 + 1013904223) >>> 0
      return state / 4294967295
    }

    const baseHue = Math.round(rng() * 360)
    const oceanHue = (baseHue + 360) % 360
    const oceanSat = 50 + Math.round(rng() * 22)
    const oceanLight = 34 + Math.round(rng() * 14)
    const oceanColor = `hsl(${oceanHue}, ${oceanSat}%, ${oceanLight}%)`
    const atmosphere = `radial-gradient(circle at 35% 30%, hsla(${(oceanHue + 20) % 360}, ${60 + Math.round(rng() * 15)}%, ${78 + Math.round(rng() * 10)}%, 0.9) 0%, hsla(${(oceanHue + 340) % 360}, ${40 + Math.round(rng() * 15)}%, ${55 + Math.round(rng() * 8)}%, 0.75) 45%, rgba(2,6,23,0.35) 100%)`
    const surface = `radial-gradient(circle at 62% 68%, ${oceanColor} 0%, hsl(${(oceanHue + 25) % 360}, ${Math.min(85, oceanSat + 18)}%, ${Math.max(18, oceanLight - 12)}%) 72%, hsl(${(oceanHue + 16) % 360}, ${Math.min(88, oceanSat + 22)}%, ${Math.max(10, oceanLight - 20)}%) 100%)`

    const bandColor = (offset: number, saturationAdjust: number, lightAdjust: number, alpha: number) => {
      const hue = (oceanHue + offset + 360) % 360
      const saturation = clampNumber(oceanSat + saturationAdjust, 25, 92)
      const lightness = clampNumber(oceanLight + lightAdjust, 18, 96)
      return `hsla(${hue}, ${saturation}%, ${lightness}%, ${alpha})`
    }

    const primaryBands = [
      bandColor(8, 24, 28, 0.95),
      bandColor(-16, -8, -18, 0.9),
      bandColor(22, 18, 18, 0.88),
      'hsla(0, 0%, 100%, 0.62)'
    ]
    const secondaryBands = [
      bandColor(10, 20, 26, 0.9),
      bandColor(-22, -10, -24, 0.86),
      bandColor(36, 14, 16, 0.84),
      'hsla(200, 60%, 86%, 0.52)'
    ]

    const swirlSpeedPrimary = 36 + Math.round(rng() * 14)
    const swirlSpeedSecondary = swirlSpeedPrimary + 16 + Math.round(rng() * 12)
    const swirlDelayPrimary = -Math.round(rng() * swirlSpeedPrimary * 10) / 10
    const swirlDelaySecondary = -Math.round(rng() * swirlSpeedSecondary * 10) / 10

  const streakOpacity = 0.34 + rng() * 0.18
    const streakSpeed = swirlSpeedPrimary * 0.82 + rng() * 6
    const streakDelay = -Math.round(rng() * streakSpeed * 10) / 10
    const streakHighlight = (0.36 + rng() * 0.12).toFixed(2)
    const streakFade = (0.08 + rng() * 0.08).toFixed(2)

    const cloudMask = 'radial-gradient(ellipse at center, rgba(255,255,255,1) 38%, rgba(255,255,255,0.85) 60%, rgba(255,255,255,0.45) 78%, rgba(255,255,255,0) 96%)'

    const primaryStripes = `repeating-linear-gradient(180deg,
      ${primaryBands[0]} 0%,
      ${primaryBands[0]} 14%,
      ${primaryBands[1]} 14%,
      ${primaryBands[1]} 28%,
      ${primaryBands[2]} 28%,
      ${primaryBands[2]} 42%,
      ${primaryBands[3]} 42%,
      ${primaryBands[3]} 56%)`

    const primaryGlare = `linear-gradient(90deg,
      transparent 0%,
      rgba(255,255,255,0.16) 26%,
      rgba(255,255,255,0.28) 48%,
      rgba(255,255,255,0.16) 70%,
      transparent 100%)`

    const secondaryStripes = `repeating-linear-gradient(180deg,
      ${secondaryBands[0]} 0%,
      ${secondaryBands[0]} 16%,
      ${secondaryBands[1]} 16%,
      ${secondaryBands[1]} 32%,
      ${secondaryBands[2]} 32%,
      ${secondaryBands[2]} 48%,
      ${secondaryBands[3]} 48%,
      ${secondaryBands[3]} 62%)`

    const secondaryGlare = `linear-gradient(90deg,
      transparent 0%,
      rgba(255,255,255,0.12) 18%,
      rgba(255,255,255,0.22) 38%,
      rgba(255,255,255,0.12) 58%,
      transparent 100%)`

    const streakBands = `repeating-linear-gradient(180deg,
      rgba(255,255,255,0.24) 0%,
      rgba(255,255,255,0.24) 18%,
      rgba(255,255,255,0.08) 18%,
      rgba(255,255,255,0.08) 36%)`

    const streakGlare = `linear-gradient(90deg,
      transparent 0%,
      rgba(255,255,255,${streakFade}) 20%,
      rgba(255,255,255,${streakHighlight}) 50%,
      rgba(255,255,255,${streakFade}) 80%,
      transparent 100%)`

    const primarySwirlStyle: CSSPropertiesWithVars = {
      position: 'absolute',
      inset: '-8%',
      borderRadius: '50%',
      backgroundImage: `${primaryGlare}, ${primaryStripes}`,
      backgroundRepeat: 'repeat, repeat',
      backgroundSize: '220% 100%, 100% 140%',
      backgroundPosition: '0% 50%, 50% 50%',
      maskImage: cloudMask,
      WebkitMaskImage: cloudMask,
  opacity: 0.9,
  mixBlendMode: 'overlay',
      pointerEvents: 'none',
  filter: 'blur(2.2px)',
      animation: `planet-band-drift ${swirlSpeedPrimary}s linear infinite`,
      animationDelay: `${swirlDelayPrimary}s`,
      animationPlayState: shouldAnimate ? 'running' : 'paused'
    }

    const secondarySwirlStyle: CSSPropertiesWithVars = {
      position: 'absolute',
      inset: '-9%',
      borderRadius: '50%',
      backgroundImage: `${secondaryGlare}, ${secondaryStripes}`,
      backgroundRepeat: 'repeat, repeat',
      backgroundSize: '200% 100%, 100% 130%',
  backgroundPosition: '0% 50%, 50% 50%',
      maskImage: cloudMask,
      WebkitMaskImage: cloudMask,
  opacity: 0.76,
  mixBlendMode: 'overlay',
      pointerEvents: 'none',
  filter: 'blur(3px)',
      animation: `planet-band-drift-reverse ${swirlSpeedSecondary}s linear infinite`,
      animationDelay: `${swirlDelaySecondary}s`,
      animationPlayState: shouldAnimate ? 'running' : 'paused'
    }

    const streakSwirlStyle: CSSPropertiesWithVars = {
      position: 'absolute',
      inset: '-10%',
      borderRadius: '50%',
      backgroundImage: `${streakGlare}, ${streakBands}`,
      backgroundRepeat: 'repeat, repeat',
      backgroundSize: '200% 100%, 100% 120%',
  backgroundPosition: '0% 52%, 50% 50%',
      maskImage: cloudMask,
      WebkitMaskImage: cloudMask,
      opacity: streakOpacity,
  mixBlendMode: 'screen',
      pointerEvents: 'none',
      filter: 'blur(6px) saturate(140%)',
      animation: `planet-band-drift ${streakSpeed}s ease-in-out infinite`,
      animationDirection: 'alternate',
      animationDelay: `${streakDelay}s`,
      animationPlayState: shouldAnimate ? 'running' : 'paused'
    }

    return (
      <div
        style={{
          width: size,
          height: size,
          borderRadius: '50%',
          position: 'relative',
          background: surface,
          boxShadow: '0 0 18px rgba(59,130,246,0.45), inset -8px -12px 20px rgba(8,16,37,0.55)',
          overflow: 'hidden'
        }}
      >
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            background: atmosphere,
            mixBlendMode: 'screen',
            opacity: 0.68
          }}
        />
        <div
          aria-hidden
          className="planet-swirl-layer"
          style={primarySwirlStyle}
        />
        <div
          aria-hidden
          className="planet-swirl-layer planet-swirl-layer--reverse"
          style={secondarySwirlStyle}
        />
        <div
          aria-hidden
          className="planet-swirl-layer planet-swirl-layer--streaks"
          style={streakSwirlStyle}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            background: 'radial-gradient(circle at 28% 28%, rgba(255,255,255,0.55) 0%, rgba(255,255,255,0.08) 42%, rgba(255,255,255,0) 56%)'
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            boxShadow: 'inset -6px -10px 16px rgba(2,6,23,0.4)'
          }}
        />
      </div>
    )
  }, [hashString])

  const renderMoonIcon = useCallback((name: string, size: number) => {
    const hash = hashString(name.toLowerCase()) || 1
    let state = hash
    const rng = () => {
      state = (state * 22695477 + 1) >>> 0
      return state / 4294967295
    }

    const baseLight = 52 + Math.round(rng() * 16)
    const surface = `radial-gradient(circle at 34% 32%, hsl(220, 12%, ${Math.min(95, baseLight + 18)}%) 0%, hsl(220, 10%, ${baseLight}%) 45%, hsl(220, 16%, ${Math.max(18, baseLight - 24)}%) 100%)`
    const craterCount = 4 + Math.floor(rng() * 3)
    const craters = Array.from({ length: craterCount }).map(() => {
      const diameter = size * (0.14 + rng() * 0.22)
      const cx = size * (0.18 + rng() * 0.64)
      const cy = size * (0.2 + rng() * 0.62)
      return {
        diameter,
        left: cx - diameter / 2,
        top: cy - diameter / 2,
        depth: 0.35 + rng() * 0.4
      }
    })

    return (
      <div
        style={{
          width: size,
          height: size,
          borderRadius: '50%',
          position: 'relative',
          background: surface,
          boxShadow: '0 0 16px rgba(148,163,184,0.45), inset -6px -10px 18px rgba(15,23,42,0.55)'
        }}
      >
        {craters.map((crater, idx) => (
          <span
            key={idx}
            style={{
              position: 'absolute',
              left: crater.left,
              top: crater.top,
              width: crater.diameter,
              height: crater.diameter,
              borderRadius: '50%',
              background: 'radial-gradient(circle at 35% 35%, rgba(255,255,255,0.55) 0%, rgba(226,232,240,0.35) 45%, rgba(148,163,184,0.65) 100%)',
              boxShadow: `inset ${Math.round(crater.diameter * 0.08)}px ${Math.round(crater.diameter * 0.12)}px ${Math.round(crater.diameter * 0.18)}px rgba(15,23,42,${0.25 + crater.depth * 0.22})`,
              opacity: 0.82
            }}
          />
        ))}
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            background: 'radial-gradient(circle at 30% 28%, rgba(255,255,255,0.45) 0%, rgba(255,255,255,0.1) 40%, rgba(255,255,255,0) 55%)'
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            boxShadow: 'inset -5px -10px 16px rgba(15,23,42,0.55)'
          }}
        />
      </div>
    )
  }, [hashString])

  const renderStationIcon = useCallback((name: string, size: number, shouldAnimate = true) => {
    const hash = hashString(name.toLowerCase()) || 1
    let state = hash
    const rng = () => {
      state = (state * 1103515245 + 12345) >>> 0
      return state / 4294967295
    }

    const baseHue = 200 + Math.round(rng() * 80)
    const glowColor = `hsla(${baseHue}, 90%, ${50 + Math.round(rng() * 12)}%, 0.45)`
    const ringColor = `hsla(${(baseHue + 20) % 360}, 72%, ${46 + Math.round(rng() * 10)}%, 0.9)`
    const coreLight = `linear-gradient(135deg, hsla(${(baseHue + 12) % 360}, 85%, 78%, 0.92) 0%, hsla(${(baseHue + 340) % 360}, 82%, 64%, 0.95) 100%)`
    const strutColor = `linear-gradient(180deg, hsla(${(baseHue + 350) % 360}, 65%, 72%, 0.92), hsla(${(baseHue + 320) % 360}, 65%, 58%, 0.88))`

    const podCount = 4
    const pods = Array.from({ length: podCount }).map((_, idx) => {
      const angle = (idx / podCount) * Math.PI * 2 + rng() * 0.2
      const podSize = size * (0.22 + rng() * 0.08)
      const orbitRadius = size * 0.48
      return {
        size: podSize,
        left: size / 2 + Math.cos(angle) * orbitRadius - podSize / 2,
        top: size / 2 + Math.sin(angle) * orbitRadius - podSize / 2
      }
    })

    const ringSpinSpeed = 12 + Math.round(rng() * 6)
    const ringDelay = -Math.round(rng() * ringSpinSpeed * 10) / 10
    const ringArcPrimary = `hsla(${(baseHue + 32) % 360}, ${78 + Math.round(rng() * 12)}%, ${66 + Math.round(rng() * 10)}%, 0.92)`
    const ringArcSecondary = `hsla(${(baseHue + 300) % 360}, ${74 + Math.round(rng() * 14)}%, ${62 + Math.round(rng() * 10)}%, 0.78)`
    const ringArcTertiary = `hsla(${(baseHue + 240) % 360}, ${68 + Math.round(rng() * 12)}%, ${68 + Math.round(rng() * 8)}%, 0.85)`
    const ringStartAngle = Math.round(rng() * 360)
    const rotatingRingGradient = `conic-gradient(from ${ringStartAngle}deg, transparent 0deg, ${ringArcPrimary} 45deg, transparent 110deg, ${ringArcSecondary} 170deg, transparent 235deg, ${ringArcTertiary} 290deg, transparent 360deg)`
    const ringInnerPercent = 40 + Math.round(rng() * 4)
    const ringOuterPercent = ringInnerPercent + 10 + Math.round(rng() * 4)
    const ringMask = `radial-gradient(circle, transparent ${Math.max(0, ringInnerPercent - 6)}%, rgba(0,0,0,0.98) ${ringInnerPercent}%, rgba(0,0,0,0.98) ${ringOuterPercent}%, transparent ${Math.min(100, ringOuterPercent + 8)}%)`
    const ringGlowOpacity = 0.55 + rng() * 0.25
  const rotatingRingStyle: CSSPropertiesWithVars = {
      position: 'absolute',
      inset: size * 0.22,
      borderRadius: '50%',
      background: rotatingRingGradient,
      opacity: ringGlowOpacity,
      mixBlendMode: 'screen',
      pointerEvents: 'none',
      '--station-ring-speed': `${ringSpinSpeed}s`,
      animationDelay: `${ringDelay}s`,
      maskImage: ringMask,
      WebkitMaskImage: ringMask,
      maskSize: 'cover',
      WebkitMaskSize: 'cover',
      filter: 'drop-shadow(0 0 14px rgba(94,234,212,0.45))',
      animationPlayState: shouldAnimate ? 'running' : 'paused'
    }

  const rotationContainerStyle: CSSPropertiesWithVars = {
      position: 'absolute',
      inset: 0,
      '--station-ring-speed': `${ringSpinSpeed}s`,
      animationDelay: `${ringDelay}s`,
      animationPlayState: shouldAnimate ? 'running' : 'paused'
    }

    return (
      <div
        style={{
          width: size,
          height: size,
          borderRadius: '50%',
          position: 'relative',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center'
        }}
      >
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: size * 0.08 * -0.2,
            borderRadius: '50%',
            background: `radial-gradient(circle, ${glowColor} 0%, rgba(8,47,73,0.35) 55%, rgba(2,6,23,0) 80%)`,
            filter: 'blur(2px)'
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: size * 0.08,
            borderRadius: '50%',
            background: `radial-gradient(circle, rgba(15,23,42,0.9) 0%, rgba(2,6,23,0.65) 70%, rgba(2,6,23,0) 100%)`,
            border: `1px solid hsla(${baseHue}, 62%, 55%, 0.45)`
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            inset: size * 0.18,
            borderRadius: '50%',
            background: coreLight,
            boxShadow: '0 0 24px rgba(190,242,255,0.45)'
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            width: size * 0.2,
            height: size * 0.6,
            borderRadius: size * 0.12,
            background: strutColor,
            boxShadow: '0 0 16px rgba(59,130,246,0.38)'
          }}
        />
        <div
          aria-hidden
          style={{
            position: 'absolute',
            width: size * 0.6,
            height: size * 0.2,
            borderRadius: size * 0.12,
            background: strutColor,
            boxShadow: '0 0 16px rgba(59,130,246,0.38)'
          }}
        />
        <div aria-hidden className="station-rotator" style={rotationContainerStyle}>
          <div
            aria-hidden
            style={{
              position: 'absolute',
              inset: size * 0.32,
              borderRadius: '50%',
              background: 'radial-gradient(circle, rgba(2,6,23,0.92) 0%, rgba(15,23,42,0.95) 60%, rgba(15,23,42,0.2) 100%)',
              border: `1px dashed hsla(${baseHue}, 60%, 78%, 0.35)`
            }}
          />
          <div aria-hidden className="station-ring station-ring--glow" style={rotatingRingStyle} />
          <div
            aria-hidden
            style={{
              position: 'absolute',
              inset: size * 0.22,
              borderRadius: '50%',
              border: `2px solid ${ringColor}`,
              boxShadow: '0 0 20px rgba(94,234,212,0.35)'
            }}
          />
          {pods.map((pod, idx) => (
            <span
              key={idx}
              style={{
                position: 'absolute',
                left: pod.left,
                top: pod.top,
                width: pod.size,
                height: pod.size,
                borderRadius: '50%',
                background: `radial-gradient(circle, rgba(226, 232, 240, 0.92) 0%, hsla(${baseHue}, 70%, 75%, 0.78) 55%, hsla(${baseHue}, 68%, 58%, 0.62) 100%)`,
                border: `1px solid hsla(${baseHue}, 58%, 85%, 0.5)`,
                boxShadow: '0 0 12px rgba(148, 197, 255, 0.45)'
              }}
            />
          ))}
        </div>
      </div>
    )
  }, [hashString])

  const facilitiesByPlanet = useMemo(() => {
      const facilityMap = room?.room?.facilities as Record<string, FacilitySummary[] | FacilitySummary | undefined> | undefined
      if (!facilityMap) return {}
      const normalized: Record<string, FacilitySummary[]> = {}
      Object.entries(facilityMap).forEach(([planet, value]) => {
        if (!value) return
        if (Array.isArray(value)) {
          normalized[planet] = (value as FacilitySummary[]).filter(Boolean)
        } else {
          normalized[planet] = [value as FacilitySummary]
        }
      })
      return normalized
    }, [room?.room?.facilities])

  // UI
  if (stage === 'title') {
    return (
      <div style={{ 
        minHeight: '100vh',
        background: 'linear-gradient(135deg, #0f1419 0%, #1a202c 50%, #2d3748 100%)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        padding: isMobile ? 20 : 40,
        position: 'relative',
        overflow: 'hidden'
      }}>
        {/* Animated background stars */}
        <div style={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundImage: starfieldUrl ? `url(${starfieldUrl})` : 'none',
          backgroundSize: 'cover',
          opacity: 0.3,
          animation: 'twinkle 4s ease-in-out infinite alternate'
        }} />
        
        {/* Main content container */}
        <div style={{
          position: 'relative',
          zIndex: 1,
          background: 'rgba(255, 255, 255, 0.02)',
          backdropFilter: 'blur(10px)',
          border: '1px solid rgba(255, 255, 255, 0.1)',
          borderRadius: isMobile ? 16 : 24,
          padding: isMobile ? 32 : 48,
          maxWidth: isMobile ? '90vw' : 600,
          width: '100%',
          textAlign: 'center',
          boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)'
        }}>
          {/* Game Logo/Title */}
          <div style={{ marginBottom: isMobile ? 24 : 32 }}>
            <h1 className="glow" style={{ 
              fontSize: isMobile ? '3rem' : '4rem',
              fontWeight: 700,
              margin: 0,
              background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
              WebkitBackgroundClip: 'text',
              WebkitTextFillColor: 'transparent',
              textShadow: 'none'
            }}>
              Space Trader
            </h1>
            <p style={{
              fontSize: isMobile ? '1.1rem' : '1.3rem',
              color: 'rgba(255, 255, 255, 0.7)',
              margin: '8px 0 0 0',
              fontWeight: 300
            }}>
              Navigate the galaxy. Trade goods. Build your fortune.
            </p>
          </div>

          {/* Connection Form */}
          <div style={{
            background: 'rgba(255, 255, 255, 0.05)',
            borderRadius: isMobile ? 12 : 16,
            padding: isMobile ? 24 : 32,
            marginBottom: 24
          }}>
            {user ? (
              // Authenticated user interface
              <div>
                <div style={{ 
                  marginBottom: 20,
                  padding: 16,
                  background: 'rgba(76, 175, 80, 0.1)',
                  border: '1px solid rgba(76, 175, 80, 0.3)',
                  borderRadius: 8,
                  color: '#4caf50'
                }}>
                  <div style={{ fontWeight: 600, marginBottom: 4 }}>
                    ✅ Authenticated as {user.name || user.username}
                  </div>
                  <div style={{ fontSize: '0.9em', opacity: 0.8 }}>
                    {user.email}
                  </div>
                </div>
                
                <div style={{ 
                  display: 'flex', 
                  flexDirection: isMobile ? 'column' : 'row',
                  gap: isMobile ? 16 : 12,
                  alignItems: isMobile ? 'stretch' : 'center'
                }}>
                  <input 
                    placeholder="Enter your commander name" 
                    value={name} 
                    onChange={e=>handleCommanderNameChange(e.target.value)}
                    style={{
                      padding: isMobile ? '16px 20px' : '12px 16px',
                      fontSize: isMobile ? `${shrinkFont(18)}px` : '16px',
                      flex: 1,
                      border: '2px solid rgba(255, 255, 255, 0.1)',
                      borderRadius: isMobile ? 12 : 8,
                      background: 'rgba(255, 255, 255, 0.05)',
                      color: 'white',
                      outline: 'none',
                      transition: 'all 0.3s ease'
                    }}
                    onFocus={(e) => {
                      e.target.style.borderColor = 'rgba(102, 126, 234, 0.5)'
                      e.target.style.boxShadow = '0 0 0 3px rgba(102, 126, 234, 0.1)'
                    }}
                    onBlur={(e) => {
                      e.target.style.borderColor = 'rgba(255, 255, 255, 0.1)'
                      e.target.style.boxShadow = 'none'
                    }}
                  />
                  <button 
                    onClick={onConnect} 
                    disabled={!name.trim() || authLoading}
                    style={{ 
                      padding: isMobile ? '16px 32px' : '12px 24px',
                      fontSize: isMobile ? `${shrinkFont(18)}px` : '16px',
                      fontWeight: 600,
                      minHeight: isMobile ? '56px' : '48px',
                      minWidth: isMobile ? 'auto' : 120,
                      background: name.trim() && !authLoading
                        ? 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)' 
                        : 'rgba(255, 255, 255, 0.1)',
                      border: 'none',
                      borderRadius: isMobile ? 12 : 8,
                      color: 'white',
                      cursor: name.trim() && !authLoading ? 'pointer' : 'not-allowed',
                      transition: 'all 0.3s ease',
                      opacity: name.trim() && !authLoading ? 1 : 0.5
                    }}
                    onMouseEnter={(e) => {
                      if (name.trim() && !authLoading) {
                        e.currentTarget.style.transform = 'translateY(-2px)'
                        e.currentTarget.style.boxShadow = '0 10px 25px rgba(102, 126, 234, 0.3)'
                      }
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.transform = 'translateY(0)'
                      e.currentTarget.style.boxShadow = 'none'
                    }}
                  >
                    {authLoading ? 'Loading...' : 'Launch Mission'}
                  </button>
                </div>
                
                <div style={{ marginTop: 16 }}>
                  <button
                    onClick={signOut}
                    style={{
                      padding: '8px 16px',
                      fontSize: '14px',
                      background: 'transparent',
                      border: '1px solid rgba(255, 255, 255, 0.2)',
                      borderRadius: 6,
                      color: 'rgba(255, 255, 255, 0.7)',
                      cursor: 'pointer',
                      transition: 'all 0.2s ease'
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.backgroundColor = 'rgba(255, 255, 255, 0.1)'
                      e.currentTarget.style.color = 'white'
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.backgroundColor = 'transparent'
                      e.currentTarget.style.color = 'rgba(255, 255, 255, 0.7)'
                    }}
                  >
                    Sign Out
                  </button>
                </div>
              </div>
            ) : (
              // Non-authenticated interface
              <div>
                <div style={{ 
                  marginBottom: 20,
                  padding: 16,
                  background: 'rgba(255, 193, 7, 0.1)',
                  border: '1px solid rgba(255, 193, 7, 0.3)',
                  borderRadius: 8,
                  color: '#ffc107'
                }}>
                  <div style={{ fontWeight: 600, marginBottom: 4 }}>
                    🔐 Authentication Required
                  </div>
                  <div style={{ fontSize: '0.9em', opacity: 0.8 }}>
                    Please sign in to access the space trading simulation
                  </div>
                </div>
                
                <button 
          onClick={async () => {
                    // Hosted UI redirect
                    try {
            await signInWithRedirect()
                    } catch (e) {
                      // Hosted UI redirect failed
                    }
                  }}
                  disabled={authLoading}
                  style={{ 
                    padding: isMobile ? '16px 32px' : '12px 24px',
                    fontSize: isMobile ? `${shrinkFont(18)}px` : '16px',
                    fontWeight: 600,
                    minHeight: isMobile ? '56px' : '48px',
                    width: '100%',
                    background: !authLoading
                      ? 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)' 
                      : 'rgba(255, 255, 255, 0.1)',
                    border: 'none',
                    borderRadius: isMobile ? 12 : 8,
                    color: 'white',
                    cursor: !authLoading ? 'pointer' : 'not-allowed',
                    transition: 'all 0.3s ease',
                    opacity: !authLoading ? 1 : 0.5
                  }}
                  onMouseEnter={(e) => {
                    if (!authLoading) {
                      e.currentTarget.style.transform = 'translateY(-2px)'
                      e.currentTarget.style.boxShadow = '0 10px 25px rgba(102, 126, 234, 0.3)'
                    }
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.transform = 'translateY(0)'
                    e.currentTarget.style.boxShadow = 'none'
                  }}
                >
                  {authLoading ? 'Loading...' : 'Sign In / Sign Up'}
                </button>
              </div>
            )}
          </div>

          {/* Development Mode Warning */}
          {(import.meta as any).env?.VITE_DEV_MODE === 'true' && (
            <div style={{ 
              marginBottom: 24, 
              padding: isMobile ? 16 : 20, 
              background: 'rgba(68, 68, 255, 0.1)',
              border: '1px solid rgba(68, 68, 255, 0.3)',
              borderRadius: isMobile ? 12 : 16,
              fontSize: isMobile ? `${shrinkFont(14)}px` : '15px',
              lineHeight: 1.5,
              textAlign: 'left'
            }}>
              <div style={{ 
                fontWeight: 600, 
                color: '#60a5fa', 
                marginBottom: 8,
                display: 'flex',
                alignItems: 'center',
                gap: 8
              }}>
                <span style={{ fontSize: '1.2em' }}>⚠️</span>
                Development Mode
              </div>
              <div style={{ color: 'rgba(255, 255, 255, 0.8)' }}>
                If you encounter certificate warnings, visit{' '}
                <a 
                  href="https://localhost:8443/rooms" 
                  target="_blank" 
                  style={{ 
                    color: '#60a5fa',
                    textDecoration: 'none',
                    borderBottom: '1px solid rgba(96, 165, 250, 0.3)'
                  }}
                >
                  https://localhost:8443/rooms
                </a>{' '}
                and accept the self-signed certificate.
              </div>
            </div>
          )}

          {/* Error Display */}
          {error && (
            <div style={{ 
              marginBottom: 24,
              padding: isMobile ? 16 : 20, 
              background: 'rgba(248, 113, 113, 0.1)',
              border: '1px solid rgba(248, 113, 113, 0.3)',
              borderRadius: isMobile ? 12 : 16,
              fontSize: isMobile ? `${shrinkFont(14)}px` : '15px'
            }}>
              <div style={{ 
                fontWeight: 600, 
                color: '#f87171', 
                marginBottom: 8,
                display: 'flex',
                alignItems: 'center',
                gap: 8
              }}>
                <span style={{ fontSize: '1.2em' }}>🚫</span>
                Connection Failed
              </div>
              <div style={{ color: 'rgba(255, 255, 255, 0.8)' }}>
                {error}
              </div>
            </div>
          )}

          {/* Connection Info */}
          {url && (
            <div style={{ 
              fontSize: isMobile ? `${shrinkFont(12)}px` : '13px', 
              color: 'rgba(255, 255, 255, 0.4)',
              fontFamily: 'monospace',
              wordBreak: 'break-all',
              background: 'rgba(0, 0, 0, 0.2)',
              padding: isMobile ? 8 : 12,
              borderRadius: 8,
              border: '1px solid rgba(255, 255, 255, 0.1)'
            }}>
              {/* Endpoint hidden in production */}
            </div>
          )}
        </div>

        {/* Floating elements for visual appeal */}
        <div style={{
          position: 'absolute',
          top: '10%',
          left: '10%',
          width: 100,
          height: 100,
          background: 'radial-gradient(circle, rgba(102, 126, 234, 0.1) 0%, transparent 70%)',
          borderRadius: '50%',
          animation: 'float 6s ease-in-out infinite'
        }} />
        <div style={{
          position: 'absolute',
          bottom: '20%',
          right: '15%',
          width: 150,
          height: 150,
          background: 'radial-gradient(circle, rgba(118, 75, 162, 0.1) 0%, transparent 70%)',
          borderRadius: '50%',
          animation: 'float 8s ease-in-out infinite reverse'
        }} />

        <style>
          {`
            @keyframes twinkle {
              0% { opacity: 0.3; }
              100% { opacity: 0.6; }
            }
            @keyframes float {
              0%, 100% { transform: translateY(0px); }
              50% { transform: translateY(-20px); }
            }
          `}
        </style>

        {/* Connection Status */}
        <ConnectionStatus 
          connectionState={connectionState}
          isReconnecting={isReconnecting}
          error={error}
          reconnect={reconnect}
          isMobile={isMobile}
        />
      </div>
    )
  }

  if (stage === 'lobby') {
    return (
      <div style={{ 
        minHeight: '100vh',
        background: 'linear-gradient(135deg, #0f1419 0%, #1a202c 50%, #2d3748 100%)',
        padding: isMobile ? 20 : 40,
        position: 'relative',
        overflow: 'hidden'
      }}>
        {/* Background stars */}
        <div style={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundImage: starfieldUrl ? `url(${starfieldUrl})` : 'none',
          backgroundSize: 'cover',
          opacity: 0.2
        }} />

        {/* Main content */}
        <div style={{
          position: 'relative',
          zIndex: 1,
          maxWidth: isMobile ? '100%' : 1200,
          margin: '0 auto'
        }}>
          {/* Header */}
          <div style={{
            textAlign: 'center',
            marginBottom: isMobile ? 32 : 48
          }}>
            <h1 className="glow" style={{ 
              fontSize: isMobile ? '2.5rem' : '3.5rem',
              fontWeight: 700,
              margin: '0 0 8px 0',
              background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
              WebkitBackgroundClip: 'text',
              WebkitTextFillColor: 'transparent'
            }}>
              Mission Control
            </h1>
            <p style={{
              fontSize: isMobile ? '1.1rem' : '1.3rem',
              color: 'rgba(255, 255, 255, 0.7)',
              margin: 0,
              fontWeight: 300
            }}>
              Choose your mission or create a new trading expedition
            </p>
          </div>

          {lobbyNotice && (
            <div style={{
              marginBottom: isMobile ? 16 : 24,
              padding: isMobile ? '12px 16px' : '14px 20px',
              borderRadius: isMobile ? 12 : 14,
              border: '1px solid rgba(248, 113, 113, 0.35)',
              background: 'rgba(248, 113, 113, 0.12)',
              color: 'white',
              display: 'flex',
              alignItems: isMobile ? 'flex-start' : 'center',
              justifyContent: 'space-between',
              gap: 12,
              flexDirection: isMobile ? 'column' : 'row'
            }}>
              <span style={{ fontSize: isMobile ? '0.95rem' : '1rem', lineHeight: 1.4 }}>{lobbyNotice}</span>
              <button
                onClick={() => setLobbyNotice(null)}
                style={{
                  padding: isMobile ? '8px 14px' : '6px 12px',
                  fontSize: isMobile ? '0.85rem' : '0.8rem',
                  fontWeight: 600,
                  borderRadius: 999,
                  border: '1px solid rgba(248, 113, 113, 0.5)',
                  background: 'transparent',
                  color: 'rgba(255,255,255,0.85)',
                  cursor: 'pointer'
                }}
              >
                Dismiss
              </button>
            </div>
          )}

          {/* Action Cards Container */}
          <div style={{
            display: 'grid',
            gridTemplateColumns: isMobile ? '1fr' : '1fr 2fr',
            gap: isMobile ? 24 : 32,
            marginBottom: 32
          }}>
            {/* Create New Game Card */}
            <div style={{
              background: 'rgba(255, 255, 255, 0.02)',
              backdropFilter: 'blur(10px)',
              border: '1px solid rgba(255, 255, 255, 0.1)',
              borderRadius: isMobile ? 16 : 20,
              padding: isMobile ? 24 : 32,
              textAlign: 'center',
              boxShadow: '0 20px 40px -12px rgba(0, 0, 0, 0.3)'
            }}>
              <div style={{
                fontSize: isMobile ? '3rem' : '4rem',
                marginBottom: 16,
                opacity: 0.8
              }}>
                🚀
              </div>
              <h3 style={{
                fontSize: isMobile ? '1.3rem' : '1.5rem',
                margin: '0 0 12px 0',
                color: 'white',
                fontWeight: 600
              }}>
                Start New Mission
              </h3>
              <p style={{
                color: 'rgba(255, 255, 255, 0.6)',
                fontSize: isMobile ? '0.9rem' : '1rem',
                margin: '0 0 24px 0',
                lineHeight: 1.5
              }}>
                Launch a new trading expedition and invite other commanders to join your crew.
              </p>
              <div style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 10,
                marginBottom: 16,
                textAlign: 'left'
              }}>
                <label htmlFor="room-name" style={{
                  fontSize: isMobile ? '0.85rem' : '0.9rem',
                  textTransform: 'uppercase',
                  letterSpacing: 0.6,
                  color: 'rgba(255, 255, 255, 0.65)',
                  fontWeight: 600
                }}>
                  Room Name
                </label>
                <input
                  id="room-name"
                  placeholder="E.g. Galactic Express"
                  value={newRoomName}
                  onChange={e => handleRoomNameChange(e.target.value)}
                  style={{
                    padding: isMobile ? '14px 16px' : '12px 14px',
                    fontSize: isMobile ? '1rem' : '0.95rem',
                    borderRadius: 10,
                    border: '1px solid rgba(255, 255, 255, 0.15)',
                    background: 'rgba(255, 255, 255, 0.06)',
                    color: 'white',
                    outline: 'none'
                  }}
                  onFocus={e => {
                    e.currentTarget.style.borderColor = 'rgba(167, 139, 250, 0.6)'
                    e.currentTarget.style.boxShadow = '0 0 0 3px rgba(167, 139, 250, 0.2)'
                  }}
                  onBlur={e => {
                    e.currentTarget.style.borderColor = 'rgba(255, 255, 255, 0.15)'
                    e.currentTarget.style.boxShadow = 'none'
                  }}
                />
              </div>
              <label style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                justifyContent: 'flex-start',
                marginBottom: 20,
                color: 'rgba(255, 255, 255, 0.7)',
                fontSize: isMobile ? '0.95rem' : '0.9rem'
              }}>
                <input
                  type="checkbox"
                  checked={singleplayerMode}
                  onChange={e => setSingleplayerMode(e.target.checked)}
                  style={{ width: 18, height: 18 }}
                />
                <span>
                  Singleplayer — make room private and pause when you step away
                </span>
              </label>
              <button 
                onClick={createRoom}
                style={{ 
                  padding: isMobile ? '16px 32px' : '14px 28px',
                  fontSize: isMobile ? '18px' : '16px',
                  fontWeight: 600,
                  minHeight: isMobile ? '56px' : '48px',
                  width: '100%',
                  background: 'linear-gradient(135deg, #10b981 0%, #059669 100%)',
                  border: 'none',
                  borderRadius: isMobile ? 12 : 10,
                  color: 'white',
                  cursor: 'pointer',
                  transition: 'all 0.3s ease'
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.transform = 'translateY(-2px)'
                  e.currentTarget.style.boxShadow = '0 10px 25px rgba(16, 185, 129, 0.3)'
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.transform = 'translateY(0)'
                  e.currentTarget.style.boxShadow = 'none'
                }}
              >
                Create New Game
              </button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: isMobile ? 24 : 32 }}>
              {singleplayerSaves.length > 0 && (
                <div style={{
                  background: 'rgba(255, 255, 255, 0.02)',
                  backdropFilter: 'blur(10px)',
                  border: '1px solid rgba(255, 255, 255, 0.1)',
                  borderRadius: isMobile ? 16 : 20,
                  padding: isMobile ? 24 : 28,
                  boxShadow: '0 20px 40px -12px rgba(0, 0, 0, 0.3)'
                }}>
                  <div style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: isMobile ? 'flex-start' : 'center',
                    flexDirection: isMobile ? 'column' : 'row',
                    gap: isMobile ? 12 : 16,
                    marginBottom: isMobile ? 20 : 24
                  }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <span style={{ fontSize: isMobile ? '1.5rem' : '2rem' }}>🧭</span>
                      <h3 style={{
                        fontSize: isMobile ? '1.25rem' : '1.4rem',
                        margin: 0,
                        color: 'white',
                        fontWeight: 600
                      }}>
                        Continue Singleplayer
                      </h3>
                    </div>
                    <span style={{
                      fontSize: isMobile ? '0.8rem' : '0.85rem',
                      color: 'rgba(255, 255, 255, 0.55)'
                    }}>
                      Auto-saved each turn
                    </span>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: isMobile ? 12 : 16 }}>
                    {singleplayerSaves.map(save => {
                      const isActive = lobby.rooms.some(r => r.id === save.roomId)
                      return (
                        <div
                          key={save.roomId}
                          style={{
                            display: 'flex',
                            flexDirection: isMobile ? 'column' : 'row',
                            alignItems: isMobile ? 'flex-start' : 'center',
                            justifyContent: 'space-between',
                            gap: isMobile ? 12 : 16,
                            padding: isMobile ? '14px 16px' : '16px 20px',
                            borderRadius: isMobile ? 12 : 14,
                            background: 'rgba(255, 255, 255, 0.05)',
                            border: '1px solid rgba(255, 255, 255, 0.08)'
                          }}
                        >
                          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 6 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                              <span style={{
                                fontSize: isMobile ? '1.05rem' : '1.1rem',
                                fontWeight: 600,
                                color: 'white'
                              }}>
                                {save.roomName || 'Unnamed Mission'}
                              </span>
                              <span style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: 6,
                                fontSize: isMobile ? '0.75rem' : '0.75rem',
                                color: isActive ? '#34d399' : '#fbbf24'
                              }}>
                                <span style={{
                                  width: 8,
                                  height: 8,
                                  borderRadius: 4,
                                  background: isActive ? '#34d399' : '#fbbf24'
                                }} />
                                {isActive ? 'Room live' : 'Room offline'}
                              </span>
                            </div>
                            <div style={{
                              display: 'flex',
                              flexWrap: 'wrap',
                              gap: 8,
                              fontSize: isMobile ? '0.85rem' : '0.85rem',
                              color: 'rgba(255, 255, 255, 0.65)'
                            }}>
                              <span>Turn {save.lastTurn}</span>
                              <span>•</span>
                              <span>{formatRelativeTime(save.updatedAt)}</span>
                              <span>•</span>
                              <span>{save.turnCount} snapshot{save.turnCount === 1 ? '' : 's'}</span>
                            </div>
                          </div>
                          <div style={{
                            display: 'flex',
                            flexDirection: isMobile ? 'column' : 'row',
                            gap: 8,
                            width: isMobile ? '100%' : 'auto'
                          }}>
                            <button
                              onClick={() => handleContinueSave(save)}
                              style={{
                                padding: isMobile ? '10px 16px' : '10px 18px',
                                fontSize: isMobile ? '0.9rem' : '0.85rem',
                                borderRadius: 999,
                                border: 'none',
                                background: isActive
                                  ? 'linear-gradient(135deg, #34d399 0%, #059669 100%)'
                                  : 'linear-gradient(135deg, #60a5fa 0%, #3b82f6 100%)',
                                color: 'white',
                                cursor: 'pointer',
                                fontWeight: 600,
                                width: isMobile ? '100%' : undefined
                              }}
                            >
                              {isActive ? 'Continue' : 'Rejoin'}
                            </button>
                            <button
                              onClick={() => handleDeleteSave(save.roomId)}
                              style={{
                                padding: isMobile ? '10px 16px' : '10px 18px',
                                fontSize: isMobile ? '0.9rem' : '0.85rem',
                                borderRadius: 999,
                                border: '1px solid rgba(255, 255, 255, 0.25)',
                                background: 'transparent',
                                color: 'rgba(255, 255, 255, 0.75)',
                                cursor: 'pointer',
                                fontWeight: 600,
                                width: isMobile ? '100%' : undefined
                              }}
                            >
                              Delete
                            </button>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}

              {/* Active Missions List */}
              <div style={{
                background: 'rgba(255, 255, 255, 0.02)',
                backdropFilter: 'blur(10px)',
                border: '1px solid rgba(255, 255, 255, 0.1)',
                borderRadius: isMobile ? 16 : 20,
                padding: isMobile ? 24 : 32,
                boxShadow: '0 20px 40px -12px rgba(0, 0, 0, 0.3)'
              }}>
                <div style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  marginBottom: 24
                }}>
                  <span style={{ fontSize: isMobile ? '1.5rem' : '2rem' }}>🌌</span>
                  <h3 style={{
                    fontSize: isMobile ? '1.3rem' : '1.5rem',
                    margin: 0,
                    color: 'white',
                    fontWeight: 600
                  }}>
                    Active Missions ({lobby.rooms.length})
                  </h3>
                </div>

                {lobby.rooms.length === 0 ? (
                  <div style={{
                    textAlign: 'center',
                    padding: isMobile ? '32px 16px' : '48px 24px',
                    color: 'rgba(255, 255, 255, 0.5)'
                  }}>
                    <div style={{ fontSize: isMobile ? '3rem' : '4rem', marginBottom: 16, opacity: 0.3 }}>
                      🛸
                    </div>
                    <p style={{ 
                      fontSize: isMobile ? '1rem' : '1.1rem',
                      margin: 0,
                      lineHeight: 1.5
                    }}>
                      No active missions found.<br />
                      Create a new game to start trading!
                    </p>
                  </div>
                ) : (
                  <div style={{
                    display: 'flex',
                    flexDirection: 'column',
                    gap: isMobile ? 12 : 16,
                    maxHeight: isMobile ? '400px' : '500px',
                    overflowY: 'auto',
                    paddingRight: 8
                  }}>
                    {lobby.rooms.map(r => (
                      <div 
                        key={r.id}
                        style={{
                          background: 'rgba(255, 255, 255, 0.05)',
                          border: '1px solid rgba(255, 255, 255, 0.1)',
                          borderRadius: isMobile ? 12 : 14,
                          padding: isMobile ? 16 : 20,
                          transition: 'all 0.3s ease',
                          cursor: 'pointer'
                        }}
                        onMouseEnter={(e) => {
                          e.currentTarget.style.background = 'rgba(255, 255, 255, 0.08)'
                          e.currentTarget.style.borderColor = 'rgba(102, 126, 234, 0.3)'
                          e.currentTarget.style.transform = 'translateY(-2px)'
                        }}
                        onMouseLeave={(e) => {
                          e.currentTarget.style.background = 'rgba(255, 255, 255, 0.05)'
                          e.currentTarget.style.borderColor = 'rgba(255, 255, 255, 0.1)'
                          e.currentTarget.style.transform = 'translateY(0)'
                        }}
                        onClick={() => joinRoom(r.id)}
                      >
                        <div style={{
                          display: 'flex',
                          justifyContent: 'space-between',
                          alignItems: isMobile ? 'flex-start' : 'center',
                          flexDirection: isMobile ? 'column' : 'row',
                          gap: isMobile ? 8 : 16
                        }}>
                          <div style={{ flex: 1 }}>
                            <h4 style={{
                              fontSize: isMobile ? '1.1rem' : '1.2rem',
                              margin: '0 0 8px 0',
                              color: 'white',
                              fontWeight: 600
                            }}>
                              {r.private && <span style={{ marginRight: 6 }} aria-hidden="true">🔒</span>}
                              {r.name}
                            </h4>
                            <div style={{
                              display: 'flex',
                              flexWrap: 'wrap',
                              gap: isMobile ? 8 : 12,
                              fontSize: isMobile ? '0.85rem' : '0.9rem',
                              color: 'rgba(255, 255, 255, 0.6)'
                            }}>
                              <span style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: 4
                              }}>
                                <span style={{ fontSize: '1rem' }}>👥</span>
                                {r.playerCount} commanders
                              </span>
                              {r.started && (
                                <span style={{
                                  display: 'flex',
                                  alignItems: 'center',
                                  gap: 4,
                                  color: '#10b981'
                                }}>
                                  <span style={{ fontSize: '1rem' }}>🎯</span>
                                  Turn {r.turn ?? 0}
                                </span>
                              )}
                            </div>
                          </div>
                          <div style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 8
                          }}>
                            {r.private && (
                              <span style={{
                                padding: isMobile ? '6px 12px' : '4px 8px',
                                fontSize: isMobile ? '0.75rem' : '0.7rem',
                                fontWeight: 600,
                                borderRadius: 20,
                                background: 'rgba(167, 139, 250, 0.2)',
                                color: '#a855f7',
                                border: '1px solid rgba(167, 139, 250, 0.35)'
                              }}>
                                PRIVATE
                              </span>
                            )}
                            {r.paused && (
                              <span style={{
                                padding: isMobile ? '6px 12px' : '4px 8px',
                                fontSize: isMobile ? '0.75rem' : '0.7rem',
                                fontWeight: 600,
                                borderRadius: 20,
                                background: 'rgba(251, 191, 36, 0.2)',
                                color: '#fbbf24',
                                border: '1px solid rgba(251, 191, 36, 0.35)'
                              }}>
                                PAUSED
                              </span>
                            )}
                            <span style={{
                              padding: isMobile ? '6px 12px' : '4px 8px',
                              fontSize: isMobile ? '0.75rem' : '0.7rem',
                              fontWeight: 600,
                              borderRadius: 20,
                              background: r.started 
                                ? 'rgba(16, 185, 129, 0.2)' 
                                : 'rgba(59, 130, 246, 0.2)',
                              color: r.started ? '#10b981' : '#3b82f6',
                              border: `1px solid ${r.started 
                                ? 'rgba(16, 185, 129, 0.3)' 
                                : 'rgba(59, 130, 246, 0.3)'}`
                            }}>
                              {r.started ? 'IN PROGRESS' : 'RECRUITING'}
                            </span>
                            <span style={{ 
                              fontSize: isMobile ? '1.2rem' : '1.5rem',
                              opacity: 0.6
                            }}>
                              →
                            </span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Footer info */}
          <div style={{
            textAlign: 'center',
            color: 'rgba(255, 255, 255, 0.4)',
            fontSize: isMobile ? '0.8rem' : '0.9rem'
          }}>
            <p style={{ margin: 0 }}>
              Welcome, Commander {name}. Select a mission to begin your trading journey.
            </p>
          </div>
        </div>

        {/* Connection Status */}
        <ConnectionStatus 
          connectionState={connectionState}
          isReconnecting={isReconnecting}
          error={error}
          reconnect={reconnect}
          isMobile={isMobile}
        />
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
  const preGame = !r.room.started
  const readyToStart = Boolean(r.room.allReady)
  const renderShipSections = () => {
    const inventory = r.you.inventory || {}
    const inventoryKeys = Object.keys(inventory).sort()
    const fuelCapacityValue = (r.you as any).fuelCapacity ?? 100
    const speedPerTurn = (r.you as any).speedPerTurn ?? 20

    return (
      <>
        <div style={{ marginBottom: isMobile ? 16 : 12 }}>
          <h4
            style={{
              margin: '0 0 8px 0',
              fontSize: isMobile ? shrinkFont(16) : 14,
              fontWeight: 600,
              color: 'var(--accent2)'
            }}
          >
            Ship Stats
          </h4>
          <div style={{ fontSize: isMobile ? shrinkFont(14) : 13, color: 'var(--text)' }}>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                padding: '4px 0'
              }}
            >
              <span>Fuel Tank:</span>
              <span>
                <strong>{fuelCapacityValue}</strong> units
              </span>
            </div>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                padding: '4px 0'
              }}
            >
              <span>Engine Speed:</span>
              <span>
                <strong>{speedPerTurn}</strong> units/turn
              </span>
            </div>
          </div>
        </div>

        <div>
          <h4
            style={{
              margin: '0 0 8px 0',
              fontSize: isMobile ? shrinkFont(16) : 14,
              fontWeight: 600,
              color: 'var(--accent2)'
            }}
          >
            Cargo Hold [{usedSlots}/{capacity}]
          </h4>
          {inventoryKeys.length === 0 ? (
            <div
              style={{
                fontSize: isMobile ? shrinkFont(14) : 13,
                color: 'var(--muted)',
                fontStyle: 'italic'
              }}
            >
              Empty
            </div>
          ) : (
            <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
              {inventoryKeys.map((g) => {
                const qty = inventory[g]
                const avg = r.you.inventoryAvgCost?.[g]
                return (
                  <li
                    key={g}
                    style={{
                      padding: isMobile ? '4px 0' : '2px 0',
                      fontSize: isMobile ? shrinkFont(14) : 13,
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      gap: 8
                    }}
                  >
                    <span>{g}:</span>
                    <span>
                      <strong>{qty}</strong>
                      {typeof avg === 'number' ? (
                        <span className="muted"> (avg ${avg})</span>
                      ) : null}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      </>
    )
  }
  const destName = r.you.destinationPlanet
  const inTransit = Boolean((r.you as any).inTransit)
  const transitFrom = (r.you as any).transitFrom || r.you.currentPlanet
  const transitRemaining: number = (r.you as any).transitRemaining ?? 0
  const transitTotal: number = (r.you as any).transitTotal ?? 0

  const travelUnits = (from?: string, to?: string) => {
    if (!from || !to || from === to) return 0
    const a = getWorldPosition(from)
    const b = getWorldPosition(to)
    if (!a || !b) return 0
    const dx = a.x - b.x
    const dy = a.y - b.y
    const distance = Math.sqrt(dx * dx + dy * dy)
    const scale = Math.max(worldBounds.unitScale, 0.1)
    return Math.max(1, Math.ceil(distance / scale))
  }

  const yourTransitPos = (() => {
    if (!inTransit) return undefined as undefined | { x: number; y: number }
    if (!transitFrom || !destName || transitTotal <= 0) return undefined
    const start = getWorldPosition(transitFrom)
    const end = getWorldPosition(destName)
    if (!start || !end) return undefined
    const progress = Math.max(0, Math.min(1, (transitTotal - transitRemaining) / transitTotal))
    const world = {
      x: start.x + (end.x - start.x) * progress,
      y: start.y + (end.y - start.y) * progress,
    }
    return worldToScreen(world)
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
              fontSize: isMobile ? shrinkFont(18) : 'inherit'
            }}>{r.you.modal.title}</div>
            <div style={{ 
              whiteSpace: 'pre-wrap', 
              marginBottom: isMobile ? 16 : 12,
              fontSize: isMobile ? shrinkFont(16) : 'inherit',
              lineHeight: isMobile ? 1.5 : 'inherit'
            }}>{r.you.modal.body}</div>
            
            {/* Auction bid input */}
            {(r.you.modal as any).kind === 'auction' && (
              <div style={{ marginBottom: isMobile ? 16 : 12 }}>
                <label style={{ 
                  display: 'block', 
                  marginBottom: isMobile ? 8 : 4, 
                  fontSize: isMobile ? shrinkFont(14) : 12,
                  color: 'var(--text-muted)'
                }}>
                  Your Bid (Suggested: {(r.you.modal as any).suggestedBid} credits):
                </label>
                <input
                  type="number"
                  value={auctionBid}
                  onChange={(e) => handleAuctionBidChange(e.target.value)}
                  placeholder={(r.you.modal as any).suggestedBid?.toString() || '0'}
                  min="1"
                  max={r.you.money}
                  style={{
                    width: '100%',
                    padding: isMobile ? '12px' : '8px',
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
                    border: '1px solid var(--border)',
                    borderRadius: '4px',
                    background: 'var(--bg)',
                    color: 'var(--text)'
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      submitAuctionBid()
                    }
                  }}
                />
                <div style={{ 
                  fontSize: isMobile ? shrinkFont(12) : 10,
                  color: 'var(--text-muted)',
                  marginTop: isMobile ? 4 : 2
                }}>
                  You have {r.you.money.toLocaleString()} credits available
                </div>
              </div>
            )}
            
            { (r.you.modal as any).kind === 'auction' ? (
              <div style={{ display:'flex', justifyContent:'flex-end', gap: isMobile ? 12 : 8, flexDirection: isMobile ? 'column' : 'row' }}>
                <button 
                  onClick={()=>ackModal(r.you.modal?.id)}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
                    minHeight: isMobile ? 48 : 'auto',
                    order: isMobile ? 2 : 'unset'
                  }}
                >
                  Skip Auction
                </button>
                <button 
                  onClick={submitAuctionBid}
                  disabled={!auctionBid || parseInt(auctionBid) <= 0 || parseInt(auctionBid) > r.you.money}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
                    minHeight: isMobile ? 48 : 'auto',
                    order: isMobile ? 1 : 'unset',
                    opacity: (!auctionBid || parseInt(auctionBid) <= 0 || parseInt(auctionBid) > r.you.money) ? 0.5 : 1
                  }}
                >
                  Place Bid
                </button>
              </div>
            ) : (r.you.modal as any).kind ? (
              <div style={{ display:'flex', justifyContent:'flex-end', gap: isMobile ? 12 : 8, flexDirection: isMobile ? 'column' : 'row' }}>
                <button 
                  onClick={()=>send('respondModal', { id: r.you.modal?.id, accept: false })}
                  style={{
                    padding: isMobile ? '12px 24px' : '8px 16px',
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
                    fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
          <strong className="glow" style={{ fontSize: isMobile ? shrinkFont(16) : 'inherit' }}>{r.room.name}</strong>
          {r.room.private && (
            <span
              className="muted"
              style={{
                fontSize: isMobile ? shrinkFont(14) : 'inherit',
                display: 'inline-flex',
                alignItems: 'center'
              }}
              role="img"
              aria-label="Private room"
              title="Private room"
            >
              🔒
            </span>
          )}
          <span className="muted" style={{ fontSize: isMobile ? shrinkFont(14) : 'inherit' }}>· Turn: {r.room.turn}</span>
          {r.room.paused ? (
            <span className="muted" style={{ fontSize: isMobile ? shrinkFont(14) : 'inherit', color: '#fbbf24', fontWeight: 600 }}>
              · Paused
            </span>
          ) : (typeof r.room.turnEndsAt === 'number' && (
            <span className="muted" style={{ fontSize: isMobile ? shrinkFont(14) : 'inherit' }}>
              · {Math.max(0, Math.ceil((r.room.turnEndsAt - now) / 1000))}s
            </span>
          ))}
          {/* Tabs */}
          <div style={{ 
            marginLeft: isMobile ? 0 : 8, 
            display: isMobile ? 'grid' : 'inline-flex', 
            gridTemplateColumns: isMobile ? 'repeat(6, minmax(0, 1fr))' : undefined,
            border: '1px solid var(--border)', 
            borderRadius: 8, 
            overflow: 'hidden',
            width: isMobile ? '100%' : 'auto',
            order: isMobile ? 1 : 'unset'
          }}>
            <button onClick={()=>setActiveTab('map')} style={{ 
              padding: isMobile ? '10px 8px' : '4px 8px', 
              background: activeTab==='map' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              border: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? shrinkFont(14) : 'inherit',
              minHeight: isMobile ? 44 : 'auto'
            }}>Map</button>
                  <button
              onClick={() => {
                if (preGame) return
                setActiveTab('market')
              }}
              disabled={preGame}
              style={{ 
                padding: isMobile ? '10px 8px' : '4px 8px', 
                background: activeTab==='market' ? 'rgba(167,139,250,0.18)' : 'transparent', 
                borderLeft: isMobile ? 'none' : '1px solid var(--border)', 
                borderRight: 'none', 
                borderTop: 'none', 
                borderBottom: 'none',
                flex: isMobile ? 1 : 'none',
                fontSize: isMobile ? shrinkFont(14) : 'inherit',
                minHeight: isMobile ? 44 : 'auto',
                opacity: preGame ? 0.35 : 1,
                cursor: preGame ? 'not-allowed' : 'pointer'
              }}
            >Market</button>
            <button onClick={()=>setActiveTab('locations')} style={{ 
              padding: isMobile ? '10px 8px' : '4px 8px', 
              background: activeTab==='locations' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: isMobile ? 'none' : '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? shrinkFont(14) : 'inherit',
              minHeight: isMobile ? 44 : 'auto'
            }}>Locations</button>
            <button onClick={()=>setActiveTab('players')} style={{ 
              padding: isMobile ? '10px 8px' : '4px 8px', 
              background: activeTab==='players' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: isMobile ? 'none' : '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? shrinkFont(14) : 'inherit',
              minHeight: isMobile ? 44 : 'auto'
            }}>Players</button>
            <button onClick={()=>setActiveTab('ship')} style={{ 
              padding: isMobile ? '10px 8px' : '4px 8px', 
              background: activeTab==='ship' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: isMobile ? 'none' : '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? shrinkFont(14) : 'inherit',
              minHeight: isMobile ? 44 : 'auto'
            }}>Ship</button>
            <button onClick={()=>setActiveTab('graphs')} style={{ 
              padding: isMobile ? '10px 8px' : '4px 8px', 
              background: activeTab==='graphs' ? 'rgba(167,139,250,0.18)' : 'transparent', 
              borderLeft: isMobile ? 'none' : '1px solid var(--border)', 
              borderRight: 'none', 
              borderTop: 'none', 
              borderBottom: 'none',
              flex: isMobile ? 1 : 'none',
              fontSize: isMobile ? shrinkFont(14) : 'inherit',
              minHeight: isMobile ? 44 : 'auto'
            }}>Graphs</button>
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
        fontSize: isMobile ? shrinkFont(16) : 'inherit',
        fontWeight: isMobile ? 600 : 'normal',
        minHeight: isMobile ? 48 : 'auto'
      }}
            title={r.you.ready ? 'Ready' : 'Not Ready'}
          >
            Ready
          </button>
          <span style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}><strong>${r.you.money}</strong></span>
          <span style={{ fontSize: isMobile ? shrinkFont(14) : 'inherit', color: 'var(--muted)' }}>
            Cargo: <strong>{usedSlots}</strong>/{capacity}
          </span>
          <div title="Ship fuel (price varies by planet)" style={{ 
            display: isMobile ? 'flex' : 'block',
            flexDirection: isMobile ? 'column' : 'row',
            gap: isMobile ? 4 : 0,
            alignItems: isMobile ? 'flex-start' : 'center'
          }}>
            <span style={{ 
              marginLeft: isMobile ? 0 : 8,
              fontSize: isMobile ? shrinkFont(14) : 'inherit'
            }}>
              Fuel: <strong>{r.you.fuel}</strong>/{(r.you as any).fuelCapacity ?? 100}
            </span>
      <span className="muted" style={{ 
        marginLeft: isMobile ? 0 : 8,
        fontSize: isMobile ? shrinkFont(12) : 'inherit'
      }}>@ ${ fuelPrice }/unit</span>
            <button 
              onClick={() => refuel(0)} 
              style={{ 
                marginLeft: isMobile ? 0 : 6,
                marginTop: isMobile ? 4 : 0,
                padding: isMobile ? '8px 16px' : '4px 8px',
                fontSize: isMobile ? shrinkFont(14) : 'inherit',
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
                onClick={addBot}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none'
                }}
              >
                Add Bot
              </button>
              <button 
                onClick={() => {
                  const gameUrl = `${window.location.origin}${window.location.pathname}?room=${r.room.id}`
                  const toastId = Date.now().toString()
                  navigator.clipboard.writeText(gameUrl).then(() => {
                    setToasts(prev => [...prev, { 
                      id: toastId, 
                      text: 'Game link copied to clipboard!',
                      at: Date.now()
                    }])
                    setTimeout(() => {
                      setToasts(prev => prev.filter(t => t.id !== toastId))
                    }, 2000)
                  }).catch(() => {
                    // Fallback for older browsers
                    const textArea = document.createElement('textarea')
                    textArea.value = gameUrl
                    document.body.appendChild(textArea)
                    textArea.select()
                    document.execCommand('copy')
                    document.body.removeChild(textArea)
                    setToasts(prev => [...prev, { 
                      id: toastId, 
                      text: 'Game link copied to clipboard!',
                      at: Date.now()
                    }])
                    setTimeout(() => {
                      setToasts(prev => prev.filter(t => t.id !== toastId))
                    }, 2000)
                  })
                }}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none',
                  background: 'var(--accent)',
                  border: '1px solid var(--accent2)',
                  color: 'white'
                }}
                title="Copy shareable link to this game"
              >
                📋 Share
              </button>
              <button 
                onClick={exitRoom}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
                    fontSize: isMobile ? shrinkFont(18) : 'inherit'
                  }}>Ship Inventory — {playerInfo.name}</div>
                  <button 
                    onClick={()=>setPlayerInfo(null)}
                    style={{
                      padding: isMobile ? '8px 16px' : '4px 8px',
                      fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
                onClick={() => {
                  const gameUrl = `${window.location.origin}${window.location.pathname}?room=${r.room.id}`
                  const toastId = Date.now().toString()
                  navigator.clipboard.writeText(gameUrl).then(() => {
                    setToasts(prev => [...prev, { 
                      id: toastId, 
                      text: 'Game link copied to clipboard!',
                      at: Date.now()
                    }])
                    setTimeout(() => {
                      setToasts(prev => prev.filter(t => t.id !== toastId))
                    }, 2000)
                  }).catch(() => {
                    // Fallback for older browsers
                    const textArea = document.createElement('textarea')
                    textArea.value = gameUrl
                    document.body.appendChild(textArea)
                    textArea.select()
                    document.execCommand('copy')
                    document.body.removeChild(textArea)
                    setToasts(prev => [...prev, { 
                      id: toastId, 
                      text: 'Game link copied to clipboard!',
                      at: Date.now()
                    }])
                    setTimeout(() => {
                      setToasts(prev => prev.filter(t => t.id !== toastId))
                    }, 2000)
                  })
                }}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
                  fontWeight: isMobile ? 600 : 'normal',
                  minHeight: isMobile ? 48 : 'auto',
                  flex: isMobile ? 1 : 'none',
                  background: 'var(--accent)',
                  border: '1px solid var(--accent2)',
                  color: 'white'
                }}
                title="Copy shareable link to this game"
              >
                📋 Share
              </button>
              <button 
                onClick={exitRoom}
                style={{
                  padding: isMobile ? '12px 20px' : '6px 12px',
                  fontSize: isMobile ? shrinkFont(16) : 'inherit',
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
            @keyframes flowingDots {
              0% { stroke-dashoffset: 12; }
              100% { stroke-dashoffset: 0; }
            }
            @keyframes solarPulse {
              0% { transform: translate(-50%, -50%) scale(1); opacity: 0.9; }
              50% { transform: translate(-50%, -50%) scale(1.08); opacity: 1; }
              100% { transform: translate(-50%, -50%) scale(1); opacity: 0.9; }
            }
            @keyframes readyPulse {
              0% { transform: scale(1); box-shadow: 0 0 0 rgba(34,197,94,0.45); }
              50% { transform: scale(1.05); box-shadow: 0 0 22px rgba(34,197,94,0.55); }
              100% { transform: scale(1); box-shadow: 0 0 0 rgba(34,197,94,0.45); }
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
      <div style={{ flex:1, minHeight:0, display:'flex', flexDirection:'column', padding: isMobile ? '0 12px 12px' : '0 16px 16px' }}>
        <div
          ref={planetsContainerRef}
          className="panel"
          onWheel={handleWheel}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerLeave={handlePointerLeave}
          onPointerCancel={handlePointerUp}
          style={{ 
          position:'relative', 
          flex:1, 
          minHeight: isMobile ? 300 : 0, 
          overflow:'hidden', 
          backgroundColor:'#000', 
          backgroundImage: `url(${starfieldUrl})`, 
          backgroundSize:'cover', 
          backgroundPosition:'center', 
          backgroundRepeat:'no-repeat',
          touchAction:'none',
          userSelect:'none',
          cursor: isDraggingMap ? 'grabbing' : 'grab',
          filter: preGame ? 'grayscale(0.55) brightness(0.85)' : 'none',
          transition: 'filter 220ms ease'
        }}>
          <div aria-hidden style={{
            position: 'absolute',
            left: '50%',
            top: '50%',
            transform: 'translate(-50%, -50%)',
            width: isMobile ? 160 : 220,
            height: isMobile ? 160 : 220,
            borderRadius: '50%',
            background: 'radial-gradient(circle at 50% 50%, rgba(255, 220, 160, 0.95) 0%, rgba(255, 196, 120, 0.75) 45%, rgba(255, 160, 60, 0.45) 70%, rgba(255, 140, 40, 0) 100%)',
            boxShadow: '0 0 45px rgba(255, 200, 120, 0.35), 0 0 120px rgba(255, 170, 80, 0.25)',
            pointerEvents: 'none',
            zIndex: 0,
            animation: 'solarPulse 6s ease-in-out infinite'
          }} />
          <ul style={{ listStyle:'none', padding:0, margin:0, position:'absolute', inset:0, zIndex:1 }}>
            {r.room.planets.map(p => {
              const onPlanet = (r.room.players as any[]).filter(pl => pl.currentPlanet === p && !(pl as any).bankrupt)
              const center = planetScreenPos[p]
              if (!center) {
                return null
              }
              const left = center.x
              const top = center.y
              const need = travelUnits(r.you.currentPlanet, p)
              const canReach = !inTransit && (p === r.you.currentPlanet || need <= (r.you.fuel ?? 0))
              const isHere = p === r.you.currentPlanet
              const locationType = classifyLocation(p)
              const isStationLocation = locationType === 'station'
              const isMoonLocation = locationType === 'moon'
              const disabled = p === r.you.currentPlanet || !canReach
              const mobileScale = isMobile ? 0.8 : 1
              const iconSize = Math.max(24, Math.round(basePlanetIconDiameter * locationIconScale))
              const haloPadding = isHere ? Math.max(18, Math.round(iconSize * 0.42)) : 0
              const containerDiameter = iconSize + haloPadding
              const labelFontPx = Math.round((isMobile ? Math.max(11, Math.round(14 * mobileScale)) : 14) * clampNumber(Math.sqrt(locationIconScale), 1, 1.3))
              const labelFontSize = `${labelFontPx}px`
              const labelColor = isStationLocation
                ? 'rgba(222,247,254,0.95)'
                : isMoonLocation
                  ? 'rgba(226,232,240,0.92)'
                  : 'rgba(248,250,252,0.96)'
              const labelIcon = isStationLocation ? '🛰️' : isMoonLocation ? '🌙' : '🪐'
              const labelVisibleStart = 4
              const labelVisibleFull = 4.4
              const labelFadeRange = Math.max(0.0001, labelVisibleFull - labelVisibleStart)
              const labelVisibility = clampNumber((mapView.zoom - labelVisibleStart) / labelFadeRange, 0, 1)
              const showLabelStack = mapView.zoom >= labelVisibleStart
              const buttonGapBase = Math.max(4, Math.round((isMobile ? 6 : 4) * mobileScale))
              const buttonGap = Math.max(3, Math.round(buttonGapBase * clampNumber(locationIconScale, 0.9, 1.2)))
              const labelAnchorHeight = ((isHere ? containerDiameter : iconSize) / 2) + buttonGap
              const allowLocationAnimation = mapView.zoom >= 4
              const orbitPlayers = onPlanet.filter((pl: any) => !(pl.id === r.you.id && inTransit))
              const orbitCount = orbitPlayers.length
              const orbitRadius = Math.max((isHere ? containerDiameter : iconSize) * 0.5 + 14, iconSize * 0.75)
              const orbitDiameter = orbitRadius * 2
              const orbitSpeed = 18 + orbitCount * 2
              const orbitIconSize = Math.max(18, Math.round(iconSize * 0.36))

              const locationIcon = (() => {
                if (isStationLocation) return renderStationIcon(p, iconSize, allowLocationAnimation)
                if (isMoonLocation) return renderMoonIcon(p, iconSize)
                return renderPlanetIcon(p, iconSize, allowLocationAnimation)
              })()

              return (
                <li
                  key={p}
                  ref={(el: HTMLLIElement | null) => { planetRefs.current[p] = el }}
                  style={{
                    position: 'absolute',
                    left,
                    top,
                    pointerEvents: 'none'
                  }}
                >
                  <div
                    style={{
                      position: 'relative',
                      width: containerDiameter,
                      height: containerDiameter,
                      transform: 'translate(-50%, -50%)',
                      pointerEvents: 'none',
                      background: 'transparent'
                    }}
                  >
                    {isHere && (
                      <span
                        aria-hidden
                        style={{
                          position: 'absolute',
                          inset: '50%',
                          transform: 'translate(-50%, -50%)',
                          width: containerDiameter,
                          height: containerDiameter,
                          borderRadius: '50%',
                          border: '2px solid rgba(34,197,94,0.85)',
                          boxShadow: '0 0 16px rgba(34,197,94,0.45)',
                          background: 'rgba(34,197,94,0.12)',
                          pointerEvents: 'none'
                        }}
                      />
                    )}
                    <button
                      disabled={disabled}
                      onClick={() => selectPlanet(p)}
                      aria-label={p === r.you.currentPlanet ? `${p} (current location)` : `Set course to ${p}`}
                      style={{
                        position: 'absolute',
                        inset: '50%',
                        transform: 'translate(-50%, -50%)',
                        background: 'transparent',
                        border: 'none',
                        width: iconSize,
                        height: iconSize,
                        padding: 0,
                        borderRadius: '50%',
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        zIndex: 1,
                        cursor: disabled ? 'default' : 'pointer',
                        touchAction: 'manipulation',
                        opacity: disabled && !isHere ? 0.55 : 1,
                        pointerEvents: disabled ? 'none' : 'auto'
                      }}
                      title={inTransit ? 'Unavailable while in transit' : (p === r.you.currentPlanet ? 'You are here' : (!canReach ? `Need ${need} units (have ${r.you.fuel ?? 0})` : undefined))}
                    >
                      <span
                        style={{
                          position: 'relative',
                          display: 'inline-flex',
                          width: '100%',
                          height: '100%',
                          alignItems: 'center',
                          justifyContent: 'center'
                        }}
                      >
                        <span
                          style={{
                            display: 'inline-flex',
                            width: '100%',
                            height: '100%',
                            alignItems: 'center',
                            justifyContent: 'center',
                            opacity: disabled && !isHere ? 0.6 : 1,
                            transition: 'opacity 160ms ease'
                          }}
                        >
                          {locationIcon}
                        </span>
                      </span>
                    </button>
                    {orbitCount > 0 && (
                      <div
                        aria-hidden
                        style={{
                          position: 'absolute',
                          inset: '50%',
                          transform: 'translate(-50%, -50%)',
                          width: orbitDiameter,
                          height: orbitDiameter,
                          pointerEvents: 'none',
                          zIndex: 4
                        }}
                      >
                        {orbitPlayers.map((pl: any, idx: number) => {
                          const angle = orbitCount > 1 ? (360 / orbitCount) * idx : 0
                          const shipColor = colorFor(String(pl.id))
                          const slotStyle: CSSPropertiesWithVars = {
                            '--orbit-angle': `${angle}deg`,
                            '--orbit-radius': `${orbitRadius}px`,
                            '--orbit-speed': `${orbitSpeed}s`,
                            animationPlayState: allowLocationAnimation ? 'running' : 'paused'
                          }
                          const iconStyle: CSSPropertiesWithVars = {
                            '--orbit-icon-size': `${orbitIconSize}px`,
                            background: shipColor,
                            boxShadow: `0 0 10px ${shipColor}55`
                          }
                          return (
                            <span
                              key={pl.id}
                              className="planet-orbit-slot"
                              style={slotStyle}
                              title={pl.name}
                            >
                              <span className="planet-orbit-track">
                                <span className="planet-orbit-icon" style={iconStyle}>
                                  <span className="planet-orbit-emoji" role="img" aria-label={`${pl.name || 'Player'} spaceship`}>
                                    🚀
                                  </span>
                                </span>
                              </span>
                            </span>
                          )
                        })}
                      </div>
                    )}
                    {showLabelStack && (
                      <div
                        style={{
                          position: 'absolute',
                          top: labelAnchorHeight,
                          left: '50%',
                          transform: 'translate(-50%, 0)',
                          display: 'flex',
                          flexDirection: 'column',
                          alignItems: 'center',
                          gap: Math.max(4, Math.round(buttonGap * 0.8)),
                          pointerEvents: 'none',
                          zIndex: 3,
                          opacity: labelVisibility,
                          transition: 'opacity 160ms ease'
                        }}
                      >
                        <span
                          style={{
                            fontSize: labelFontSize,
                            fontWeight: 600,
                            letterSpacing: 0.4,
                            color: labelColor,
                            textShadow: '0 2px 6px rgba(2,6,23,0.85)',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 6
                          }}
                        >
                          {labelIcon} {p}
                        </span>
                      </div>
                    )}
                  </div>
                </li>
              )
            })}
          </ul>
          <div
            style={{
              position: 'absolute',
              right: isMobile ? 12 : 16,
              bottom: isMobile ? 12 : 16,
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: isMobile ? 8 : 10,
              zIndex: 5,
              pointerEvents: 'auto'
            }}
          >
            <div
              aria-hidden
              style={{
                minWidth: zoomButtonSize,
                padding: isMobile ? '3px 10px' : '4px 12px',
                borderRadius: 999,
                background: 'rgba(17, 24, 39, 0.78)',
                border: '1px solid rgba(148, 163, 184, 0.35)',
                color: 'rgba(226,232,240,0.92)',
                fontSize: isMobile ? 12 : 13,
                fontWeight: 600,
                textAlign: 'center',
                letterSpacing: 0.2,
                boxShadow: '0 6px 14px rgba(15, 23, 42, 0.38)',
                pointerEvents: 'none'
              }}
            >
              {mapView.zoom.toFixed(2)}×
            </div>
            <button
              type="button"
              onClick={() => handleZoomButton('in')}
              disabled={mapLocked}
              aria-label="Zoom in"
              style={{
                width: zoomButtonSize,
                height: zoomButtonSize,
                borderRadius: zoomButtonSize / 2,
                background: 'rgba(17, 24, 39, 0.82)',
                border: '1px solid rgba(148, 163, 184, 0.45)',
                color: '#fff',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: `${zoomButtonFontSize}px`,
                fontWeight: 600,
                cursor: mapLocked ? 'not-allowed' : 'pointer',
                opacity: mapLocked ? 0.45 : 1,
                boxShadow: '0 6px 14px rgba(15, 23, 42, 0.45)',
                transition: 'transform 120ms ease, box-shadow 120ms ease'
              }}
            >
              +
            </button>
            <button
              type="button"
              onClick={() => handleZoomButton('out')}
              disabled={mapLocked}
              aria-label="Zoom out"
              style={{
                width: zoomButtonSize,
                height: zoomButtonSize,
                borderRadius: zoomButtonSize / 2,
                background: 'rgba(17, 24, 39, 0.82)',
                border: '1px solid rgba(148, 163, 184, 0.45)',
                color: '#fff',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: `${zoomButtonFontSize - 4}px`,
                fontWeight: 600,
                cursor: mapLocked ? 'not-allowed' : 'pointer',
                opacity: mapLocked ? 0.45 : 1,
                boxShadow: '0 6px 14px rgba(15, 23, 42, 0.45)',
                transition: 'transform 120ms ease, box-shadow 120ms ease'
              }}
            >
              −
            </button>
          </div>
          {/* Destination arrows overlay */}
          <svg
            width={containerSize.width}
            height={containerSize.height}
            style={{ position: 'absolute', inset: 0, pointerEvents: 'none', zIndex: 3 }}
          >
            {(r.room.players as any[]).filter(pl => !(pl as any).bankrupt).map(pl => {
              const from = planetScreenPos[pl.currentPlanet]
              const to = pl.destinationPlanet ? planetScreenPos[pl.destinationPlanet] : undefined
              if (!from || !to) return null
              if (pl.destinationPlanet === pl.currentPlanet) return null
              const x1 = from.x, y1 = from.y
              const x2 = to.x, y2 = to.y
              const d = `M ${x1},${y1} L ${x2},${y2}`
              const dx = x2 - x1
              const dy = y2 - y1
              const midX = x1 + dx * 0.5
              const midY = y1 + dy * 0.5
              const isYou = String(pl.id) === String(r.you.id)
              const pathUnits = isYou && pl.destinationPlanet ? travelUnits(pl.currentPlanet, pl.destinationPlanet) : 0
              let angle = Math.atan2(dy, dx) * 180 / Math.PI
              if (angle > 90 || angle < -90) {
                angle += 180
              }
              return (
                <g key={pl.id}>
                  <path 
                    d={d} 
                    fill="none" 
                    stroke={colorFor(String(pl.id))} 
                    strokeWidth={isMobile ? 4 : 3} 
                    strokeLinecap="round" 
                    strokeDasharray="8,4"
                    strokeDashoffset="12"
                    opacity={1}
                    style={{
                      animation: 'flowingDots 0.4s linear infinite',
                      filter: 'drop-shadow(0 0 2px rgba(255,255,255,0.3))'
                    }}
                  />
                  {isYou && pathUnits > 0 && (
                    <text
                      x={midX}
                      y={midY}
                      fill="#fff"
                      fontSize={isMobile ? 12 : 14}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      transform={`rotate(${angle}, ${midX}, ${midY})`}
                      stroke="rgba(15,23,42,0.65)"
                      strokeWidth={isMobile ? 1.4 : 1}
                      paintOrder="stroke"
                      letterSpacing="0.5"
                    >
                      {`${pathUnits} units`}
                    </text>
                  )}
                </g>
              )
            })}
            {inTransit && yourTransitPos && (
              <circle cx={yourTransitPos.x} cy={yourTransitPos.y} r={isMobile ? 9 : 7} fill={colorFor(String(r.you.id))} stroke="#111" strokeOpacity={0.15} />
            )}
          </svg>
          {preGame && (
            <div
              style={{
                position: 'absolute',
                inset: 0,
                zIndex: 6,
                background: 'rgba(10,14,29,0.72)',
                backdropFilter: 'blur(1.8px)',
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                gap: isMobile ? 12 : 18,
                padding: isMobile ? 16 : 28,
                pointerEvents: 'auto'
              }}
            >
              <button
                onClick={startGame}
                disabled={!readyToStart}
                title={readyToStart ? 'All players ready — launch mission' : 'Waiting for every commander to ready up'}
                style={{
                  padding: isMobile ? '18px 34px' : '18px 44px',
                  fontSize: isMobile ? shrinkFont(22) : '1.15rem',
                  fontWeight: 700,
                  borderRadius: 999,
                  border: readyToStart ? '1px solid rgba(21, 128, 61, 0.75)' : '1px solid rgba(148,163,184,0.45)',
                  color: readyToStart ? '#052e16' : 'rgba(226,232,240,0.9)',
                  background: readyToStart ? 'linear-gradient(135deg, #bbf7d0 0%, #22c55e 40%, #16a34a 100%)' : 'linear-gradient(135deg, rgba(148,163,184,0.65) 0%, rgba(71,85,105,0.82) 100%)',
                  cursor: readyToStart ? 'pointer' : 'not-allowed',
                  boxShadow: readyToStart ? '0 16px 32px rgba(34,197,94,0.45)' : '0 10px 26px rgba(15,23,42,0.55)',
                  transition: 'transform 160ms ease, box-shadow 160ms ease',
                  animation: readyToStart ? 'readyPulse 1.6s ease-in-out infinite' : 'none',
                  pointerEvents: 'auto'
                }}
              >
                Start Game
              </button>
              <span
                style={{
                  fontSize: isMobile ? shrinkFont(16) : '1rem',
                  color: 'rgba(226,232,240,0.88)',
                  textAlign: 'center',
                  maxWidth: 420,
                  lineHeight: 1.45
                }}
              >
                {readyToStart ? 'All commanders are ready. Launch when you are!' : 'Waiting for every commander to ready up. Map and market remain locked until then.'}
              </span>
            </div>
          )}
        </div>
      </div>
    )}

    {activeTab==='players' && (() => {
      const playersList = Array.isArray(r.room.players) ? (r.room.players as any[]) : []
      const readyCount = playersList.filter(pl => pl.ready).length
      const totalCredits = playersList.reduce((sum: number, pl: any) => (
        typeof pl.money === 'number' ? sum + pl.money : sum
      ), 0)

      return (
        <div style={{ padding: isMobile ? 12 : 16 }}>
          <h3 className="glow" style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}>Players</h3>
          <div className="panel" style={{ padding: isMobile ? 12 : 16 }}>
            <div style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: isMobile ? 'flex-start' : 'center',
              flexDirection: isMobile ? 'column' : 'row',
              gap: 8,
              marginBottom: playersList.length > 0 ? (isMobile ? 12 : 10) : 0
            }}>
              <span style={{ fontSize: isMobile ? 14 : 13, color: 'var(--muted)' }}>
                Ready {readyCount}/{playersList.length}
              </span>
              {playersList.length > 0 && (
                <span style={{ fontSize: isMobile ? 14 : 13, color: 'var(--muted)' }}>
                  Total Credits: ${totalCredits.toLocaleString()}
                </span>
              )}
            </div>
            {playersList.length === 0 ? (
              <div style={{ padding: isMobile ? 12 : 16, textAlign: 'center', color: 'var(--muted)' }}>
                No players are currently in this room.
              </div>
            ) : (
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: isMobile ? 10 : 8 }}>
                {playersList.map(pl => {
                  const moneyDisplay = typeof pl.money === 'number' ? pl.money.toLocaleString() : pl.money
                  return (
                    <li
                      key={pl.id}
                      style={{
                        display: 'grid',
                        gridTemplateColumns: isMobile
                          ? 'auto auto minmax(0,1fr) auto'
                          : 'auto auto minmax(0,240px) auto auto',
                        alignItems: 'center',
                        gap: isMobile ? 10 : 12,
                        padding: isMobile ? '12px 10px' : '10px 12px',
                        borderRadius: 8,
                        border: '1px solid var(--border)',
                        background: 'rgba(255,255,255,0.02)'
                      }}
                    >
                      <span
                        title={pl.ready ? 'Ready' : 'Not Ready'}
                        style={{
                          width: isMobile ? 10 : 8,
                          height: isMobile ? 10 : 8,
                          borderRadius: '50%',
                          background: pl.ready ? 'var(--good)' : 'var(--bad)'
                        }}
                      />
                      <span
                        style={{
                          width: isMobile ? 14 : 12,
                          height: isMobile ? 14 : 12,
                          borderRadius: '50%',
                          background: colorFor(String(pl.id)),
                          boxShadow: '0 0 0 1px rgba(0,0,0,0.15)'
                        }}
                      />
                      <button
                        onClick={() => requestPlayerInfo(pl.id)}
                        style={{
                          display: 'flex',
                          flexDirection: 'column',
                          alignItems: 'flex-start',
                          gap: 4,
                          background: 'transparent',
                          border: 'none',
                          padding: 0,
                          cursor: 'pointer',
                          color: 'var(--accent2)',
                          fontSize: isMobile ? shrinkFont(16) : 14,
                          fontWeight: 600,
                          textAlign: 'left'
                        }}
                        title="View inventory"
                      >
                        <span>{pl.name}</span>
                        <span style={{ fontSize: isMobile ? shrinkFont(12) : 11, color: 'var(--muted)', fontWeight: 400 }}>View inventory</span>
                      </button>
                      <span style={{ fontWeight: 600, fontSize: isMobile ? shrinkFont(15) : 13 }}>${moneyDisplay}</span>
                      <span className="muted" style={{ fontSize: isMobile ? shrinkFont(13) : 12 }}>@ {pl.currentPlanet}</span>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </div>
      )
    })()}

    {activeTab==='ship' && (
      <div style={{ padding: isMobile ? 12 : 16 }}>
        <h3 className="glow" style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}>Ship</h3>
        <div
          className="panel"
          style={{
            padding: isMobile ? 12 : 16,
            display: 'flex',
            flexDirection: 'column',
            gap: isMobile ? 12 : 16
          }}
        >
          <div
            style={{
              display: 'flex',
              flexDirection: isMobile ? 'column' : 'row',
              alignItems: isMobile ? 'flex-start' : 'center',
              justifyContent: 'space-between',
              gap: isMobile ? 8 : 16
            }}
          >
            <span style={{ fontSize: isMobile ? shrinkFont(14) : 13, color: 'var(--muted)' }}>
              Credits: <strong>${r.you.money.toLocaleString()}</strong>
            </span>
            <span style={{ fontSize: isMobile ? shrinkFont(14) : 13, color: 'var(--muted)' }}>
              Fuel: <strong>{r.you.fuel}</strong>/{(r.you as any).fuelCapacity ?? 100} @ ${fuelPrice}/unit
            </span>
            <span style={{ fontSize: isMobile ? shrinkFont(14) : 13, color: 'var(--muted)' }}>
              Free Slots: <strong>{freeSlots}</strong>
            </span>
          </div>
          {renderShipSections()}
        </div>
      </div>
    )}

    {activeTab==='market' && (() => {
      const inventory = r.you.inventory || {}
      const allGoods = Object.keys(goods)
      const goodsInCargo = allGoods
        .filter(g => (inventory[g] ?? 0) > 0)
        .sort((a, b) => a.localeCompare(b))
      const otherGoods = allGoods
        .filter(g => (inventory[g] ?? 0) <= 0)
        .sort((a, b) => a.localeCompare(b))
  const mobileGridTemplate = 'minmax(0,1.65fr) minmax(0,1fr) minmax(0,1fr) min-content minmax(0,1.25fr) minmax(0,1.25fr)'

  const renderMobileCard = (g: string) => {
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
        const pctValue = range ? (() => {
          const max = range[1]
          return max > 0 ? Math.max(0, Math.min(100, Math.round((price / max) * 100))) : 0
        })() : null
        const pctText = pctValue === null ? '—' : `${pctValue}%`
        const pctStyle: React.CSSProperties = pctValue === null
          ? { color: 'var(--muted)' }
          : pctValue <= 50
            ? { color: 'var(--good)', fontWeight: 600 }
            : { color: 'var(--bad)', fontWeight: 600 }
        const qtyId = `qty-${g.replace(/\s+/g, '-').toLowerCase()}`

        return (
          <div
            key={g}
            style={{
              padding: '10px 12px',
              border: '1px solid rgba(148,163,184,0.22)',
              borderRadius: 8,
              background: 'rgba(4,7,21,0.7)',
              display: 'grid',
              gridTemplateColumns: mobileGridTemplate,
              alignItems: 'stretch',
              gap: 8
            }}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <span style={{ fontWeight: 700, fontSize: shrinkFont(15), color: 'var(--text)' }}>{g}</span>
              <span style={{ fontWeight: 600, fontSize: shrinkFont(13), color: 'var(--muted)' }}>${price}</span>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: shrinkFont(11), color: 'var(--muted)' }}>
              <span><strong style={{ color: 'var(--text)', fontSize: shrinkFont(12) }}>{available}</strong> avail</span>
              <span>Range {rangeText}</span>
              <span>
                % Max{' '}
                <span style={pctStyle}>{pctText}</span>
              </span>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: shrinkFont(11), color: 'var(--muted)' }}>
              <span><strong style={{ color: 'var(--text)', fontSize: shrinkFont(12) }}>{owned}</strong> owned</span>
              {owned > 0 && typeof youPaid === 'number' ? (
                <span>Avg ${youPaid}</span>
              ) : (
                <span>Avg —</span>
              )}
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, justifySelf: 'start', alignItems: 'flex-start' }}>
              <label htmlFor={qtyId} style={{ fontSize: shrinkFont(10), color: 'var(--muted)', letterSpacing: 0.4, paddingLeft: 2 }}>Qty</label>
              <input
                id={qtyId}
                style={{
                  width: 36,
                  padding: '6px 4px',
                  fontSize: shrinkFont(12),
                  minHeight: 34,
                  borderRadius: 6,
                  border: '1px solid var(--border)',
                  background: 'rgba(12,18,38,0.95)',
                  color: 'var(--text)',
                  textAlign: 'center',
                  WebkitAppearance: 'none',
                  MozAppearance: 'textfield'
                }}
                type="number"
                value={amt}
                min={0}
                max={maxBuy}
                disabled={disabledTrade}
                onChange={e => {
                  const sanitized = sanitizeNumeric(e.target.value)
                  if (sanitized !== e.target.value) {
                    e.target.value = sanitized
                  }
                  const v = Number(sanitized)
                  const capped = Math.max(0, Math.min(maxBuy, isNaN(v) ? 0 : v))
                  setAmountsByGood(s => ({ ...s, [g]: capped }))
                }}
              />
            </div>
            <button
              disabled={disabledTrade || amt <= 0}
              onClick={() => buy(g, amt)}
              title={disabledTrade ? 'Unavailable while in transit' : freeSlots <= 0 ? 'Cargo full' : undefined}
              style={{
                width: '100%',
                padding: '12px 12px',
                fontSize: shrinkFont(14),
                fontWeight: 600,
                minHeight: 46,
                height: '100%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: 6,
                background: disabledTrade || amt <= 0 ? 'rgba(59,130,246,0.35)' : 'var(--accent)',
                color: '#fff',
                border: 'none',
                cursor: disabledTrade || amt <= 0 ? 'not-allowed' : 'pointer'
              }}
            >
              Buy
            </button>
            <button
              disabled={disabledTrade || owned <= 0}
              onClick={() => sell(g, owned)}
              style={{
                width: '100%',
                padding: '12px 12px',
                fontSize: shrinkFont(14),
                fontWeight: 600,
                minHeight: 46,
                height: '100%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: 6,
                cursor: disabledTrade || owned <= 0 ? 'not-allowed' : 'pointer',
                border: '1px solid transparent',
                background: disabledTrade || owned <= 0 ? 'rgba(148,163,184,0.18)' : 'rgba(15,23,42,0.7)',
                color: 'var(--text)',
                ...sellStyle
              }}
              title={disabledTrade ? 'Unavailable while in transit' : undefined}
            >
              Sell
            </button>
          </div>
        )
      }

      const renderTableRow = (g: string) => {
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
        const pctValue = range ? (() => {
          const max = range[1]
          return max > 0 ? Math.max(0, Math.min(100, Math.round((price / max) * 100))) : 0
        })() : null
        const pctText = pctValue === null ? '—' : `${pctValue}%`
        const pctStyle: React.CSSProperties = pctValue === null
          ? { color: 'var(--muted)' }
          : pctValue <= 50
            ? { color: 'var(--good)', fontWeight: 600 }
            : { color: 'var(--bad)', fontWeight: 600 }

        return (
          <tr key={g} style={{ borderBottom:'1px solid var(--border)' }}>
            <td style={{ padding:'6px 8px', fontWeight:700 }}>{g}</td>
            <td style={{ padding:'6px 8px' }}>{available}</td>
            <td style={{ padding:'6px 8px' }}>${price}</td>
            <td style={{ padding:'6px 8px' }} className="muted">{rangeText}</td>
            <td style={{ padding:'6px 8px' }}>
              {pctValue === null ? (
                <span className="muted">{pctText}</span>
              ) : (
                <span style={pctStyle}>{pctText}</span>
              )}
            </td>
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
      }

      const hasCargoGoods = goodsInCargo.length > 0
      const hasOtherGoods = otherGoods.length > 0
      const noGoods = !hasCargoGoods && !hasOtherGoods

      return (
        <div style={{ padding: isMobile ? 12 : 16 }}>
          <h3 className="glow" style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}>Market — {visible.name || r.you.currentPlanet}</h3>
          <div className="panel" style={{ overflowX: 'auto' }}>
            {noGoods ? (
              <div style={{ padding: isMobile ? 16 : 24, textAlign: 'center', color: 'var(--muted)' }}>
                No goods available at this location.
              </div>
            ) : isMobile ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {hasCargoGoods && (
                  <>
                    <div style={{
                      margin: '2px 0 4px',
                      fontSize: 12,
                      textTransform: 'uppercase',
                      letterSpacing: 0.6,
                      color: 'var(--muted)',
                      fontWeight: 600
                    }}>In Cargo</div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: mobileGridTemplate,
                          gap: 8,
                          padding: '0 12px',
                          fontSize: shrinkFont(11),
                          textTransform: 'uppercase',
                          letterSpacing: 0.5,
                          color: 'var(--muted)'
                        }}
                      >
                        <span>Good</span>
                        <span>Market</span>
                        <span>Holdings</span>
                        <span>Qty</span>
                        <span>Buy</span>
                        <span>Sell</span>
                      </div>
                      {goodsInCargo.map(renderMobileCard)}
                    </div>
                  </>
                )}
                {hasOtherGoods && (
                  <>
                    {hasCargoGoods && (
                      <div style={{ height: 1, background: 'rgba(148,163,184,0.18)' }} />
                    )}
                    <div style={{
                      margin: '2px 0 4px',
                      fontSize: 12,
                      textTransform: 'uppercase',
                      letterSpacing: 0.6,
                      color: 'var(--muted)',
                      fontWeight: 600
                    }}>Marketplace</div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: mobileGridTemplate,
                          gap: 8,
                          padding: '0 12px',
                          fontSize: shrinkFont(11),
                          textTransform: 'uppercase',
                          letterSpacing: 0.5,
                          color: 'var(--muted)'
                        }}
                      >
                        <span>Good</span>
                        <span>Market</span>
                        <span>Holdings</span>
                        <span>Qty</span>
                        <span>Buy</span>
                        <span>Sell</span>
                      </div>
                      {otherGoods.map(renderMobileCard)}
                    </div>
                  </>
                )}
              </div>
            ) : (
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
                  {hasCargoGoods && (
                    <>
                      <tr key="cargo-heading" style={{ background:'rgba(148,163,184,0.12)' }}>
                        <td colSpan={8} style={{ padding:'8px 10px', fontSize:12, fontWeight:700, textTransform:'uppercase', letterSpacing:0.6, color:'var(--muted)' }}>In Cargo</td>
                      </tr>
                      {goodsInCargo.map(renderTableRow)}
                    </>
                  )}
                  {hasOtherGoods && (
                    <>
                      {hasCargoGoods && (
                        <tr key="market-heading" style={{ background:'rgba(148,163,184,0.08)' }}>
                          <td colSpan={8} style={{ padding:'8px 10px', fontSize:12, fontWeight:700, textTransform:'uppercase', letterSpacing:0.6, color:'var(--muted)' }}>Marketplace</td>
                        </tr>
                      )}
                      {otherGoods.map(renderTableRow)}
                    </>
                  )}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )
    })()}

    {activeTab==='locations' && (() => {
      const playersList = Array.isArray(r.room.players) ? (r.room.players as any[]) : []
      const playersById = playersList.reduce<Record<string, any>>((acc, pl: any) => {
        acc[pl.id] = pl
        return acc
      }, {})
      const facilityRows = playersList
        .map(pl => ({ id: pl.id, name: pl.name, investment: pl.facilityInvestment ?? 0 }))
        .filter(row => row.investment > 0)
        .sort((a, b) => b.investment - a.investment)
      const totalFacilityInvestment = facilityRows.reduce((sum, row) => sum + row.investment, 0)
      const marketMemory = ((r.you as any)?.marketMemory ?? {}) as Record<string, MarketSnapshot>

      return (
      <div style={{ padding: isMobile ? 12 : 16 }}>
        <h3 className="glow" style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}>Locations &amp; Facilities</h3>
        <div style={{
          display: 'grid',
          gap: isMobile ? 12 : 16,
          gridTemplateColumns: isMobile ? '1fr' : 'repeat(auto-fit, minmax(260px, 1fr))'
        }}>
          {r.room.planets.map(planet => {
            const facilities = facilitiesByPlanet[planet] ?? []
            const docked = (r.room.players as any[]).filter((pl: any) => !pl.bankrupt && pl.currentPlanet === planet)
            const isHome = planet === r.you.currentPlanet
            const snapshot = marketMemory?.[planet]
            const goodsKeys = snapshot
              ? Array.from(new Set([
                  ...Object.keys(snapshot.goods ?? {}),
                  ...Object.keys(snapshot.prices ?? {})
                ])).sort((a, b) => a.localeCompare(b))
              : []
            const intelTimestamp = snapshot?.updatedAt ? new Date(snapshot.updatedAt).toLocaleString() : null
            const gridTemplate = isMobile ? 'minmax(0,1fr) 70px 90px' : 'minmax(0,1fr) 80px 90px 110px'
            return (
              <div key={planet} className="panel" style={{
                padding: isMobile ? 14 : 16,
                display: 'flex',
                flexDirection: 'column',
                gap: isMobile ? 10 : 12
              }}>
                <div style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12
                }}>
                  <span style={{ fontWeight: 600, fontSize: isMobile ? shrinkFont(16) : 18 }}>{planet}</span>
                  <div style={{
                    display: 'flex',
                    gap: 6,
                    flexWrap: 'wrap',
                    justifyContent: 'flex-end'
                  }}>
                    {snapshot ? (
                      <span style={{
                        fontSize: isMobile ? shrinkFont(11) : 12,
                        padding: '4px 8px',
                        borderRadius: 999,
                        background: 'rgba(139, 92, 246, 0.18)',
                        border: '1px solid rgba(139, 92, 246, 0.35)',
                        color: '#a855f7',
                        fontWeight: 600
                      }}>
                        Intel turn {snapshot.turn}{intelTimestamp ? ` · ${intelTimestamp}` : ''}
                      </span>
                    ) : (
                      <span style={{
                        fontSize: isMobile ? shrinkFont(11) : 12,
                        padding: '4px 8px',
                        borderRadius: 999,
                        background: 'rgba(148, 163, 184, 0.18)',
                        border: '1px solid rgba(148, 163, 184, 0.3)',
                        color: 'rgba(226, 232, 240, 0.75)',
                        fontWeight: 600
                      }}>
                        No intel yet
                      </span>
                    )}
                    {isHome && (
                      <span style={{
                        fontSize: isMobile ? shrinkFont(11) : 12,
                        padding: '4px 8px',
                        borderRadius: 999,
                        background: 'rgba(56, 189, 248, 0.18)',
                        border: '1px solid rgba(56, 189, 248, 0.35)',
                        color: '#38bdf8',
                        fontWeight: 600
                      }}>
                        You are here
                      </span>
                    )}
                  </div>
                </div>

                {facilities.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: isMobile ? 8 : 10 }}>
                    {facilities.map((facility, idx) => {
                      const owner = playersById[facility.ownerId]
                      const ownedByYou = facility.ownerId === r.you.id
                      const label = facilities.length > 1 ? `Facility ${idx + 1}` : 'Facility'
                      return (
                        <div key={facility.id ?? `${facility.ownerId}-${facility.type}-${idx}`}
                          style={{
                            padding: isMobile ? 10 : 12,
                            borderRadius: 8,
                            border: '1px solid rgba(255,255,255,0.08)',
                            background: 'rgba(148, 163, 184, 0.06)',
                            display: 'grid',
                            gap: 6,
                            fontSize: isMobile ? 12.5 : 13,
                            color: 'rgba(255,255,255,0.82)'
                          }}
                        >
                          <div style={{ fontWeight: 600, color: 'rgba(255,255,255,0.92)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                            <span>{label}: {facility.type}</span>
                            {ownedByYou && (
                              <span style={{
                                fontSize: isMobile ? 11 : 12,
                                padding: '2px 8px',
                                borderRadius: 999,
                                background: 'rgba(16, 185, 129, 0.22)',
                                border: '1px solid rgba(16, 185, 129, 0.35)',
                                color: '#34d399',
                                fontWeight: 600
                              }}>
                                Yours
                              </span>
                            )}
                          </div>
                          <div>
                            <strong style={{ color: 'rgba(255,255,255,0.9)' }}>Owner:</strong> {facility.ownerName || owner?.name || 'Unknown commander'}
                          </div>
                          {typeof facility.purchasePrice === 'number' && (
                            <div>
                              <strong style={{ color: 'rgba(255,255,255,0.9)' }}>Purchase Price:</strong> ${facility.purchasePrice.toLocaleString()}
                            </div>
                          )}
                          <div>
                            <strong style={{ color: 'rgba(255,255,255,0.9)' }}>Usage Charge:</strong> ${facility.usageCharge.toLocaleString()}
                          </div>
                          {typeof facility.accruedMoney === 'number' && (
                            <div>
                              <strong style={{ color: 'rgba(255,255,255,0.9)' }}>Accrued Revenue:</strong> ${facility.accruedMoney.toLocaleString()}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <div style={{ fontSize: isMobile ? 12.5 : 13, color: 'rgba(255,255,255,0.65)' }}>
                    No facilities have been constructed here yet.
                  </div>
                )}

                <div>
                  <div style={{
                    fontSize: isMobile ? 11 : 12,
                    letterSpacing: '0.04em',
                    textTransform: 'uppercase',
                    color: 'rgba(255,255,255,0.45)',
                    marginBottom: 6
                  }}>
                    Docked Commanders
                  </div>
                  {docked.length === 0 ? (
                    <div style={{ fontSize: isMobile ? 12 : 12.5, color: 'rgba(255,255,255,0.5)' }}>No ships currently docked.</div>
                  ) : (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                      {docked.map((pl: any) => (
                        <span key={pl.id} style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: 6,
                          padding: '6px 10px',
                          background: 'rgba(255,255,255,0.05)',
                          border: '1px solid rgba(255,255,255,0.1)',
                          borderRadius: 999,
                          fontSize: isMobile ? 12 : 12.5
                        }}>
                          <span style={{
                            width: 10,
                            height: 10,
                            borderRadius: 5,
                            background: colorFor(String(pl.id)),
                            boxShadow: '0 0 0 1px rgba(0,0,0,0.25)'
                          }} />
                          {pl.name}{pl.id === r.you.id ? ' (You)' : ''}
                        </span>
                      ))}
                    </div>
                  )}
                </div>

                  <div>
                    <div style={{
                      fontSize: isMobile ? 11 : 12,
                      letterSpacing: '0.04em',
                      textTransform: 'uppercase',
                      color: 'rgba(255,255,255,0.45)',
                      marginBottom: 6
                    }}>
                      Your Market Intel
                    </div>
                    {snapshot ? (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                        <div style={{ fontSize: isMobile ? 12 : 12.5, color: 'rgba(255,255,255,0.7)' }}>
                          Last seen turn {snapshot.turn}
                          {intelTimestamp ? ` · ${intelTimestamp}` : ''}
                        </div>
                        <div style={{ fontSize: isMobile ? 12 : 12.5, color: 'rgba(255,255,255,0.7)' }}>
                          Fuel price: {typeof snapshot.fuelPrice === 'number' ? `$${snapshot.fuelPrice}` : '—'}
                        </div>
                        {goodsKeys.length > 0 ? (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                            <div style={{
                              display: 'grid',
                              gridTemplateColumns: gridTemplate,
                              gap: isMobile ? 6 : 8,
                              fontSize: isMobile ? 11.5 : 12.5,
                              color: 'rgba(255,255,255,0.75)',
                              fontWeight: 600
                            }}>
                              <span>Good</span>
                              <span style={{ textAlign: 'right' }}>Qty</span>
                              <span style={{ textAlign: 'right' }}>Price</span>
                              {!isMobile && <span style={{ textAlign: 'right' }}>Range</span>}
                            </div>
                            {goodsKeys.map(good => {
                              const qty = snapshot.goods?.[good]
                              const price = snapshot.prices?.[good]
                              const range = snapshot.priceRanges?.[good]
                              const rangeText = Array.isArray(range) ? `${range[0]}–${range[1]}` : '—'
                              return (
                                <div key={good} style={{
                                  display: 'grid',
                                  gridTemplateColumns: gridTemplate,
                                  gap: isMobile ? 6 : 8,
                                  fontSize: isMobile ? 11.5 : 12.5,
                                  color: 'rgba(255,255,255,0.75)',
                                  background: 'rgba(255,255,255,0.05)',
                                  border: '1px solid rgba(255,255,255,0.08)',
                                  borderRadius: 6,
                                  padding: '6px 8px'
                                }}>
                                  <span style={{ fontWeight: 600, color: 'rgba(255,255,255,0.9)' }}>{good}</span>
                                  <span style={{ textAlign: 'right' }}>{typeof qty === 'number' ? qty.toLocaleString() : '—'}</span>
                                  <span style={{ textAlign: 'right' }}>{typeof price === 'number' ? `$${price}` : '—'}</span>
                                  {!isMobile && <span style={{ textAlign: 'right', color: 'rgba(255,255,255,0.6)' }}>{rangeText}</span>}
                                </div>
                              )
                            })}
                          </div>
                        ) : (
                          <div style={{ fontSize: isMobile ? 12 : 12.5, color: 'rgba(255,255,255,0.5)' }}>
                            No goods data recorded yet.
                          </div>
                        )}
                      </div>
                    ) : (
                      <div style={{ fontSize: isMobile ? 12 : 12.5, color: 'rgba(255,255,255,0.5)' }}>
                        Visit this planet to log current market prices.
                      </div>
                    )}
                  </div>
              </div>
            )
          })}
        </div>

        <div className="panel" style={{
          marginTop: isMobile ? 16 : 20,
          padding: isMobile ? 14 : 18
        }}>
          <div style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: isMobile ? 'flex-start' : 'center',
            flexDirection: isMobile ? 'column' : 'row',
            gap: isMobile ? 8 : 12,
            marginBottom: isMobile ? 12 : 16
          }}>
            <h4 style={{ margin: 0, fontSize: isMobile ? 16 : 18 }}>Facility Investments</h4>
            <span style={{ fontSize: isMobile ? 12 : 13, color: 'rgba(255,255,255,0.65)' }}>
              Total Invested: ${totalFacilityInvestment.toLocaleString()}
            </span>
          </div>

          {facilityRows.length === 0 ? (
            <div style={{ fontSize: isMobile ? 12.5 : 13, color: 'rgba(255,255,255,0.6)' }}>
              No facility spending has been recorded yet.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {facilityRows.map(row => (
                <div key={row.id} style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  justifyContent: 'space-between',
                  borderBottom: '1px solid rgba(255,255,255,0.06)',
                  paddingBottom: 8
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span style={{
                      width: 12,
                      height: 12,
                      borderRadius: 6,
                      background: colorFor(String(row.id)),
                      boxShadow: '0 0 0 1px rgba(0,0,0,0.28)'
                    }} />
                    <span style={{ fontWeight: 600 }}>{row.name}</span>
                  </div>
                  <div style={{ fontWeight: 600, color: 'rgba(255,255,255,0.85)' }}>
                    ${row.investment.toLocaleString()}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
      )})()}

    {activeTab==='graphs' && (
      <div style={{ padding: isMobile ? 12 : 16 }}>
  <h3 className="glow" style={{ fontSize: isMobile ? shrinkFont(18) : 'inherit' }}>Wealth Over Time</h3>
        <div style={{ overflowX: isMobile ? 'auto' : 'visible' }}>
          <WealthCharts 
            history={wealthHistory} 
            players={r.room.players}
            isMobile={isMobile}
          />
        </div>
      </div>
    )}
  </div>

    {/* Connection Status */}
    <ConnectionStatus 
      connectionState={connectionState}
      isReconnecting={isReconnecting}
      error={error}
      reconnect={reconnect}
      isMobile={isMobile}
    />
    
    {/* Login Modal (render via Portal to escape stacking contexts) */}
    {showLogin && createPortal(
      <div style={{ 
        position: 'fixed', 
        inset: 0, 
        backgroundColor: 'rgba(0,0,0,0.8)', 
        display: 'flex', 
        justifyContent: 'center', 
        alignItems: 'center',
        zIndex: 999999
      }}>
        <div style={{ 
          backgroundColor: '#1a1a2e', 
          padding: '2rem', 
          borderRadius: '8px', 
          border: '1px solid #16213e',
          minWidth: '320px',
          maxWidth: '90vw',
          color: '#eee'
        }}>
          <h2 style={{ marginTop: 0 }}>Sign In / Sign Up</h2>
          {/* Keep the simple test to verify visibility; we’ll replace with LoginForm once visible */}
          <p style={{ marginBottom: 12 }}>If you can see this, the modal is visible and on top.</p>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button onClick={() => {
              console.log('Closing test modal');
              setShowLogin(false);
            }}>
              Close
            </button>
            <a
              href={`${window.location.protocol}//${window.location.host}/auth/start`}
              style={{
                display: 'inline-block',
                padding: '8px 12px',
                border: '1px solid #444',
                borderRadius: 6,
                textDecoration: 'none',
                color: '#fff'
              }}
            >
              Use Hosted UI
            </a>
          </div>
        </div>
      </div>,
      document.body
    )}
    </div>
  )
}
