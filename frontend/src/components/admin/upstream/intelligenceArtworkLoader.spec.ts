import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'

const detail = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail } }))
const run = (id: number, changes: Partial<IntelligenceRun> = {}) => ({ id, status: 'succeeded', ...changes } as IntelligenceRun)
const artwork = (id: number, html = '<svg></svg>') => run(id, { html })
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let loader: typeof import('./intelligenceArtworkLoader')
beforeEach(async () => {
  vi.resetModules()
  detail.mockReset()
  localStorage.setItem('auth_token', 'test-session')
  loader = await import('./intelligenceArtworkLoader')
})

describe('intelligence artwork request scheduling', () => {
  it('starts immediately, limits actual requests to four and drains the next queued request', async () => {
    const responses = Array.from({ length: 5 }, () => deferred<IntelligenceRun>())
    detail.mockImplementation((id: number) => responses[id - 1]!.promise)
    const requests = responses.map((_, i) => loader.loadIntelligenceArtwork(run(i + 1)))
    expect(detail.mock.calls.map(call => call[0])).toEqual([1, 2, 3, 4])
    responses[0]!.resolve(artwork(1))
    await flushPromises()
    expect(detail.mock.calls.map(call => call[0])).toEqual([1, 2, 3, 4, 5])
    responses.slice(1).forEach((response, index) => response.resolve(artwork(index + 2)))
    await expect(Promise.all(requests)).resolves.toHaveLength(5)
  })

  it('shares a request without cancelling remaining consumers and removes their abort listeners', async () => {
    const response = deferred<IntelligenceRun>()
    detail.mockReturnValue(response.promise)
    const first = new AbortController(), second = new AbortController()
    const removeFirst = vi.spyOn(first.signal, 'removeEventListener')
    const removeSecond = vi.spyOn(second.signal, 'removeEventListener')
    const a = loader.loadIntelligenceArtwork(run(1), first.signal)
    const b = loader.loadIntelligenceArtwork(run(1), second.signal)
    const aborted = expect(a).rejects.toMatchObject({ name: 'AbortError' })
    first.abort()
    await aborted
    expect(detail).toHaveBeenCalledTimes(1)
    expect((detail.mock.calls[0]![1] as AbortSignal).aborted).toBe(false)
    response.resolve(artwork(1))
    await expect(b).resolves.toBe('<svg></svg>')
    expect(removeFirst).toHaveBeenCalledTimes(1)
    expect(removeSecond).toHaveBeenCalledTimes(1)
  })

  it('re-enters an aborted run immediately and does not let its late completion delete or cache over the replacement', async () => {
    const old = deferred<IntelligenceRun>(), current = deferred<IntelligenceRun>()
    detail.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const controller = new AbortController()
    const first = loader.loadIntelligenceArtwork(run(1), controller.signal)
    const aborted = expect(first).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await aborted
    const replacement = loader.loadIntelligenceArtwork(run(1))
    expect(detail).toHaveBeenCalledTimes(2)
    old.resolve(artwork(1, '<svg>obsolete</svg>'))
    await flushPromises()
    const sharedReplacement = loader.loadIntelligenceArtwork(run(1))
    expect(detail).toHaveBeenCalledTimes(2)
    current.resolve(artwork(1, '<svg>current</svg>'))
    await expect(Promise.all([replacement, sharedReplacement])).resolves.toEqual(['<svg>current</svg>', '<svg>current</svg>'])
    await expect(loader.loadIntelligenceArtwork(run(1))).resolves.toBe('<svg>current</svg>')
  })

  it('removes queued consumers before they start', async () => {
    const responses = Array.from({ length: 4 }, () => deferred<IntelligenceRun>())
    detail.mockImplementation((id: number) => responses[id - 1]!.promise)
    const requests = responses.map((_, i) => loader.loadIntelligenceArtwork(run(i + 1)))
    const controller = new AbortController()
    const queued = loader.loadIntelligenceArtwork(run(5), controller.signal)
    const aborted = expect(queued).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await aborted
    responses.forEach((response, index) => response.resolve(artwork(index + 1)))
    await Promise.all(requests)
    expect(detail).toHaveBeenCalledTimes(4)
  })

  it.each([
    ['empty HTML', artwork(1, '   ')],
    ['pending result', run(1, { status: 'pending', html: '<svg></svg>' })],
    ['failed result', run(1, { status: 'failed', html: '<svg></svg>' })],
    ['wrong run', artwork(2)],
  ])('does not cache %s and permits a retry', async (_, invalid) => {
    detail.mockResolvedValueOnce(invalid).mockResolvedValueOnce(artwork(1))
    await expect(loader.loadIntelligenceArtwork(run(1))).rejects.toThrow()
    await expect(loader.loadIntelligenceArtwork(run(1))).resolves.toBe('<svg></svg>')
    expect(detail).toHaveBeenCalledTimes(2)
  })

  it('retries network failures and removes listeners when requests fail', async () => {
    detail.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(artwork(1))
    const controller = new AbortController()
    const remove = vi.spyOn(controller.signal, 'removeEventListener')
    await expect(loader.loadIntelligenceArtwork(run(1), controller.signal)).rejects.toThrow('offline')
    expect(remove).toHaveBeenCalledTimes(1)
    await expect(loader.loadIntelligenceArtwork(run(1))).resolves.toBe('<svg></svg>')
  })

  it('reuses completed HTML across metadata updates, but rejects an already cancelled consumer', async () => {
    detail.mockResolvedValue(artwork(1))
    await loader.loadIntelligenceArtwork(run(1))
    await loader.loadIntelligenceArtwork(run(1, { duration_ms: 1000, http_status: 200 }))
    expect(detail).toHaveBeenCalledTimes(1)
    const controller = new AbortController()
    controller.abort()
    await expect(loader.loadIntelligenceArtwork(run(1), controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    loader.clearIntelligenceArtworkCache(1)
    await loader.loadIntelligenceArtwork(run(1))
    expect(detail).toHaveBeenCalledTimes(2)
  })

  it('separates cached HTML and pending requests when the signed-in authorization changes', async () => {
    detail.mockResolvedValueOnce(artwork(1, '<svg>account-a</svg>'))
    await loader.loadIntelligenceArtwork(run(1))
    const old = deferred<IntelligenceRun>()
    detail.mockReturnValueOnce(old.promise)
    const pending = loader.loadIntelligenceArtwork(run(2))
    const aborted = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    localStorage.setItem('auth_token', 'different-account')
    detail.mockResolvedValueOnce(artwork(1, '<svg>account-b</svg>'))
    await expect(loader.loadIntelligenceArtwork(run(1))).resolves.toBe('<svg>account-b</svg>')
    await aborted
    expect((detail.mock.calls[1]![1] as AbortSignal).aborted).toBe(true)
    old.resolve(artwork(2, '<svg>old-account</svg>'))
    await flushPromises()
    detail.mockResolvedValueOnce(artwork(2, '<svg>new-account</svg>'))
    await expect(loader.loadIntelligenceArtwork(run(2))).resolves.toBe('<svg>new-account</svg>')
  })

  it('bounds cache memory using two bytes per UTF-16 code unit', async () => {
    const halfCapacity = '鹈'.repeat(loader.intelligenceArtworkLoaderLimits.maxCacheBytes / 4 + 1)
    detail.mockImplementation((id: number) => Promise.resolve(artwork(id, halfCapacity)))
    await loader.loadIntelligenceArtwork(run(1))
    await loader.loadIntelligenceArtwork(run(2))
    await loader.loadIntelligenceArtwork(run(2))
    expect(detail).toHaveBeenCalledTimes(2)
    await loader.loadIntelligenceArtwork(run(1))
    expect(detail).toHaveBeenCalledTimes(3)
  })
})
