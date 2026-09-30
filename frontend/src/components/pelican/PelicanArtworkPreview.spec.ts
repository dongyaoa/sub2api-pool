import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { PelicanRun } from '@/api/pelicanMonitor'
import PelicanArtworkPreview from './PelicanArtworkPreview.vue'
import { pelicanPageActiveKey } from './pelicanContext'
import { resetPelicanArtworkAccess } from './pelicanArtworkLoader'
const detail = vi.hoisted(() => vi.fn())
vi.mock('@/api/pelicanMonitor', () => ({ pelicanMonitorAPI: { detail } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const run = (changes: Partial<PelicanRun> = {}) => ({ id: 1, plan_id: 7, status: 'succeeded', ...changes }) as PelicanRun
let wrapper: VueWrapper | undefined
let host: HTMLElement | undefined
beforeEach(() => { vi.resetAllMocks(); resetPelicanArtworkAccess(); localStorage.setItem('auth_token', 'user-session'); vi.stubGlobal('IntersectionObserver', undefined) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; host?.remove(); host = undefined; vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
function render(active = ref(true)) { host = document.createElement('div'); document.body.append(host); wrapper = mount(PelicanArtworkPreview, { attachTo: host, props: { run: run() }, global: { stubs: { Icon: true }, provide: { [pelicanPageActiveKey as symbol]: active } } }); return wrapper }
describe('safe user artwork previews', () => {
  it('opens a completed artwork before its thumbnail detail has loaded', async () => {
    let finish!: (value: PelicanRun) => void
    detail.mockImplementation(() => new Promise<PelicanRun>(resolve => { finish = resolve }))
    const view = render(); await flushPromises()
    expect(view.find('iframe').exists()).toBe(false)
    const open = view.get('button[aria-label="pelicanMonitor.open"]')
    expect(open.attributes('aria-haspopup')).toBe('dialog')
    await open.trigger('click')
    expect(view.emitted('open')).toEqual([[]])
    finish(run({ html: '<svg>Completed artwork</svg>' })); await flushPromises()
    expect(view.get('iframe').attributes('srcdoc')).toContain('Completed artwork')
  })

  it.each(['pending', 'running', 'failed'] as const)('does not open a %s generation', async status => {
    const view = render(ref(false))
    await view.setProps({ run: run({ status }) }); await flushPromises()
    expect(view.find('button[aria-label="pelicanMonitor.open"]').exists()).toBe(false)
    await view.trigger('click')
    expect(view.emitted('open')).toBeUndefined()
    expect(detail).not.toHaveBeenCalled()
  })

  it('keeps opening available after a thumbnail load error and still allows retry', async () => {
    detail.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(run({ html: '<svg>Recovered artwork</svg>' }))
    const view = render(); await flushPromises()
    expect(view.text()).toContain('pelicanMonitor.loadFailed')
    await view.get('button[aria-label="pelicanMonitor.open"]').trigger('click')
    expect(view.emitted('open')).toEqual([[]])
    const retry = view.findAll('button').find(button => button.text() === 'pelicanMonitor.refresh')
    expect(retry).toBeDefined()
    await retry!.trigger('click'); await flushPromises()
    expect(view.get('iframe').attributes('srcdoc')).toContain('Recovered artwork')
  })

  it('loads only the user API into isolated frames, strips remote elements, and retains iframe across metadata refreshes', async () => {
    detail.mockResolvedValue(run({ html: '<svg><text>Artwork</text></svg><script src="https://example.test/x.js"></script><form action="https://example.test/pay">form</form>' }))
    const view = render(); await flushPromises()
    const frame = view.get('iframe')
    expect(frame.attributes('sandbox')).toBe('allow-scripts')
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes()).toHaveProperty('credentialless')
    expect(frame.attributes('srcdoc')).toContain('Artwork')
    expect(frame.attributes('srcdoc')).not.toContain('https://example.test')
    expect(frame.attributes('srcdoc')).toContain('connect-src')
    await view.setProps({ run: run({ duration_ms: 1000 }) }); await flushPromises()
    expect(view.get('iframe').element).toBe(frame.element)
    expect(detail).toHaveBeenCalledTimes(1)
  })
  it('pauses hidden panels, cancels pending requests and loads again when visible', async () => {
    detail.mockImplementationOnce((_id: number, signal: AbortSignal) => new Promise((_, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))))).mockResolvedValueOnce(run({ html: '<svg>Visible</svg>' }))
    const active = ref(false), view = render(active); await flushPromises()
    expect(detail).not.toHaveBeenCalled()
    active.value = true; await flushPromises()
    expect(detail).toHaveBeenCalledTimes(1)
    active.value = false; await flushPromises()
    expect((detail.mock.calls[0]![1] as AbortSignal).aborted).toBe(true)
    active.value = true; await flushPromises()
    expect(view.get('iframe').attributes('srcdoc')).toContain('Visible')
  })
  it('never displays backend debug errors or source metadata', async () => {
    const view = render(ref(false))
    await view.setProps({ run: run({ status: 'failed', error: 'PRIVATE_API_KEY_AND_UPSTREAM' }) })
    expect(view.text()).not.toContain('PRIVATE_API_KEY_AND_UPSTREAM')
    expect(view.text()).toContain('pelicanMonitor.status.failed')
    expect(detail).not.toHaveBeenCalled()
  })

  it('plays all artworks without hover or intersection and resumes the same frame after the page is hidden', async () => {
    vi.stubGlobal('IntersectionObserver', class {
      observe() {}
      disconnect() {}
    })
    detail.mockResolvedValue(run({ html: '<svg>Autoplay artwork</svg>' }))
    const view = render()
    await flushPromises()
    const frame = view.get<HTMLIFrameElement>('iframe')
    const playback = vi.spyOn(frame.element.contentWindow!, 'postMessage')
    await frame.trigger('load')
    expect(playback).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    await view.trigger('mouseenter'); await view.trigger('mouseleave'); await view.trigger('focusout')
    expect(playback.mock.calls.some(([message]) => message.playing === false)).toBe(false)
    await view.setProps({ run: run({ duration_ms: 1234 }) }); await flushPromises()
    expect(view.get('iframe').element).toBe(frame.element)
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(playback).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(playback).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    expect(view.get('iframe').element).toBe(frame.element)
    expect(detail).toHaveBeenCalledTimes(1)
  })

  it('recovers a temporarily unavailable completed artwork without manual refresh', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    detail.mockRejectedValueOnce(new Error('temporary')).mockResolvedValueOnce(run({ html: '<svg>Recovered automatically</svg>' }))
    const view = render(); await flushPromises()
    expect(view.text()).toContain('pelicanMonitor.loadFailed')
    vi.advanceTimersByTime(1000); await flushPromises()
    expect(view.get('iframe').attributes('srcdoc')).toContain('Recovered automatically')
    expect(detail).toHaveBeenCalledTimes(2)
  })

  it('bounds retries and cancels scheduled attempts when hidden or unmounted', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    detail.mockRejectedValue(new Error('unavailable'))
    const active = ref(true), view = render(active); await flushPromises()
    active.value = false; await flushPromises()
    vi.advanceTimersByTime(10000); await flushPromises()
    expect(detail).toHaveBeenCalledTimes(1)
    active.value = true; await flushPromises()
    for (const delay of [1000, 2000, 4000, 30000]) { vi.advanceTimersByTime(delay); await flushPromises() }
    const attempts = detail.mock.calls.length
    expect(attempts).toBeLessThanOrEqual(5)
    vi.advanceTimersByTime(60000); await flushPromises()
    expect(detail).toHaveBeenCalledTimes(attempts)
    await view.findAll('button').find(button => button.text() === 'pelicanMonitor.refresh')!.trigger('click'); await flushPromises()
    const afterManual = detail.mock.calls.length
    view.unmount(); wrapper = undefined
    vi.advanceTimersByTime(60000); await flushPromises()
    expect(detail).toHaveBeenCalledTimes(afterManual)
  })
})
