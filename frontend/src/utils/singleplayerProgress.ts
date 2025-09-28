const STORAGE_PREFIX = 'gs:singleplayer:'
const DEFAULT_MAX_TURNS = 120

const getSessionStorage = () => {
  if (typeof window === 'undefined') {
    return null
  }
  try {
    return window.sessionStorage
  } catch (error) {
    console.warn('Session storage unavailable:', error)
    return null
  }
}

const encodeRecord = (record: SingleplayerSaveRecord): string | null => {
  if (typeof window === 'undefined') return null
  try {
    const json = JSON.stringify(record)
    const encoder = new TextEncoder()
    const bytes = encoder.encode(json)
    let binary = ''
    bytes.forEach(byte => {
      binary += String.fromCharCode(byte)
    })
    return window.btoa(binary)
  } catch (error) {
    console.warn('Failed to encode singleplayer save record:', error)
    return null
  }
}

const decodeRecord = (encoded: string): SingleplayerSaveRecord | null => {
  if (typeof window === 'undefined') return null
  try {
    const binary = window.atob(encoded)
    const len = binary.length
    const bytes = new Uint8Array(len)
    for (let i = 0; i < len; i++) {
      bytes[i] = binary.charCodeAt(i)
    }
    const decoder = new TextDecoder()
    const json = decoder.decode(bytes)
    return JSON.parse(json) as SingleplayerSaveRecord
  } catch (error) {
    console.warn('Failed to decode singleplayer save record:', error)
    return null
  }
}

export interface SingleplayerTurnEntry<TState = unknown> {
  turn: number
  recordedAt: number
  state: TState
}

export interface SingleplayerSaveRecord<TState = unknown> {
  version: number
  playerId: string
  playerName?: string
  roomId: string
  roomName: string
  createdAt: number
  updatedAt: number
  turns: SingleplayerTurnEntry<TState>[]
}

export interface SingleplayerSaveSummary<TState = unknown> {
  key: string
  encoded: string
  record: SingleplayerSaveRecord<TState>
}

export type RecordSingleplayerTurnParams<TState = unknown> = {
  playerId: string
  playerName?: string
  roomId: string
  roomName: string
  turn: number
  turnState: TState
  recordedAt?: number
  maxTurns?: number
}

export function recordSingleplayerTurn<TState = unknown>(params: RecordSingleplayerTurnParams<TState>): SingleplayerSaveRecord<TState> | null {
  const storage = getSessionStorage()
  if (!storage) return null
  const {
    playerId,
    playerName,
    roomId,
    roomName,
    turn,
    turnState,
    recordedAt = Date.now(),
    maxTurns = DEFAULT_MAX_TURNS
  } = params

  const key = `${STORAGE_PREFIX}${roomId}`
  let record: SingleplayerSaveRecord<TState> | null = null
  const existing = storage.getItem(key)
  if (existing) {
    record = decodeRecord(existing) as SingleplayerSaveRecord<TState> | null
  }

  if (!record) {
    record = {
      version: 1,
      playerId,
      playerName,
      roomId,
      roomName,
      createdAt: recordedAt,
      updatedAt: recordedAt,
      turns: []
    }
  } else {
    record.playerId = playerId
    if (playerName) {
      record.playerName = playerName
    }
    if (!record.roomName) {
      record.roomName = roomName
    }
  }

  const entry: SingleplayerTurnEntry<TState> = {
    turn,
    recordedAt,
    state: turnState
  }

  record.turns = record.turns.filter(t => t.turn !== turn)
  record.turns.push(entry)
  record.turns.sort((a, b) => a.turn - b.turn)
  if (record.turns.length > maxTurns) {
    record.turns = record.turns.slice(record.turns.length - maxTurns)
  }
  record.updatedAt = recordedAt

  const encoded = encodeRecord(record)
  if (!encoded) return null

  try {
    storage.setItem(key, encoded)
  } catch (error) {
    console.warn('Failed to persist singleplayer turn record:', error)
    return null
  }

  return record
}

export function listSingleplayerSaves<TState = unknown>(filter?: { playerId?: string }): SingleplayerSaveSummary<TState>[] {
  const storage = getSessionStorage()
  if (!storage) return []
  const results: SingleplayerSaveSummary<TState>[] = []
  try {
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i)
      if (!key || !key.startsWith(STORAGE_PREFIX)) continue
      const encoded = storage.getItem(key)
      if (!encoded) continue
      const record = decodeRecord(encoded) as SingleplayerSaveRecord<TState> | null
      if (!record) continue
      if (filter?.playerId && record.playerId !== filter.playerId) continue
      results.push({ key, encoded, record })
    }
  } catch (error) {
    console.warn('Failed to enumerate singleplayer saves:', error)
    return []
  }
  results.sort((a, b) => b.record.updatedAt - a.record.updatedAt)
  return results
}

export function deleteSingleplayerSave(key: string) {
  const storage = getSessionStorage()
  if (!storage) return
  try {
    storage.removeItem(key)
  } catch (error) {
    console.warn('Failed to delete singleplayer save:', error)
  }
}

export function loadSingleplayerSave(encoded: string): SingleplayerSaveRecord | null {
  return decodeRecord(encoded)
}

export function rekeySingleplayerSave<TState = unknown>(oldKey: string, newRoomId: string, record: SingleplayerSaveRecord<TState>): { key: string; encoded: string } | null {
  const storage = getSessionStorage()
  if (!storage) return null
  try {
    storage.removeItem(oldKey)
  } catch (error) {
    console.warn('Failed to remove old singleplayer save key:', error)
  }
  record.roomId = newRoomId
  record.updatedAt = Date.now()
  const encoded = encodeRecord(record)
  if (!encoded) return null
  const key = `${STORAGE_PREFIX}${newRoomId}`
  try {
    storage.setItem(key, encoded)
  } catch (error) {
    console.warn('Failed to persist rekeyed singleplayer save:', error)
    return null
  }
  return { key, encoded }
}
