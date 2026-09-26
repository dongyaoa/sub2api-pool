import { intelligenceMonitorAPI, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'

const MAX_IN_FLIGHT = 4
const MAX_CACHE_ENTRIES = 64
const MAX_CACHE_BYTES = 8 * 1024 * 1024

type CacheEntry = { runID: number; html: string }
type Consumer = { resolve: (html: string) => void; reject: (error: unknown) => void; cleanup: () => void }
type RequestEntry = {
  key: string
  run: IntelligenceRun
  namespace: number
  controller: AbortController
  consumers: Set<Consumer>
  started: boolean
}

const cache = new Map<string, CacheEntry>()
const requests = new Map<string, RequestEntry>()
const queue: RequestEntry[] = []
let inFlight = 0
let cacheBytes = 0
let authorization: string | null | undefined
let namespace = 0

function abortError() { return new DOMException('The operation was aborted', 'AbortError') }

function removeCache(key: string) {
  const entry = cache.get(key)
  if (!entry) return
  cache.delete(key)
  cacheBytes -= entry.html.length * 2
}

function putCache(entry: RequestEntry, html: string) {
  removeCache(entry.key)
  cache.set(entry.key, { runID: entry.run.id, html })
  cacheBytes += html.length * 2
  while (cache.size > MAX_CACHE_ENTRIES || cacheBytes > MAX_CACHE_BYTES) {
    const oldest = cache.keys().next().value
    if (!oldest) break
    removeCache(oldest)
  }
}

function touchCache(key: string) {
  const entry = cache.get(key)
  if (!entry) return undefined
  cache.delete(key)
  cache.set(key, entry)
  return entry.html
}

function forgetRequest(entry: RequestEntry) {
  // A replacement for the same run may already be queued after cancellation.
  if (requests.get(entry.key) === entry) requests.delete(entry.key)
}

function cancelEntry(entry: RequestEntry) {
  entry.controller.abort()
  forgetRequest(entry)
  if (!entry.started) {
    const index = queue.indexOf(entry)
    if (index >= 0) queue.splice(index, 1)
  }
}

function currentNamespace() {
  const token = localStorage.getItem('auth_token')
  if (authorization !== token) {
    authorization = token
    namespace++
    clearIntelligenceArtworkCache()
    // Neither cached HTML nor an old session's queued requests cross logins.
    for (const entry of [...requests.values()]) {
      cancelEntry(entry)
      for (const consumer of entry.consumers) {
        consumer.cleanup()
        consumer.reject(abortError())
      }
      entry.consumers.clear()
    }
  }
  return namespace
}

function drain() {
  while (inFlight < MAX_IN_FLIGHT && queue.length) {
    const entry = queue.shift()!
    if (!entry.consumers.size || entry.controller.signal.aborted) {
      forgetRequest(entry)
      continue
    }
    entry.started = true
    inFlight++
    void runRequest(entry)
  }
}

async function runRequest(entry: RequestEntry) {
  try {
    const result = await intelligenceMonitorAPI.detail(entry.run.id, entry.controller.signal)
    if (entry.controller.signal.aborted || entry.namespace !== currentNamespace()) throw abortError()
    const html = typeof result.html === 'string' ? result.html.trim() : ''
    if (result.id !== entry.run.id || result.status !== 'succeeded' || !html) throw new Error('Artwork detail is not ready')
    putCache(entry, html)
    for (const consumer of entry.consumers) consumer.resolve(html)
  } catch (error) {
    for (const consumer of entry.consumers) consumer.reject(error)
  } finally {
    for (const consumer of entry.consumers) consumer.cleanup()
    entry.consumers.clear()
    forgetRequest(entry)
    inFlight--
    drain()
  }
}

/** Load a completed run; share requests and cache successful HTML for this login. */
export function loadIntelligenceArtwork(run: IntelligenceRun, signal?: AbortSignal): Promise<string> {
  if (signal?.aborted) return Promise.reject(abortError())
  if (run.status !== 'succeeded') return Promise.reject(new Error('Artwork is not complete'))
  const scope = currentNamespace()
  const key = `${scope}:${run.id}:${run.status}:${run.finished_at || ''}`
  const cached = touchCache(key)
  if (cached !== undefined) return Promise.resolve(cached)

  let entry = requests.get(key)
  if (!entry || entry.controller.signal.aborted) {
    entry = { key, run, namespace: scope, controller: new AbortController(), consumers: new Set(), started: false }
    requests.set(key, entry)
    queue.push(entry)
  }
  const request = entry
  return new Promise<string>((resolve, reject) => {
    const abort = () => {
      if (!request.consumers.delete(consumer)) return
      consumer.cleanup()
      reject(abortError())
      if (!request.consumers.size) cancelEntry(request)
      drain()
    }
    const consumer: Consumer = { resolve, reject, cleanup: () => signal?.removeEventListener('abort', abort) }
    request.consumers.add(consumer)
    signal?.addEventListener('abort', abort, { once: true })
    drain()
  })
}

export function clearIntelligenceArtworkCache(runID?: number) {
  if (runID === undefined) {
    cache.clear()
    cacheBytes = 0
    return
  }
  for (const [key, entry] of cache) if (entry.runID === runID) removeCache(key)
}

export const intelligenceArtworkLoaderLimits = { maxInFlight: MAX_IN_FLIGHT, maxCacheEntries: MAX_CACHE_ENTRIES, maxCacheBytes: MAX_CACHE_BYTES }
