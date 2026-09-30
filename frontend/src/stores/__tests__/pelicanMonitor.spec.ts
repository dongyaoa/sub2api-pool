import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const state = vi.hoisted(() => ({ auth: { token: null as string | null, isAuthenticated: false }, config: vi.fn(), reset: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/api/pelicanMonitor', () => ({ pelicanMonitorAPI: { config: state.config } }))
vi.mock('@/components/pelican/pelicanArtworkLoader', () => ({ resetPelicanArtworkAccess: state.reset }))
import { usePelicanMonitorStore } from '../pelicanMonitor'
beforeEach(() => { setActivePinia(createPinia()); state.auth = reactive({ token: null, isAuthenticated: false }); vi.resetAllMocks() })
describe('pelican sidebar config gate', () => {
  it('is hidden by default and never fetches for logged-out users', async () => {
    const store = usePelicanMonitorStore(); await store.load()
    expect(store.enabled).toBe(false); expect(state.config).not.toHaveBeenCalled()
  })
  it('shares the logged-in request, caches it, accepts live page config and resets on logout', async () => {
    let resolve!: (value: unknown) => void
    state.config.mockReturnValue(new Promise(yes => { resolve = yes }))
    state.auth.token = 'session'; state.auth.isAuthenticated = true
    const store = usePelicanMonitorStore()
    const first = store.load(), second = store.load()
    expect(state.config).toHaveBeenCalledTimes(1)
    resolve({ enabled: true, title: 'Public artwork', description: '', notice: '' }); await Promise.all([first, second]); await flushPromises()
    expect(store.enabled).toBe(true)
    await store.load(); expect(state.config).toHaveBeenCalledTimes(1)
    store.apply({ enabled: false, title: '', description: '', notice: '' })
    expect(store.enabled).toBe(false); expect(state.reset).toHaveBeenCalled()
    state.auth.token = null; state.auth.isAuthenticated = false; await flushPromises()
    expect(store.enabled).toBe(false); expect(state.config).toHaveBeenCalledTimes(1)
  })
  it('does not allow a stale config response to reopen a disabled page', async () => {
    let resolve!: (value: unknown) => void
    state.config.mockReturnValue(new Promise(yes => { resolve = yes }))
    state.auth.token = 'session'; state.auth.isAuthenticated = true
    const store = usePelicanMonitorStore()
    store.apply({ enabled: false, title: '', description: '', notice: '' })
    resolve({ enabled: true }); await flushPromises()
    expect(store.enabled).toBe(false)
  })

  it('a forced post-save load supersedes pending pre-save settings', async () => {
    let resolveOld!: (value: unknown) => void
    state.config.mockReturnValueOnce(new Promise(yes => { resolveOld = yes })).mockResolvedValueOnce({ enabled: true, title: 'Saved title', description: '', notice: '' })
    state.auth.token = 'session'; state.auth.isAuthenticated = true
    const store = usePelicanMonitorStore()
    const oldSignal = state.config.mock.calls[0]![0] as AbortSignal
    await store.load(true)
    expect(oldSignal.aborted).toBe(true)
    expect(state.config).toHaveBeenCalledTimes(2)
    expect(store.enabled).toBe(true)
    resolveOld({ enabled: false, title: 'Stale title', description: '', notice: '' }); await flushPromises()
    expect(store.enabled).toBe(true)
    expect(store.config.title).toBe('Saved title')
  })
})
