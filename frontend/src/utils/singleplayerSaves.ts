const STORAGE_PREFIX = 'gs:singleplayer:';
const TURN_LIMIT = 200;
const DEFAULT_TTL_MINUTES = 120;

export interface TurnSnapshot {
  turn: number;
  recordedAt: number;
  state: unknown;
}

export interface SingleplayerSave {
  version: number;
  roomId: string;
  roomName: string;
  ownerId: string;
  createdAt: number;
  updatedAt: number;
  turns: TurnSnapshot[];
}

export interface SingleplayerSaveSummary {
  roomId: string;
  roomName: string;
  ownerId: string;
  updatedAt: number;
  lastTurn: number;
  turnCount: number;
  encoded: string;
}

type RecordTurnInput = {
  roomId: string;
  roomName: string;
  ownerId: string;
  turn: number;
  state: unknown;
  authoritativeState?: unknown;
  recordedAt?: number;
};

type SessionStorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>;

function getSessionStorage(): SessionStorageLike | null {
  if (typeof window === 'undefined') return null;
  try {
    const storage = window.sessionStorage;
    storage.getItem('test');
    return storage;
  } catch (error) {
    console.warn('Session storage unavailable:', error);
    return null;
  }
}

function encodePayload(payload: unknown): string {
  const json = JSON.stringify(payload);
  if (typeof window === 'undefined' || typeof window.btoa !== 'function') {
    return json;
  }

  try {
    const encoder = new TextEncoder();
    const bytes = encoder.encode(json);
    let binary = '';
    bytes.forEach((byte) => {
      binary += String.fromCharCode(byte);
    });
    return window.btoa(binary);
  } catch (error) {
    console.warn('Failed to encode singleplayer save record:', error);
    return json;
  }
}

function decodePayload<T>(encoded: string | null): T | null {
  if (!encoded) return null;

  let json = encoded;
  if (typeof window !== 'undefined' && typeof window.atob === 'function') {
    try {
      const binary = window.atob(encoded);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i += 1) {
        bytes[i] = binary.charCodeAt(i);
      }
      const decoder = new TextDecoder();
      json = decoder.decode(bytes);
    } catch (error) {
      console.warn('Failed to decode singleplayer save record:', error);
      return null;
    }
  }

  try {
    const parsed = JSON.parse(json) as T;
    return parsed;
  } catch (error) {
    console.warn('Failed to parse singleplayer save record:', error);
    return null;
  }
}

function buildSummary(save: SingleplayerSave, encoded: string): SingleplayerSaveSummary {
  const lastTurn = save.turns.length ? save.turns[save.turns.length - 1].turn : 0;
  return {
    roomId: save.roomId,
    roomName: save.roomName,
    ownerId: save.ownerId,
    updatedAt: save.updatedAt,
    lastTurn,
    turnCount: save.turns.length,
    encoded,
  };
}

function pruneExpired(storage: SessionStorageLike, ttlMinutes: number): void {
  const now = Date.now();
  const ttlMs = ttlMinutes * 60 * 1000;
  const keysToRemove: string[] = [];

  for (let idx = 0; idx < storage.length; idx += 1) {
    const key = storage.key(idx);
    if (!key || !key.startsWith(STORAGE_PREFIX)) continue;
    const encoded = storage.getItem(key);
    if (!encoded) {
      keysToRemove.push(key);
      continue;
    }
    const parsed = decodePayload<SingleplayerSave>(encoded);
    if (!parsed) {
      keysToRemove.push(key);
      continue;
    }
    if (parsed.updatedAt && now - parsed.updatedAt > ttlMs) {
      keysToRemove.push(key);
    }
  }

  keysToRemove.forEach((key) => storage.removeItem(key));
}

export function recordSingleplayerTurn(input: RecordTurnInput, ttlMinutes = DEFAULT_TTL_MINUTES): SingleplayerSaveSummary | null {
  const storage = getSessionStorage();
  if (!storage) return null;

  pruneExpired(storage, ttlMinutes);

  const key = `${STORAGE_PREFIX}${input.roomId}`;
  const encoded = storage.getItem(key);
  const existing = decodePayload<SingleplayerSave>(encoded);
  const recordedAt = input.recordedAt ?? Date.now();

  const sourceState = input.authoritativeState ?? input.state;
  const snapshotState = JSON.parse(JSON.stringify(sourceState ?? {}));

  if (snapshotState && typeof snapshotState === 'object') {
    const stateAny = snapshotState as Record<string, any>;
    if (stateAny.you == null || typeof stateAny.you !== 'object') {
      stateAny.you = {};
    }
    const youState = stateAny.you as Record<string, any>;
    const known = new Set<string>();

    if (Array.isArray(youState.knownPlanets)) {
      for (const value of youState.knownPlanets as unknown[]) {
        if (typeof value === 'string' && value) known.add(value);
      }
    }

    if (stateAny.room && typeof stateAny.room === 'object' && Array.isArray(stateAny.room.planets)) {
      for (const value of stateAny.room.planets as unknown[]) {
        if (typeof value === 'string' && value) known.add(value);
      }
    }

    if (typeof youState.currentPlanet === 'string' && youState.currentPlanet) {
      known.add(youState.currentPlanet);
    }
    if (typeof youState.destinationPlanet === 'string' && youState.destinationPlanet) {
      known.add(youState.destinationPlanet);
    }

    if (existing && Array.isArray(existing.turns)) {
      for (const previous of existing.turns) {
        if (!previous || !previous.state) continue;
        const priorState = previous.state as Record<string, any>;
        const addFrom = (value: unknown) => {
          if (!Array.isArray(value)) return;
          for (const entry of value as unknown[]) {
            if (typeof entry === 'string' && entry) known.add(entry);
          }
        };
        addFrom(priorState.discoveredPlanets);
        if (priorState.room && typeof priorState.room === 'object') {
          addFrom((priorState.room as Record<string, unknown>).planets);
        }
        if (priorState.you && typeof priorState.you === 'object') {
          addFrom((priorState.you as Record<string, unknown>).knownPlanets);
        }
      }
    }

    if (known.size > 0) {
      const discovered = Array.from(known).sort()
      youState.knownPlanets = discovered
      stateAny.discoveredPlanets = discovered

      if (stateAny.room && typeof stateAny.room === 'object') {
        stateAny.room.planets = discovered
      }

      try {
        const preview = discovered.slice(0, 12)
        console.log('[SingleplayerSave]', {
          turn: input.turn,
          knownCount: discovered.length,
          preview,
        })
      } catch (error) {
        // Logging failure should never break saves
      }
    }
  }

  const base: SingleplayerSave = existing ?? {
    version: 1,
    roomId: input.roomId,
    roomName: input.roomName,
    ownerId: input.ownerId,
    createdAt: recordedAt,
    updatedAt: recordedAt,
    turns: [],
  };

  base.roomName = input.roomName || base.roomName;
  base.ownerId = input.ownerId || base.ownerId;
  base.updatedAt = recordedAt;

  base.turns = base.turns.filter((turn) => turn.turn !== input.turn);
  base.turns.push({
    turn: input.turn,
    recordedAt,
    state: snapshotState,
  });
  base.turns.sort((a, b) => a.turn - b.turn);
  if (base.turns.length > TURN_LIMIT) {
    base.turns = base.turns.slice(base.turns.length - TURN_LIMIT);
  }

  const nextEncoded = encodePayload(base);
  storage.setItem(key, nextEncoded);

  return buildSummary(base, nextEncoded);
}

export function listSingleplayerSaves(ownerId?: string, ttlMinutes = DEFAULT_TTL_MINUTES): SingleplayerSaveSummary[] {
  const storage = getSessionStorage();
  if (!storage) return [];

  pruneExpired(storage, ttlMinutes);

  const summaries: SingleplayerSaveSummary[] = [];
  for (let idx = 0; idx < storage.length; idx += 1) {
    const key = storage.key(idx);
    if (!key || !key.startsWith(STORAGE_PREFIX)) continue;
    const encoded = storage.getItem(key);
    if (!encoded) continue;
    const parsed = decodePayload<SingleplayerSave>(encoded);
    if (!parsed) continue;
    if (ownerId && parsed.ownerId && parsed.ownerId !== ownerId) continue;
    summaries.push(buildSummary(parsed, encoded));
  }

  return summaries.sort((a, b) => b.updatedAt - a.updatedAt);
}

export function getSingleplayerSave(roomId: string): SingleplayerSave | null {
  const storage = getSessionStorage();
  if (!storage) return null;
  const encoded = storage.getItem(`${STORAGE_PREFIX}${roomId}`);
  return decodePayload<SingleplayerSave>(encoded);
}

export function removeSingleplayerSave(roomId: string): void {
  const storage = getSessionStorage();
  if (!storage) return;
  storage.removeItem(`${STORAGE_PREFIX}${roomId}`);
}
