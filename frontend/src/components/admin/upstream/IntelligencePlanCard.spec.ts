import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligencePlanCard from './IntelligencePlanCard.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { count?: number }) => values?.count === undefined ? key : `${key}:${values.count}` }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { detail: vi.fn() } }))
const preview = defineComponent({ name: 'IntelligenceArtifactPreview', props: ['run'], emits: ['open'], template: '<button class="test-preview" @click="$emit(\'open\')">{{ run.id }}</button>' })
const run = (id: number, status: IntelligenceRun['status'] = 'succeeded') => ({ id, status, created_at: '2026-09-23T10:00:00Z', started_at: null, model: 'gpt-6-astra', reasoning_effort: 'high' } as IntelligenceRun)
const plan = (changes: Partial<IntelligencePlan> = {}): IntelligencePlan => ({ id: 1, name: 'Primary plan', source_type: 'external', source_name: 'Primary source', endpoint: 'https://relay.example', rate_snapshot: { effective_rate_multiplier: 0.3 }, interval_seconds: 3600, enabled: false, latest_run: null, ...changes } as IntelligencePlan)
function render(value: IntelligencePlan, busy = false) {
  return mount(IntelligencePlanCard, { props: { plan: value, overview: null, busy }, global: { stubs: { Icon: true, IntelligenceArtifactPreview: preview } } })
}

describe('intelligence plan card result selection', () => {
  it('places completed artwork duration at the right end of the model and reasoning row', () => {
    const completed = { ...run(91), duration_ms: 14500 }
    const view = render(plan({ recent_runs: [completed], latest_run: completed }))
    const metadata = view.get('[data-testid="artwork-metadata"]')
    expect(metadata.text()).toContain('gpt-6-astra · high')
    const duration = metadata.get('[data-testid="artwork-duration"]')
    expect(duration.text()).toBe('intelligenceMonitor.seconds:14.5')
    expect(metadata.element.lastElementChild).toBe(duration.element)
    expect(duration.classes()).toEqual(expect.arrayContaining(['ml-auto', 'shrink-0', 'whitespace-nowrap']))
    expect(duration.attributes('title')).toBe('intelligenceMonitor.duration · intelligenceMonitor.seconds:14.5')
    view.unmount()
  })

  it.each(['pending', 'running', 'failed'] as const)('does not label a %s run with a completed artwork duration', status => {
    const latest = { ...run(91, status), duration_ms: 14500 }
    const view = render(plan({ recent_runs: [latest], latest_run: latest }))
    expect(view.find('[data-testid="artwork-duration"]').exists()).toBe(false)
    view.unmount()
  })

  it.each([
    [30, 'seconds', 30],
    [75, 'seconds', 75],
    [90, 'seconds', 90],
    [300, 'minutes', 5],
    [3660, 'minutes', 61],
    [3600, 'hours', 1],
    [86400, 'hours', 24],
  ])('shows a %i-second interval without rounding away custom seconds', (interval, unit, count) => {
    const view = render(plan({ enabled: true, interval_seconds: Number(interval) }))
    expect(view.get('aside').text()).toContain(`intelligenceMonitor.${unit}:${count}`)
    view.unmount()
  })

  it('shows at most twenty results in server order and opens the specifically selected result', async () => {
    const ids = [91, 105, 82, ...Array.from({ length: 19 }, (_, i) => 70 - i)]
    const view = render(plan({ recent_runs: ids.map(id => run(id)), latest_run: run(91) }))
    expect(view.findAllComponents(preview).map(child => child.props('run').id)).toEqual(ids.slice(0, 20))
    expect(view.text()).toContain('20/20')
    await view.findAll('.test-preview')[0].trigger('click')
    const secondCaption = view.findAll('button').find(button => button.text().includes('#105'))!
    await secondCaption.trigger('click')
    expect(view.emitted('history')).toEqual([[91], [105]])
    const history = view.findAll('button').find(button => button.text().includes('intelligenceMonitor.history'))!
    await history.trigger('click')
    expect(view.emitted('history')?.[2]).toEqual([])
    view.unmount()
  })

  it('labels OAuth accounts and never displays an upstream multiplier for them', () => {
    const view = render(plan({ name: 'OAuth Account A', source_type: 'openai_oauth', source_name: 'OAuth Account A', endpoint: '', rate_snapshot: { effective_rate_multiplier: 9.99 } }))
    expect(view.get('h3').text()).toBe('OAuth Account A')
    expect(view.text()).toContain('OAuth')
    expect(view.text()).toContain('OpenAI')
    expect(view.text()).toContain('intelligenceMonitor.oauth.account')
    expect(view.text()).not.toContain('intelligenceMonitor.rate')
    expect(view.text()).not.toContain('9.99')
    view.unmount()
  })

  it('retains previous results while a run is active and prevents duplicate runs or edits', async () => {
    const view = render(plan({ latest_run: run(12, 'running'), recent_runs: [run(11, 'failed')] }))
    expect(view.findAllComponents(preview).map(child => child.props('run').id)).toEqual([12, 11])
    expect(view.text()).toContain('1/20')
    const runButton = view.get('aside').findAll('button').find(button => button.text() === 'intelligenceMonitor.status.running')!
    expect(runButton.attributes('disabled')).toBeDefined()
    expect(view.get('[aria-label="intelligenceMonitor.edit"]').attributes('disabled')).toBeDefined()
    expect(view.get('[aria-label="intelligenceMonitor.archive"]').attributes('disabled')).toBeDefined()
    await view.findAll('.test-preview')[1]!.trigger('click')
    expect(view.emitted('history')).toEqual([[11]])
    await view.setProps({ busy: true })
    expect(view.get('[aria-label="intelligenceMonitor.resume"]').attributes('disabled')).toBeDefined()
    view.unmount()
  })

  it.each(['pending', 'running'] as const)('shows the %s preview in addition to all twenty stored results', status => {
    const previous = Array.from({ length: 20 }, (_, index) => run(50 - index))
    const view = render(plan({ latest_run: run(51, status), recent_runs: previous }))
    expect(view.findAllComponents(preview).map(child => child.props('run').id)).toEqual([51, ...previous.map(item => item.id)])
    expect(view.text()).toContain('20/20')
    expect(view.text()).not.toContain('21/20')
    expect(view.text()).toContain(`intelligenceMonitor.status.${status}`)
    view.unmount()
  })

  it('shows a first active run as a preview while the completed result count remains zero', () => {
    const view = render(plan({ latest_run: run(1, 'pending'), recent_runs: [] }))
    expect(view.findAllComponents(preview).map(child => child.props('run').id)).toEqual([1])
    expect(view.text()).toContain('0/20')
    expect(view.text()).not.toContain('intelligenceMonitor.waiting')
    view.unmount()
  })

  it('preserves preview instances through polling and the active run completing before recent results refresh', async () => {
    const previous = Array.from({ length: 20 }, (_, index) => run(50 - index))
    const view = render(plan({ latest_run: run(51, 'running'), recent_runs: previous }))
    const activePreview = view.findAllComponents(preview)[0]!.vm
    const previousPreview = view.findAllComponents(preview)[1]!.vm
    await view.setProps({ plan: plan({ latest_run: { ...run(51, 'running'), duration_ms: 2000 }, recent_runs: previous.map(item => ({ ...item })) }) })
    expect(view.findAllComponents(preview)[0]!.vm).toBe(activePreview)
    expect(view.findAllComponents(preview)[1]!.vm).toBe(previousPreview)

    await view.setProps({ plan: plan({ latest_run: run(51), recent_runs: previous }) })
    expect(view.findAllComponents(preview).map(child => child.props('run').id)).toEqual([51, ...previous.slice(0, 19).map(item => item.id)])
    expect(view.findAllComponents(preview)[0]!.vm).toBe(activePreview)
    expect(view.findAllComponents(preview)[1]!.vm).toBe(previousPreview)
    expect(view.text()).toContain('20/20')

    await view.setProps({ plan: plan({ latest_run: run(51), recent_runs: [run(51), ...previous.slice(0, 19)] }) })
    expect(view.findAllComponents(preview)[0]!.vm).toBe(activePreview)
    expect(view.findAllComponents(preview)).toHaveLength(20)
    view.unmount()
  })
})

describe('intelligence plan schedule countdown', () => {
  let view: ReturnType<typeof render> | undefined
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-24T00:00:00Z'))
  })
  afterEach(() => {
    view?.unmount()
    view = undefined
    vi.useRealTimers()
  })

  it('counts down to the server deadline across polling and browser timer delays', async () => {
    const value = plan({ enabled: true, next_run_at: '2026-09-24T01:02:03Z' })
    view = render(value)
    const countdown = () => view!.get('[data-testid="intelligence-countdown"]')
    expect(countdown().text()).toBe('01:02:03')
    expect(countdown().attributes('datetime')).toBe(value.next_run_at)
    expect(countdown().attributes('title')).toBe(new Date(value.next_run_at!).toLocaleString())
    await vi.advanceTimersByTimeAsync(3000)
    expect(countdown().text()).toBe('01:02:00')
    await view.setProps({ plan: { ...value } })
    expect(countdown().text()).toBe('01:02:00')
    expect(vi.getTimerCount()).toBe(1)

    vi.setSystemTime(new Date('2026-09-24T01:00:00Z'))
    await vi.advanceTimersByTimeAsync(1000)
    expect(countdown().text()).toBe('00:02:02')
    view.unmount()
    view = undefined
    expect(vi.getTimerCount()).toBe(0)
  })

  it('waits for the scheduler at zero without inventing another cycle and accepts a new deadline', async () => {
    view = render(plan({ enabled: true, next_run_at: '2026-09-24T00:00:01.500Z' }))
    expect(view.get('[data-testid="intelligence-countdown"]').text()).toBe('00:00:02')
    await vi.advanceTimersByTimeAsync(2000)
    expect(view.find('[data-testid="intelligence-countdown"]').exists()).toBe(false)
    expect(view.get('[data-testid="intelligence-waiting-schedule"]').text()).toBe('intelligenceMonitor.waitingSchedule')
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(5000)
    expect(view.find('[data-testid="intelligence-countdown"]').exists()).toBe(false)
    await view.setProps({ plan: plan({ enabled: true, next_run_at: '2026-09-24T00:05:07Z' }) })
    expect(view.get('[data-testid="intelligence-countdown"]').text()).toBe('00:05:00')
    expect(vi.getTimerCount()).toBe(1)
  })

  it.each([null, 'invalid-date', '2026-09-23T23:59:00Z'])('shows waiting instead of a false countdown for deadline %s', next_run_at => {
    view = render(plan({ enabled: true, next_run_at }))
    expect(view.get('[data-testid="intelligence-waiting-schedule"]').text()).toBe('intelligenceMonitor.waitingSchedule')
    expect(view.find('[data-testid="intelligence-countdown"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['pending', 'running'] as const)('does not count down a provisional deadline while %s, then starts after completion', async status => {
    const value = plan({ enabled: true, latest_run: run(21, status), next_run_at: '2026-09-24T00:05:00Z' })
    view = render(value)
    expect(view.get('[data-testid="intelligence-schedule"]').text()).toContain('intelligenceMonitor.afterCurrentRun')
    expect(view.find('[data-testid="intelligence-countdown"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(30000)
    await view.setProps({ plan: { ...value, latest_run: run(21), next_run_at: '2026-09-24T00:05:30Z' } })
    expect(view.get('[data-testid="intelligence-countdown"]').text()).toBe('00:05:00')
    expect(vi.getTimerCount()).toBe(1)
    await view.setProps({ plan: { ...value, latest_run: run(22, 'running') } })
    expect(view.find('[data-testid="intelligence-countdown"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['external', 'openai_oauth'] as const)('hides and stops the %s schedule when disabled, and resumes from the new deadline', async source_type => {
    const value = plan({ source_type, enabled: false, next_run_at: '2026-09-24T00:05:00Z' })
    view = render(value)
    expect(view.find('[data-testid="intelligence-schedule"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await view.setProps({ plan: { ...value, enabled: true } })
    expect(view.get('[data-testid="intelligence-countdown"]').text()).toBe('00:05:00')
    await view.setProps({ plan: { ...value, enabled: false, next_run_at: null } })
    expect(view.find('[data-testid="intelligence-schedule"]').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(60000)
    await view.setProps({ plan: { ...value, enabled: true, next_run_at: '2026-09-24T00:11:00Z' } })
    expect(view.get('[data-testid="intelligence-countdown"]').text()).toBe('00:10:00')
  })
})
