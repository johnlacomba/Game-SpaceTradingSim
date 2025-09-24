import React from 'react'
import { createRoot } from 'react-dom/client'
import AppWithAuth from './ui/AppWithAuth.jsx'
import './theme.css'

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppWithAuth />
  </React.StrictMode>
)

// Background roiling controller: randomizes blob sources and durations independently
;(function startBackgroundRoil(){
  if (typeof window === 'undefined') return
  // Prevent duplicate timers in dev strict mode/hot reload
  // @ts-ignore
  if ((window as any).__roilStarted) return
  ;(window as any).__roilStarted = true

  const el = document.body
  const rand = (min: number, max: number) => Math.random() * (max - min) + min
  const pct = (min = 8, max = 92) => `${Math.round(rand(min, max))}%`

  type BlobKey = 'p1'|'p2'|'p3'|'q1'|'q2'|'q3'
  type Cfg = { key: BlobKey; durMin: number; durMax: number; holdMin: number; holdMax: number }
  const blobs: Cfg[] = [
    { key: 'p1', durMin: 28, durMax: 60, holdMin: 2, holdMax: 8 },
    { key: 'p2', durMin: 30, durMax: 64, holdMin: 3, holdMax: 9 },
    { key: 'p3', durMin: 26, durMax: 58, holdMin: 2, holdMax: 7 },
    { key: 'q1', durMin: 48, durMax: 90, holdMin: 4, holdMax: 10 },
    { key: 'q2', durMin: 52, durMax: 96, holdMin: 5, holdMax: 12 },
    { key: 'q3', durMin: 50, durMax: 88, holdMin: 4, holdMax: 10 },
  ]

  const timers: number[] = []
  const setVar = (name: string, value: string) => el.style.setProperty(name, value)

  const schedule = (cfg: Cfg) => {
    const doMove = () => {
      // random duration for this move
      const durSec = rand(cfg.durMin, cfg.durMax)
      setVar(`--${cfg.key}d`, `${durSec}s`)
      // random target position
      setVar(`--${cfg.key}x`, pct())
      setVar(`--${cfg.key}y`, pct())
      // after moving, wait a random hold then move again
      const holdSec = rand(cfg.holdMin, cfg.holdMax)
      const nextMs = (durSec + holdSec) * 1000
      timers.push(window.setTimeout(doMove, nextMs))
    }
    // seed with a random initial position and a small stagger
    setVar(`--${cfg.key}x`, pct())
    setVar(`--${cfg.key}y`, pct())
    timers.push(window.setTimeout(doMove, rand(500, 3000)))
  }

  blobs.forEach(schedule)

  // Optional: expose a cleanup in case of HMR
  ;(window as any).__roilStop = () => { timers.forEach(t => clearTimeout(t)) }
})()
