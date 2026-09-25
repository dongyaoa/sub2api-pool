import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref, type Ref } from 'vue'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import PelicanLoadingScene from './PelicanLoadingScene.vue'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'

const detail = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, number | string>) => {
  if (key === 'intelligenceMonitor.durationMinutesSeconds') return `${values?.minutes} 分 ${values?.seconds} 秒`
  if (key === 'intelligenceMonitor.seconds') return `${values?.count} 秒`
  if (key === 'intelligenceMonitor.totalDuration') return '总耗时'
  return key
} }) }))
const run = (id: number, status: IntelligenceRun['status'], changes: Partial<IntelligenceRun> = {}): IntelligenceRun => ({ id, status, ...changes } as IntelligenceRun)
let wrapper: VueWrapper | undefined
function render(value: IntelligenceRun, large = false, panelActive?: Ref<boolean>) {
  wrapper = mount(IntelligenceArtifactPreview, { attachTo: document.body, props: { run: value, large }, global: { stubs: { Icon: true }, provide: panelActive ? { [intelligencePanelActiveKey as symbol]: panelActive } : {} } })
  return wrapper
}
beforeEach(() => { vi.resetAllMocks(); vi.stubGlobal('IntersectionObserver', undefined) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })

describe('intelligence artifact preview states', () => {
  it.each(['pending', 'running'] as const)('shows a pelican with the actual %s status without fetching a result', async status => {
    const view = render(run(1, status))
    await flushPromises()
    expect(view.getComponent(PelicanLoadingScene).props()).toEqual({ label: `intelligenceMonitor.status.${status}`, queued: status === 'pending' })
    expect(view.get('[role="status"]').text()).toContain(`intelligenceMonitor.status.${status}`)
    expect(view.attributes('aria-busy')).toBe('true')
    expect(view.find('.animate-spin').exists()).toBe(false)
    expect(view.find('iframe').exists()).toBe(false)
    expect(view.find('[data-testid="artwork-duration"]').exists()).toBe(false)
    expect(detail).not.toHaveBeenCalled()
  })

  it('distinguishes fetching a completed result from generating a new one and preserves the finished iframe on polling', async () => {
    let resolveDetail: (value: IntelligenceRun) => void = () => undefined
    detail.mockReturnValue(new Promise(resolve => { resolveDetail = resolve }))
    const view = render(run(1, 'running'))
    await flushPromises()
    await view.setProps({ run: run(1, 'succeeded') })
    await flushPromises()
    expect(view.findComponent(PelicanLoadingScene).exists()).toBe(false)
    expect(view.get('[role="status"]').text()).toBe('intelligenceMonitor.loadingPreview')
    expect(view.find('.preview-skeleton').exists()).toBe(true)
    expect(detail).toHaveBeenCalledTimes(1)

    resolveDetail(run(1, 'succeeded', { html: '<svg><circle><animate attributeName="opacity" values="0;1;0" dur="1s" repeatCount="indefinite" /></circle></svg>' }))
    await flushPromises()
    const originalFrame = view.get('iframe').element
    expect(view.attributes('aria-busy')).toBe('false')
    expect(view.get('iframe').attributes('srcdoc')).toContain('<animate')
    await view.setProps({ run: run(1, 'succeeded', { duration_ms: 4200 }) })
    await flushPromises()
    expect(detail).toHaveBeenCalledTimes(1)
    expect(view.get('iframe').element).toBe(originalFrame)
    expect(view.find('[data-testid="artwork-duration"]').exists()).toBe(false)
  })

  it('preserves inline CSS, SMIL and scripts in an isolated card with a focusable open target', async () => {
    const html = '<style>@keyframes cycle{to{transform:rotate(360deg)}}.wheel{animation:cycle 2s linear infinite}</style><svg><circle class="wheel"><animate attributeName="opacity" values="1;.5;1" dur="1s" repeatCount="indefinite" /></circle></svg><script>alert(1)</script>'
    const view = render(run(2, 'succeeded', { html }))
    await flushPromises()
    const frame = view.get('iframe')
    expect(frame.attributes('sandbox')).toBe('allow-scripts')
    expect(frame.attributes('srcdoc')).toContain('@keyframes cycle')
    expect(frame.attributes('srcdoc')).toContain('animate attributeName')
    expect(frame.attributes('srcdoc')).toContain("frame-src 'none'")
    expect(frame.attributes('srcdoc')).toContain('alert(1)')
    expect(view.find('[role="note"]').exists()).toBe(false)
    const open = view.get('button[aria-label="intelligenceMonitor.open"]')
    expect(open.classes()).toContain('preview-open')
    expect(open.attributes('tabindex')).not.toBe('-1')
    expect(open.get('span').classes()).toContain('preview-open-label')
    await open.trigger('click')
    expect(view.emitted('open')).toEqual([[]])
  })

  it('keeps a failed generation compact and never displays it as still generating', async () => {
    const view = render(run(3, 'failed', { error: 'The upstream request timed out.', duration_ms: 900000 }))
    await flushPromises()
    expect(view.findComponent(PelicanLoadingScene).exists()).toBe(false)
    expect(view.text()).toContain('intelligenceMonitor.status.failed')
    expect(view.get('[title="The upstream request timed out."]').classes()).toContain('line-clamp-2')
    expect(detail).not.toHaveBeenCalled()
    expect(view.find('[data-testid="artwork-duration"]').exists()).toBe(false)
  })

  it.each([false, true])('keeps duration out of the artwork canvas (large=%s)', async large => {
    const view = render(run(4, 'succeeded', { html: '<svg></svg>', duration_ms: 754000 }), large)
    await flushPromises()
    expect(view.find('[data-testid="artwork-duration"]').exists()).toBe(false)
    if (!large) {
      const open = view.get('button[aria-label="intelligenceMonitor.open"]')
      expect(open.classes()).not.toContain('pb-9')
      await open.trigger('click')
      expect(view.emitted('open')).toEqual([[]])
    }
  })

  it('plays on hover or keyboard focus and pauses on leave without rebuilding the iframe', async () => {
    const view = render(run(5, 'succeeded', { html: '<svg></svg>' }))
    await flushPromises()
    const frame = view.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    const originalDocument = frame.srcdoc
    await view.get('iframe').trigger('load')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    await view.trigger('mouseenter')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    await view.trigger('mouseleave')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    await view.get('button').trigger('focusin')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    await view.get('button').trigger('focusout')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    expect(view.get('iframe').element).toBe(frame)
    expect(frame.srcdoc).toBe(originalDocument)
  })

  it('autoplays enlarged art, pauses while the page is hidden and replays on request', async () => {
    const view = render(run(6, 'succeeded', { html: '<svg></svg>' }), true)
    await flushPromises()
    const frame = view.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    await view.get('iframe').trigger('load')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    await view.get('button[title="intelligenceMonitor.reloadPreview"]').trigger('click')
    expect(view.get('iframe').element).not.toBe(frame)
    hidden.mockRestore()
  })

  it('reveals artwork only after its own frame confirms the expected fitted size', async () => {
    const view = render(run(7, 'succeeded', { html: '<svg></svg>' }), true)
    await flushPromises()
    const frame = view.get('iframe').element as HTMLIFrameElement
    const send = (width: unknown, height: unknown, source: unknown = frame.contentWindow) => window.dispatchEvent(new MessageEvent('message', { data: { type: 'intelligence-preview-fitted', width, height }, source: source as Window }))
    expect(frame.style.opacity).toBe('0')
    send(960, 600, window)
    send(560, 600)
    send(NaN, 600)
    await flushPromises()
    expect(frame.style.opacity).toBe('0')
    send(960, 600)
    await flushPromises()
    expect(frame.style.opacity).toBe('1')
    expect(view.find('[role="status"]').exists()).toBe(false)
    await view.setProps({ run: run(7, 'succeeded', { html: '<svg></svg>', duration_ms: 1500 }) })
    expect(view.get('iframe').element).toBe(frame)
    expect(frame.style.opacity).toBe('1')
    await view.get('button[title="intelligenceMonitor.reloadPreview"]').trigger('click')
    const replayed = view.get('iframe').element as HTMLIFrameElement
    expect(replayed.style.opacity).toBe('0')
    send(960, 600)
    await flushPromises()
    expect(replayed.style.opacity).toBe('0')
  })

  it('pauses a retained preview on tab hide and resumes without recreating it', async () => {
    const active = ref(true)
    const view = render(run(8, 'succeeded', { html: '<svg></svg>' }), true, active)
    await flushPromises()
    const frame = view.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    await view.get('iframe').trigger('load')
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    active.value = false
    await flushPromises()
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: false }, '*')
    active.value = true
    await flushPromises()
    expect(view.get('iframe').element).toBe(frame)
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'intelligence-preview-playback', playing: true }, '*')
    expect(detail).not.toHaveBeenCalled()
  })
})
