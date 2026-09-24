import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceHistoryDialog from './IntelligenceHistoryDialog.vue'

const mocks = vi.hoisted(() => ({ runs: vi.fn(), detail: vi.fn() }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const dialog = defineComponent({
  props: ['show'],
  template: '<div v-if="show"><slot /></div>',
})
const preview = defineComponent({
  props: ['run'],
  template: '<div data-testid="artifact-preview">{{ run.id }}</div>',
})

function plan(id = 1): IntelligencePlan {
  return {
    id, name: `Plan ${id}`, source_type: 'external', endpoint: 'https://upstream.example/v1',
    supplier_note: '', group_note: '', rate_note: '', notes: '', api_mode: 'responses',
    enabled: true, interval_seconds: 300, timeout_seconds: 180,
    model: 'gpt-6-astra', reasoning_effort: 'high', prompt: 'Render an animation.',
    api_key_masked: 'sk-…test', source_name: 'Test upstream', rate_snapshot: null,
    created_by: 1, created_at: '2026-09-24T00:00:00Z', updated_at: '2026-09-24T00:00:00Z',
    last_run_at: null, next_run_at: null, latest_run: null,
  }
}

function run(id: number): IntelligenceRun {
  return {
    id, plan_id: Math.floor(id / 100), status: 'succeeded', trigger: 'manual',
    created_at: '2026-09-24T00:00:00Z', started_at: '2026-09-24T00:00:00Z',
    finished_at: '2026-09-24T00:00:01Z', duration_ms: 1000, http_status: 200, error: '',
    model: 'gpt-6-astra', reasoning_effort: 'high', prompt: 'Render an animation.',
    source_type: 'external', source_name: 'Test upstream', source_endpoint: 'https://upstream.example/v1',
    source_snapshot: null, rate_snapshot: null, notes_snapshot: null,
    html: `<html><body>Run ${id}</body></html>`, raw_text: `Response for run ${id}`,
  }
}

let wrapper: VueWrapper | undefined

function render(initialPlan: IntelligencePlan = plan(), initialRunID?: number) {
  wrapper = mount(IntelligenceHistoryDialog, {
    props: { show: true, plan: initialPlan },
    attrs: { 'initial-run-id': initialRunID },
    global: { stubs: { BaseDialog: dialog, Icon: true, IntelligenceArtifactPreview: preview } },
  })
  return wrapper
}

function runButton(view: VueWrapper, id: number) {
  const button = view.get('aside').findAll('button').find(item => item.text().includes(`#${id}`))
  if (!button) throw new Error(`Run ${id} is missing from history`)
  return button
}

async function selectSecondPageSource(view: VueWrapper) {
  await flushPromises()
  await view.get('[data-testid="next-page"]').trigger('click')
  await flushPromises()
  await runButton(view, 122).trigger('click')
  await flushPromises()
  const sourceButton = view.findAll('button').find(item => item.text() === 'intelligenceMonitor.sourceCode')
  if (!sourceButton) throw new Error('Source code view button is missing')
  await sourceButton.trigger('click')
}

function expectPreservedSelection(view: VueWrapper) {
  expect(view.get('[data-testid="page"]').text()).toBe('2')
  expect(runButton(view, 122).classes()).toContain('border-primary-300')
  expect(view.get('pre').text()).toBe(run(122).html)
  expect(view.find('[data-testid="artifact-preview"]').exists()).toBe(false)
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  mocks.runs.mockImplementation(async (planID: number, page: number) => ({
    items: [run(planID * 100 + page * 10 + 1), run(planID * 100 + page * 10 + 2)],
    total: 24, page, page_size: 12,
  }))
  mocks.detail.mockImplementation(async (id: number) => run(id))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('intelligence history polling state', () => {
  it('uses compact sidebar pagination and disables requests outside the page bounds', async () => {
    const view = render()
    await flushPromises()
    const pagination = view.get('[data-testid="history-pagination"]')
    expect(pagination.get('p').classes()).toContain('whitespace-nowrap')
    expect(pagination.text().replace(/\s/g, '')).toBe('1/2')
    expect(view.get('[data-testid="previous-page"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="previous-page"]').trigger('click')
    expect(mocks.runs).toHaveBeenCalledTimes(1)

    await view.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    expect(pagination.text().replace(/\s/g, '')).toBe('2/2')
    expect(view.get('[data-testid="next-page"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="next-page"]').trigger('click')
    expect(mocks.runs).toHaveBeenCalledTimes(2)
    await view.get('[data-testid="previous-page"]').trigger('click')
    await flushPromises()
    expect(mocks.runs).toHaveBeenLastCalledWith(1, 1, expect.any(AbortSignal))
  })

  it('hides pagination when the history fits on one page', async () => {
    mocks.runs.mockResolvedValueOnce({ items: [run(111)], total: 1, page: 1, page_size: 12 })
    const view = render()
    await flushPromises()
    expect(view.find('[data-testid="history-pagination"]').exists()).toBe(false)
  })

  it('disables both page controls while a requested page is loading', async () => {
    const view = render()
    await flushPromises()
    let resolveRuns: (value: { items: IntelligenceRun[]; total: number }) => void = () => undefined
    mocks.runs.mockReturnValueOnce(new Promise(resolve => { resolveRuns = resolve }))
    await view.get('[data-testid="next-page"]').trigger('click')
    expect(view.get('[data-testid="previous-page"]').attributes('disabled')).toBeDefined()
    expect(view.get('[data-testid="next-page"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="previous-page"]').trigger('click')
    expect(mocks.runs).toHaveBeenCalledTimes(2)
    resolveRuns({ items: [run(121)], total: 24 })
    await flushPromises()
    expect(view.get('[data-testid="previous-page"]').attributes('disabled')).toBeUndefined()
  })

  it('keeps a manually selected slow detail request alive across history polls', async () => {
    const view = render()
    await flushPromises()
    let resolveDetail: (value: IntelligenceRun) => void = () => undefined
    mocks.detail.mockReturnValueOnce(new Promise(resolve => { resolveDetail = resolve }))
    await runButton(view, 112).trigger('click')
    const detailSignal = mocks.detail.mock.calls.at(-1)![1] as AbortSignal
    expect(mocks.detail).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(mocks.runs).toHaveBeenCalledTimes(3)
    expect(mocks.detail).toHaveBeenCalledTimes(2)
    expect(detailSignal.aborted).toBe(false)

    resolveDetail(run(112))
    await flushPromises()
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe('112')
  })
  it('clears a prior detail error when another run loads successfully', async () => {
    const view = render()
    await flushPromises()
    mocks.detail.mockRejectedValueOnce({ message: 'The first detail failed' })
    await runButton(view, 112).trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('The first detail failed')

    await runButton(view, 111).trigger('click')
    await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe('111')
  })
  it.each([12,13,20])('opens completed work %i on its actual page when an active run precedes it', async position => {
    const completed=Array.from({length:20},(_,index)=>run(200-index))
    const active={...run(201),status:'running' as const}
    const items=[active,...completed]
    mocks.runs.mockImplementation(async (_planID: number, page: number) => ({ items: items.slice((page-1)*12,page*12),total:items.length,page,page_size:12 }))
    const selected=completed[position-1]!
    const view=render({...plan(),recent_runs:completed,latest_run:active},selected.id)
    await flushPromises()

    expect(mocks.runs).toHaveBeenCalledTimes(2)
    expect(mocks.runs).toHaveBeenLastCalledWith(1,2,expect.any(AbortSignal))
    expect(mocks.detail).toHaveBeenCalledTimes(1)
    expect(mocks.detail).toHaveBeenLastCalledWith(selected.id,expect.any(AbortSignal))
    expect(view.get('[data-testid="page"]').text()).toBe('2')
    expect(runButton(view,selected.id).classes()).toContain('border-primary-300')
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe(String(selected.id))
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(mocks.detail).toHaveBeenCalledTimes(1)
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe(String(selected.id))
  })

  it('uses the server page when a newly queued run is missing from the last plan poll', async () => {
    const completed=Array.from({length:20},(_,index)=>run(200-index))
    const active={...run(201),status:'pending' as const}
    const items=[active,...completed]
    mocks.runs.mockImplementation(async (_planID: number, page: number) => ({ items:items.slice((page-1)*12,page*12),total:items.length,page,page_size:12 }))
    const selected=completed[11]!
    const view=render({...plan(),recent_runs:completed,latest_run:completed[0]!},selected.id)
    await flushPromises()
    expect(view.get('[data-testid="page"]').text()).toBe('2')
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe(String(selected.id))
    expect(mocks.detail).toHaveBeenCalledTimes(1)
  })

  it('keeps the twelfth completed work on page one when there is no active run', async () => {
    const items=Array.from({length:20},(_,index)=>run(200-index))
    mocks.runs.mockImplementation(async (_planID: number, page: number) => ({ items:items.slice((page-1)*12,page*12),total:items.length,page,page_size:12 }))
    const selected=items[11]!
    const view=render({...plan(),recent_runs:items,latest_run:items[0]!},selected.id)
    await flushPromises()
    expect(mocks.runs).toHaveBeenCalledTimes(1)
    expect(view.get('[data-testid="page"]').text()).toBe('1')
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe(String(selected.id))
  })

  it('preserves the current page, selected run and source view when a refreshed plan has the same ID', async () => {
    const view = render()
    await selectSecondPageSource(view)
    expectPreservedSelection(view)
    expect(mocks.runs).toHaveBeenCalledTimes(2)
    expect(mocks.detail).toHaveBeenCalledTimes(3)

    await view.setProps({ plan: { ...plan(), name: 'Updated plan name', updated_at: '2026-09-24T00:00:05Z', latest_run: run(113) } })
    await flushPromises()

    expect(mocks.runs).toHaveBeenCalledTimes(2)
    expect(mocks.detail).toHaveBeenCalledTimes(3)
    expectPreservedSelection(view)
  })

  it('polls the current history page without changing selection or view', async () => {
    const view = render()
    await selectSecondPageSource(view)

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(mocks.runs).toHaveBeenCalledTimes(3)
    expect(mocks.runs).toHaveBeenLastCalledWith(1, 2, expect.any(AbortSignal))
    expect(mocks.detail).toHaveBeenCalledTimes(3)
    expectPreservedSelection(view)
  })

  it('resets to page one and preview when switching to another plan', async () => {
    const view = render()
    await selectSecondPageSource(view)

    await view.setProps({ plan: plan(2) })
    await flushPromises()

    expect(mocks.runs).toHaveBeenCalledTimes(3)
    expect(mocks.runs).toHaveBeenLastCalledWith(2, 1, expect.any(AbortSignal))
    expect(mocks.detail).toHaveBeenLastCalledWith(211, expect.any(AbortSignal))
    expect(view.get('[data-testid="page"]').text()).toBe('1')
    expect(runButton(view, 211).classes()).toContain('border-primary-300')
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe('211')
    expect(view.find('pre').exists()).toBe(false)
  })

  it('stops polling while closed and resets to page one and preview when reopened', async () => {
    const view = render()
    await selectSecondPageSource(view)

    await view.setProps({ show: false })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(mocks.runs).toHaveBeenCalledTimes(2)
    expect(mocks.detail).toHaveBeenCalledTimes(3)

    await view.setProps({ show: true })
    await flushPromises()

    expect(mocks.runs).toHaveBeenCalledTimes(3)
    expect(mocks.runs).toHaveBeenLastCalledWith(1, 1, expect.any(AbortSignal))
    expect(mocks.detail).toHaveBeenLastCalledWith(111, expect.any(AbortSignal))
    expect(view.get('[data-testid="page"]').text()).toBe('1')
    expect(runButton(view, 111).classes()).toContain('border-primary-300')
    expect(view.get('[data-testid="artifact-preview"]').text()).toBe('111')
    expect(view.find('pre').exists()).toBe(false)
  })
})
