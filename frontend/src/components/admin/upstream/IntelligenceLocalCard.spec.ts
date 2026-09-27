import { defineComponent, ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceLocalCard from './IntelligenceLocalCard.vue'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { count?: number }) => values?.count === undefined ? key : `${key}:${values.count}` }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail: vi.fn() } }))
const preview = defineComponent({ name: 'IntelligenceArtifactPreview', props: ['run'], emits: ['open'], template: '<button class="test-preview" @click="$emit(\'open\')">{{ run.id }}</button>' })
const run = (id: number, status: IntelligenceRun['status'] = 'succeeded') => ({ id, status, created_at: '2026-09-27T01:00:00Z', model: 'gpt-6-astra', reasoning_effort: 'high' }) as IntelligenceRun
const plan = (fields: Partial<IntelligencePlan> = {}) => ({ id: 1, name: 'My comparison', source_type: 'local_group', local_group_name: 'Primary GPT', local_group_rate_multiplier: 0.4, local_api_key_managed: false, local_api_key_name: 'My API Key', api_key_masked: 'sk-••••1234', enabled: false, interval_seconds: 300, candy_interval_seconds: 180, model: 'gpt-6-astra', reasoning_effort: 'high', latest_run: null, ...fields }) as IntelligencePlan
let view: VueWrapper | undefined
function render(value = plan(), panelActive = ref(true)) { view = mount(IntelligenceLocalCard, { props: { plan: value, busy: false }, global: { stubs: { Icon: true, IntelligenceArtifactPreview: preview }, provide: { [intelligencePanelActiveKey as symbol]: panelActive } } }); return view }
afterEach(() => { view?.unmount(); view = undefined; vi.useRealTimers() })
describe('local monitor horizontal row', () => {
  it('shows only the current group multiplier and never substitutes an old execution rate', async () => {
    const previous = { ...run(8), rate_snapshot: { effective_rate_multiplier: 0.7 } }
    const wrapper = render(plan({ latest_run: run(9, 'pending'), recent_runs: [previous] }))
    expect(wrapper.get('[data-testid="local-group-rate"]').text()).toBe('0.4×')
    expect(wrapper.text()).not.toContain('0.7×')
    await wrapper.setProps({ plan: plan({ local_group_rate_multiplier: undefined, latest_run: previous, rate_snapshot: { effective_rate_multiplier: 0.7 } }) })
    expect(wrapper.get('[data-testid="local-group-rate"]').text()).toBe('—')
    await wrapper.setProps({ plan: plan({ local_group_rate_multiplier: 0 }) })
    expect(wrapper.get('[data-testid="local-group-rate"]').text()).toBe('0×')
  })
  it('shows live group metadata and masked administrator key even before its first run, without candy spacing', () => {
    const wrapper = render()
    expect(wrapper.get('h3').text()).toBe('Primary GPT')
    expect(wrapper.text()).toContain('0.4×')
    expect(wrapper.text()).toContain('My API Key')
    expect(wrapper.text()).toContain('sk-••••1234')
    expect(wrapper.text()).toContain('intelligenceMonitor.local.ownBadge')
    expect(wrapper.find('[data-testid="candy-monitor"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="local-featured-frame"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="local-artwork-empty"]').exists()).toBe(true)
  })
  it('keeps up to twenty artworks plus an active request in one strip and opens exact records', async () => {
    const history = Array.from({ length: 23 }, (_, index) => run(30 - index))
    const wrapper = render(plan({ latest_run: run(31, 'running'), recent_runs: history }))
    expect(wrapper.findAllComponents(preview).map(item => item.props('run').id)).toEqual([31, ...history.slice(0, 20).map(item => item.id)])
    expect(wrapper.text()).toContain('20/20')
    await wrapper.findAllComponents(preview)[3]!.trigger('click')
    expect(wrapper.emitted('history')).toEqual([[28]])
    expect(wrapper.get('[data-testid="local-pelican-run"]').attributes('disabled')).toBeDefined()
  })
  it('preserves artwork elements and horizontal position on polling, completion and visibility changes', async () => {
    const recent = [run(8), run(7), { ...run(6), test_kind: 'candy' } as IntelligenceRun, run(8)]
    const wrapper = render(plan({ latest_run: run(9, 'running'), recent_runs: recent }))
    const strip = wrapper.get('[data-testid="local-artwork-strip"]').element as HTMLElement
    const existing = wrapper.get('[data-run-id="8"]').element
    strip.scrollLeft = 210
    await wrapper.setProps({ plan: plan({ latest_run: { ...run(9, 'running') }, recent_runs: recent }) })
    expect(strip.scrollLeft).toBe(210)
    await wrapper.setProps({ plan: plan({ latest_run: run(9), recent_runs: [run(9), ...recent] }) })
    expect(wrapper.get('[data-run-id="8"]').element).toBe(existing)
    expect(wrapper.findAllComponents(preview).map(item => item.props('run').id)).toEqual([9, 8, 7])
    expect(strip.scrollLeft).toBe(210)
    await wrapper.setProps({ visible: false }); await wrapper.setProps({ visible: true })
    expect(wrapper.get('[data-run-id="8"]').element).toBe(existing)
    expect(strip.scrollLeft).toBe(210)
    await wrapper.setProps({ plan: plan({ latest_run: run(10, 'pending'), recent_runs: [run(9), ...recent] }) })
    await wrapper.vm.$nextTick()
    expect(strip.scrollLeft).toBe(0)
    expect(wrapper.get('[data-run-id="8"]').element).toBe(existing)
  })
  it('queues each test independently while preventing edit or archive during either active test', async () => {
    const wrapper = render(plan({ candy_enabled: true, candy_latest_run: { ...run(99, 'running'), test_kind: 'candy' } }))
    expect(wrapper.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="local-pelican-run"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="local-pelican-run"]').trigger('click')
    expect(wrapper.emitted('run')).toEqual([[]])
    expect(wrapper.get('[aria-label="intelligenceMonitor.edit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[aria-label="intelligenceMonitor.archive"]').attributes('disabled')).toBeDefined()
  })
  it('uses separate local countdowns and stops them when the tab or filtered card is hidden', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-27T00:00:00Z'))
    const active = ref(true)
    const wrapper = render(plan({ enabled: true, candy_enabled: true, next_run_at: '2026-09-27T00:05:00Z', candy_next_run_at: '2026-09-27T00:03:00Z' }), active)
    expect(wrapper.get('[data-testid="local-pelican-countdown"]').text()).toBe('00:05:00')
    expect(wrapper.get('[data-testid="candy-countdown"]').text()).toBe('00:03:00')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('[data-testid="candy-countdown"]').text()).toBe('00:02:59')
    await wrapper.setProps({ visible: false }); expect(vi.getTimerCount()).toBe(0)
    await wrapper.setProps({ visible: true }); expect(vi.getTimerCount()).toBe(2)
    active.value = false; await wrapper.vm.$nextTick(); expect(vi.getTimerCount()).toBe(0)
  })
})
