import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceCandyBar from './IntelligenceCandyBar.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { count?: number }) => values?.count === undefined ? key : `${key}:${values.count}` }) }))
const run = (id: number, fields: Partial<IntelligenceRun> = {}) => ({ id, plan_id: 1, test_kind: 'candy', status: 'succeeded', correct: true, answer: '21', model: 'gpt-6-astra', reasoning_effort: 'high', created_at: '2026-09-27T01:00:00Z', duration_ms: 1200, ...fields }) as IntelligenceRun
const plan = (fields: Partial<IntelligencePlan> = {}) => ({ id: 1, candy_enabled: true, latest_run: null, ...fields }) as IntelligencePlan
function render(value: IntelligencePlan, busy = false) {
  return mount(IntelligenceCandyBar, { props: { plan: value, busy }, global: { stubs: { Icon: true, HelpTooltip: { template: '<div><slot name="trigger" /><slot /></div>' } } } })
}

describe('compact candy strip', () => {
  it('shows correct in green, incorrect and request failures in red, with sixty slots and exact record selection', async () => {
    const failed = run(3, { status: 'failed', correct: null, error: 'upstream unavailable', http_status: 502 })
    const incorrect = run(2, { correct: false, answer: '20' })
    const view = render(plan({ candy_latest_run: failed, candy_recent_runs: [failed, incorrect, run(1)] }))
    expect(view.findAll('[data-candy-status]')).toHaveLength(60)
    expect(view.findAll('[data-candy-status="empty"]')).toHaveLength(57)
    expect(view.get('[data-candy-status="correct"]').classes()).toContain('candy-bar-correct')
    expect(view.get('[data-candy-status="incorrect"]').classes()).toContain('candy-bar-failed')
    expect(view.get('[data-candy-status="failed"]').classes()).toContain('candy-bar-failed')
    expect(view.text()).toContain('upstream unavailable')
    expect(view.text()).toContain('HTTP 502')
    expect(view.text()).toContain('gpt-6-astra · high')
    expect(view.text()).toContain('20')
    expect(view.text()).toContain('intelligenceMonitor.seconds:1.2')
    await view.get('[data-candy-status="incorrect"]').trigger('click')
    expect(view.emitted('select')).toEqual([[incorrect]])
    await view.get('[data-testid="candy-run"]').trigger('click')
    expect(view.emitted('run')).toEqual([[]])
    view.unmount()
  })

  it.each(['pending', 'running'] as const)('keeps the %s run neutral and retains all sixty completed results', status => {
    const view = render(plan({ candy_latest_run: run(61, { status, correct: null }), candy_recent_runs: Array.from({ length: 60 }, (_, index) => run(60 - index)) }))
    expect(view.findAll('[data-candy-status]')).toHaveLength(61)
    expect(view.get(`[data-candy-status="${status}"]`).classes()).toContain('candy-bar-empty')
    expect(view.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    expect(view.text()).toContain('intelligenceMonitor.candy.running')
    view.unmount()
  })

  it('allows candy to queue independently of pelican but blocks submission during an action', async () => {
    const view = render(plan({ latest_run: run(9, { test_kind: 'pelican', status: 'running' }) }))
    await view.get('[data-testid="candy-run"]').trigger('click')
    expect(view.emitted('run')).toEqual([[]])
    await view.setProps({ plan: plan(), busy: true })
    expect(view.get('[data-testid="candy-run"]').attributes('disabled')).toBeDefined()
    view.unmount()
  })
})
