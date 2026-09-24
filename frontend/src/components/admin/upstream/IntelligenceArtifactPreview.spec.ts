import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import PelicanLoadingScene from './PelicanLoadingScene.vue'

const detail = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const run = (id: number, status: IntelligenceRun['status'], changes: Partial<IntelligenceRun> = {}): IntelligenceRun => ({ id, status, ...changes } as IntelligenceRun)
let wrapper: VueWrapper | undefined
function render(value: IntelligenceRun, large = false) {
  wrapper = mount(IntelligenceArtifactPreview, { props: { run: value, large }, global: { stubs: { Icon: true } } })
  return wrapper
}
beforeEach(() => { vi.resetAllMocks(); vi.stubGlobal('IntersectionObserver', undefined) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })

describe('intelligence artifact preview states', () => {
  it.each(['pending', 'running'] as const)('shows a cycling pelican with the actual %s status without fetching a result', async status => {
    const view = render(run(1, status))
    await flushPromises()
    expect(view.getComponent(PelicanLoadingScene).props()).toEqual({ label: `intelligenceMonitor.status.${status}`, queued: status === 'pending' })
    expect(view.get('[role="status"]').text()).toContain(`intelligenceMonitor.status.${status}`)
    expect(view.attributes('aria-busy')).toBe('true')
    expect(view.find('.animate-spin').exists()).toBe(false)
    expect(view.find('iframe').exists()).toBe(false)
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
  })

  it('autoplays sanitized CSS and SMIL in a compact card and provides a focusable full-preview open target', async () => {
    const html = '<style>@keyframes cycle{to{transform:rotate(360deg)}}.wheel{animation:cycle 2s linear infinite}</style><svg><circle class="wheel"><animate attributeName="opacity" values="1;.5;1" dur="1s" repeatCount="indefinite" /></circle></svg><script>alert(1)</script>'
    const view = render(run(2, 'succeeded', { html }))
    await flushPromises()
    const frame = view.get('iframe')
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('srcdoc')).toContain('@keyframes cycle')
    expect(frame.attributes('srcdoc')).toContain('<animate')
    expect(frame.attributes('srcdoc')).toContain("script-src 'none'")
    expect(frame.attributes('srcdoc')).not.toContain('<script>')
    expect(view.find('[role="note"]').exists()).toBe(false)
    const open = view.get('button[aria-label="intelligenceMonitor.open"]')
    expect(open.classes()).toContain('preview-open')
    expect(open.attributes('tabindex')).not.toBe('-1')
    expect(open.get('span').classes()).toContain('preview-open-label')
    await open.trigger('click')
    expect(view.emitted('open')).toEqual([[]])
  })

  it('keeps a failed generation compact and never displays it as still generating', async () => {
    const view = render(run(3, 'failed', { error: 'The upstream request timed out.' }))
    await flushPromises()
    expect(view.findComponent(PelicanLoadingScene).exists()).toBe(false)
    expect(view.text()).toContain('intelligenceMonitor.status.failed')
    expect(view.get('[title="The upstream request timed out."]').classes()).toContain('line-clamp-2')
    expect(detail).not.toHaveBeenCalled()
  })
})
