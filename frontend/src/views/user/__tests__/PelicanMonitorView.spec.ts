import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PelicanMonitorGroup, PelicanMonitorSnapshot, PelicanRun } from '@/api/pelicanMonitor'
const state = vi.hoisted(() => ({ list: vi.fn(), artwork: vi.fn(), auth: { token: 'session', isAuthenticated: true }, settings: {} as Record<string, unknown>, reset: vi.fn(), retain: vi.fn() }))
vi.mock('@/api/pelicanMonitor', () => ({ pelicanMonitorAPI: { list: state.list } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/stores/pelicanMonitor', () => ({ usePelicanMonitorStore: () => state.settings }))
vi.mock('@/components/pelican/pelicanArtworkLoader', () => ({ loadPelicanArtwork: state.artwork, resetPelicanArtworkAccess: state.reset, retainPelicanArtworks: state.retain }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en'), t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${Object.values(values).join(',')}` : key }) }))
import PelicanMonitorView from '../PelicanMonitorView.vue'
const run = { id: 8, plan_id: 2, status: 'succeeded', model: 'gpt-6-astra', reasoning_effort: 'high', created_at: '2026-09-30T00:00:00Z', started_at: '2026-09-30T00:00:00Z', duration_ms: 125000 } as PelicanRun
const group = { id: 2, group_name: 'Visible group', group_rate_multiplier: 0.8, enabled: true, interval_seconds: 300, latest_run: run, recent_runs: [run], next_run_at: '2030-01-01T00:00:00Z' } as PelicanMonitorGroup
const config = { enabled: true, title: '', description: '', notice: '' }
const snapshot = (items = [group], enabled = true) => ({ config: { ...config, enabled }, server_time: new Date().toISOString(), items }) as PelicanMonitorSnapshot
let wrapper: VueWrapper | undefined
let host: HTMLElement | undefined
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] }); vi.resetAllMocks()
  state.auth = reactive({ token: 'session', isAuthenticated: true })
  state.settings = reactive({ config: { ...config }, enabled: true, apply(value: typeof config) { state.settings.config = value; state.settings.enabled = value.enabled }, disable() { state.settings.enabled = false } })
  state.list.mockResolvedValue(snapshot())
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; host?.remove(); host = undefined; vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
function render() {
  wrapper = mount(PelicanMonitorView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, PelicanArtworkPreview: { props: ['run'], emits: ['open'], template: '<button data-testid="artwork" @click="$emit(\'open\')">artwork</button>' }, BaseDialog: { props: ['show', 'title'], template: '<section><h3>{{ title }}</h3><slot /></section>' } } } })
  return wrapper
}
function renderWithRealArtwork() {
  vi.stubGlobal('IntersectionObserver', undefined)
  host = document.createElement('div'); document.body.append(host)
  wrapper = mount(PelicanMonitorView, { attachTo: host, global: { stubs: { Icon: true } } })
  return wrapper
}
describe('read-only user pelican page', () => {
  it('automatically inserts new runs and updates their status without restarting old artwork', async () => {
    state.artwork.mockImplementation(async (item: PelicanRun) => `<svg>Artwork ${item.id}</svg>`)
    const view = renderWithRealArtwork(); await flushPromises()
    const oldFrame = view.get<HTMLIFrameElement>('iframe').element
    const playback = vi.spyOn(oldFrame.contentWindow!, 'postMessage')
    await view.get('.preview-open').trigger('click'); await flushPromises()
    oldFrame.dispatchEvent(new Event('load')); await flushPromises()
    expect(playback).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    const dialog = document.querySelector('[role="dialog"]')
    const gallery = view.get<HTMLElement>('[data-testid="pelican-gallery"]')
    gallery.element.scrollLeft = 180
    const next = { ...run, id: 9, status: 'pending' } as PelicanRun
    state.list.mockResolvedValue(snapshot([{ ...group, latest_run: next, recent_runs: [run] }]))
    vi.advanceTimersByTime(1500); await flushPromises()
    expect(view.findAll('.pelican-work').map(item => item.attributes('data-run-id'))).toEqual(['9', '8'])
    expect(view.get('.pelican-work').text()).toContain('pelicanMonitor.latest')
    expect(view.get('.pelican-work [role="img"]').attributes('aria-label')).toBe('pelicanMonitor.status.pending')
    expect(gallery.element.scrollLeft).toBe(0)

    state.list.mockResolvedValue(snapshot([{ ...group, latest_run: { ...next, status: 'running' }, recent_runs: [run] }]))
    vi.advanceTimersByTime(1500); await flushPromises()
    expect(view.get('.pelican-work [role="img"]').attributes('aria-label')).toBe('pelicanMonitor.status.running')
    state.list.mockResolvedValue(snapshot([{ ...group, latest_run: { ...next, status: 'succeeded' }, recent_runs: [run] }]))
    vi.advanceTimersByTime(1500); await flushPromises()
    expect(view.get('.pelican-work iframe').attributes('srcdoc')).toContain('Artwork 9')
    expect(view.get('.pelican-work [role="img"]').classes()).toContain('bg-emerald-500')
    expect(view.get('[data-run-id="8"] iframe').element).toBe(oldFrame)
    expect(document.querySelector('[role="dialog"]')).toBe(dialog)
  })

  it('labels the latest failed run and distinguishes historical status with colored dots', async () => {
    state.list.mockResolvedValue(snapshot([{ ...group, latest_run: { ...run, id: 9, status: 'failed' }, recent_runs: [run] }]))
    const view = render(); await flushPromises()
    const items = view.findAll('.pelican-work')
    expect(items[0]!.text()).toContain('pelicanMonitor.latest')
    expect(items[0]!.get('[role="img"]').classes()).toContain('bg-rose-500')
    expect(items[0]!.get('[role="img"]').attributes('aria-label')).toBe('pelicanMonitor.status.failed')
    expect(items[1]!.text()).toContain('#8')
    expect(items[1]!.get('[role="img"]').classes()).toContain('bg-emerald-500')
    expect(items[0]!.get('time').text()).toContain('2026')
  })

  it('opens unfinished thumbnail loading in a real dialog, plays the result and preserves it across refreshes', async () => {
    let finishArtwork!: (html: string) => void
    const artwork = new Promise<string>(resolve => { finishArtwork = resolve })
    state.artwork.mockReturnValue(artwork)
    const view = renderWithRealArtwork(); await flushPromises()
    expect(view.find('iframe').exists()).toBe(false)
    const thumbnailOpen = view.get<HTMLButtonElement>('button[aria-haspopup="dialog"]')
    thumbnailOpen.element.focus()
    await thumbnailOpen.trigger('click'); await flushPromises()
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog!.textContent).toContain('Visible group')
    expect(dialog!.textContent).toContain('pelicanMonitor.loadingPreview')

    finishArtwork('<svg><text>Animated pelican</text><animate attributeName="opacity" values="1;.5;1" dur="1s" repeatCount="indefinite" /></svg>')
    await flushPromises()
    const frame = dialog!.querySelector<HTMLIFrameElement>('iframe')!
    expect(frame.getAttribute('srcdoc')).toContain('Animated pelican')
    expect(frame.getAttribute('srcdoc')).toContain('<animate')
    const playback = vi.spyOn(frame.contentWindow!, 'postMessage')
    frame.dispatchEvent(new Event('load')); await flushPromises()
    expect(playback).toHaveBeenCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    expect(dialog!.textContent).toContain('2m 5s')

    state.list.mockResolvedValue(snapshot([{ ...group, latest_run: { ...run, duration_ms: 126000 } }]))
    await view.get('[data-testid="pelican-refresh"]').trigger('click'); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBe(dialog)
    expect(dialog!.querySelector('iframe')).toBe(frame)
    expect(dialog!.textContent).toContain('2m 6s')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(thumbnailOpen.element)
    expect(document.body.classList.contains('modal-open')).toBe(false)

    const informationOpen = view.get<HTMLButtonElement>('.pelican-work > button')
    informationOpen.element.focus()
    await informationOpen.trigger('click'); await flushPromises()
    const reopened = document.querySelector<HTMLElement>('[role="dialog"]')!
    expect(reopened.querySelector('iframe')!.getAttribute('srcdoc')).toContain('Animated pelican')
    reopened.querySelector<HTMLButtonElement>('button[aria-label="Close modal"]')!.click(); await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(informationOpen.element)
  })

  it('shows group rate and artwork, with no private metadata or admin controls', async () => {
    state.list.mockResolvedValue(snapshot([{ ...group, api_key: 'PRIVATE_KEY', source_name: 'PRIVATE_UPSTREAM', latest_run: { ...run, source_endpoint: 'https://private.example' } } as PelicanMonitorGroup]))
    const view = render(); await flushPromises()
    expect(view.text()).toContain('Visible group'); expect(view.text()).toContain('0.8×')
    expect(view.text()).toContain('pelicanMonitor.everyMinutes:5')
    await view.get('[data-testid="artwork"]').trigger('click'); await flushPromises()
    expect(view.text()).toContain('2m 5s')
    expect(view.text()).not.toMatch(/PRIVATE_|private\.example|API Key|HTML 源码|立即测试/)
    expect(view.find('[data-testid="candy-monitor"]').exists()).toBe(false)
    const buttons = view.findAll('button').map(button => button.text()).join(' ')
    expect(buttons).not.toMatch(/edit|delete|download|runCandy|admin\//)
  })
  it('removes existing groups and enlarged artwork when the feature is disabled', async () => {
    const view = render(); await flushPromises(); await view.get('[data-testid="artwork"]').trigger('click')
    state.list.mockResolvedValue(snapshot([], false))
    await view.get('[data-testid="pelican-refresh"]').trigger('click'); await flushPromises()
    expect(view.find('[data-testid="pelican-disabled"]').exists()).toBe(true)
    expect(view.find('[data-testid="pelican-group"]').exists()).toBe(false)
    expect(view.text()).not.toContain('Visible group')
    expect(state.reset).toHaveBeenCalled()
    const count = state.list.mock.calls.length
    vi.advanceTimersByTime(15000); await flushPromises()
    expect(state.list).toHaveBeenCalledTimes(count)
  })
  it('keeps one page poll, pauses while hidden and stops after unmount', async () => {
    state.list.mockResolvedValue(snapshot(Array.from({ length: 8 }, (_, index) => ({ ...group, id: index + 1 }))))
    const view = render(); await flushPromises()
    expect(state.list).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(5000); await flushPromises(); expect(state.list).toHaveBeenCalledTimes(2)
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange')); vi.advanceTimersByTime(15000); await flushPromises()
    expect(state.list).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(false); document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(state.list).toHaveBeenCalledTimes(3)
    view.unmount(); wrapper = undefined; vi.advanceTimersByTime(15000); await flushPromises()
    expect(state.list).toHaveBeenCalledTimes(3)
  })
  it('closes selected artwork when its group is removed and hides data after denied access', async () => {
    const view = render(); await flushPromises(); await view.get('[data-testid="artwork"]').trigger('click')
    state.list.mockResolvedValue(snapshot([])); await view.get('[data-testid="pelican-refresh"]').trigger('click'); await flushPromises()
    expect(view.text()).not.toContain('Visible group')
    state.list.mockRejectedValue({ response: { status: 403 } }); await view.get('[data-testid="pelican-refresh"]').trigger('click'); await flushPromises()
    expect(view.find('[data-testid="pelican-disabled"]').exists()).toBe(true)
  })

  it('rejects a delayed list from the previous account while starting the new session immediately', async () => {
    let finishOld!: (value: PelicanMonitorSnapshot) => void
    const old = new Promise<PelicanMonitorSnapshot>(resolve => { finishOld = resolve })
    state.list.mockReturnValueOnce(old).mockResolvedValueOnce(snapshot([{ ...group, group_name: 'New account group' }]))
    const view = render(); await flushPromises()
    const oldSignal = state.list.mock.calls[0]![0] as AbortSignal
    state.auth.token = 'new-session'; await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(state.list).toHaveBeenCalledTimes(2)
    expect(view.text()).toContain('New account group')
    finishOld(snapshot([{ ...group, group_name: 'PRIVATE_OLD_ACCOUNT_GROUP' }])); await flushPromises()
    expect(view.text()).not.toContain('PRIVATE_OLD_ACCOUNT_GROUP')
    expect(view.text()).toContain('New account group')
  })
})
