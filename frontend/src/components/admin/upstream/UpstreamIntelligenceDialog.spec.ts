import { computed, inject, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview, UpstreamTarget } from '@/api/admin/upstreamCenter'
import UpstreamIntelligenceDialog from './UpstreamIntelligenceDialog.vue'
import { intelligencePanelActiveKey, intelligencePreviewRefreshKey } from './intelligenceMonitorContext'

const mocks = vi.hoisted(() => ({
  plans: vi.fn(), update: vi.fn(), run: vi.fn(), runCandy: vi.fn(), archive: vi.fn(), purge: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: mocks, PELICAN_MODEL: 'gpt-6-astra', PELICAN_REASONING: 'high' }))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { purge: mocks.purge } }))
vi.mock('./IntelligencePlanCard.vue', () => ({ default: {
  name: 'IntelligencePlanCard', props: ['plan', 'overview', 'busy', 'visible'], emits: ['run', 'candyRun', 'candySelect', 'toggle', 'edit', 'history', 'archive'],
  setup(props: { visible: boolean }) {
    const panelActive = inject(intelligencePanelActiveKey, ref(true))
    const active = computed(() => panelActive.value && props.visible)
    const refresh = inject(intelligencePreviewRefreshKey, ref(0))
    return { active, refresh, revision: computed(() => refresh.value) }
  },
  template: '<div data-testid="plan-card" :data-id="plan.id" :data-active="active" :data-revision="revision">{{ plan.name }}</div>',
} }))
vi.mock('./IntelligencePlanDialog.vue', () => ({ default: {
  name: 'IntelligencePlanDialog', props: ['show', 'plan', 'overview', 'upstreamTargetId'], emits: ['close', 'saved'],
  template: '<div data-testid="plan-editor" />',
} }))
vi.mock('./IntelligenceHistoryDialog.vue', () => ({ default: {
  name: 'IntelligenceHistoryDialog', props: ['show', 'plan', 'initialRunId'], emits: ['close'],
  setup() { return { active: inject(intelligencePanelActiveKey, ref(true)) } },
  template: '<div data-testid="plan-history" :data-active="active" />',
} }))
vi.mock('./UpstreamDeleteDialog.vue', () => ({ default: {
  name: 'UpstreamDeleteDialog', props: ['show', 'item', 'busy', 'error'], emits: ['close', 'confirm'],
  template: '<div data-testid="plan-delete">{{ error }}</div>',
} }))
vi.mock('./IntelligenceCandyDetailDialog.vue', () => ({ default: { name: 'IntelligenceCandyDetailDialog', props: ['run'], emits: ['close'], template: '<div data-testid="candy-detail" />' } }))

const baseDialog = {
  name: 'BaseDialog', props: { show: Boolean, title: String, width: String, closeOnEscape: Boolean, showCloseButton: Boolean }, emits: ['close'],
  template: '<div data-testid="group-dialog"><slot /></div>',
}
const target = { id: 11, name: 'Premium', provider: 'openai', endpoint: 'https://relay.example/v1' } as UpstreamTarget
const overview = { suppliers: [{ id: 1, targets: [target] }], monitors: [] } as unknown as UpstreamOverview
function plan(id = 1, overrides: Partial<IntelligencePlan> = {}): IntelligencePlan {
  return { id, name: 'Premium plan', source_type: 'upstream', upstream_target_id: 11, enabled: false, next_run_at: null, latest_run: null, recent_runs: [], ...overrides } as IntelligencePlan
}
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let wrapper: VueWrapper | undefined
function render() {
  wrapper = mount(UpstreamIntelligenceDialog, { props: { target, overview }, global: { stubs: { BaseDialog: baseDialog, Icon: true } } })
  return wrapper
}
const cards = (view: VueWrapper) => view.findAllComponents({ name: 'IntelligencePlanCard' })
const refresh = (view: VueWrapper) => view.get('[data-testid="group-intelligence-refresh"]').trigger('click')
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  mocks.plans.mockResolvedValue({ items: [] })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('upstream group intelligence dialog', () => {
  it('uses the separate candy action, prevents duplicate runs and retains its result through refresh errors', async () => {
    mocks.plans.mockResolvedValue({ items: [plan(1, { candy_enabled: true })] })
    const view = render(); await flushPromises()
    const response = deferred<IntelligenceRun>(); mocks.runCandy.mockReturnValueOnce(response.promise)
    cards(view)[0]!.vm.$emit('candyRun'); cards(view)[0]!.vm.$emit('candyRun'); cards(view)[0]!.vm.$emit('run')
    await flushPromises()
    expect(mocks.runCandy).toHaveBeenCalledTimes(1)
    expect(mocks.run).not.toHaveBeenCalled()
    const queued = { id: 102, plan_id: 1, test_kind: 'candy', status: 'pending' } as IntelligenceRun
    mocks.plans.mockRejectedValue(new Error('offline')); response.resolve(queued); await flushPromises()
    expect(cards(view)[0]!.props('plan')).toMatchObject({ candy_latest_run: queued, latest_run: null })
    expect(view.emitted('changed')).toHaveLength(1)
    cards(view)[0]!.vm.$emit('candyRun'); cards(view)[0]!.vm.$emit('run'); await flushPromises()
    expect(mocks.runCandy).toHaveBeenCalledTimes(1)
    expect(mocks.run).toHaveBeenCalledTimes(1)
  })

  it('polls candy-only activity, updates its open detail and protects the parent modal until detail closes', async () => {
    const candy = { id: 102, plan_id: 1, test_kind: 'candy', status: 'running' } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [plan(1, { candy_enabled: true, candy_latest_run: candy })] })
    const view = render(); await flushPromises()
    cards(view)[0]!.vm.$emit('candySelect', candy); await flushPromises()
    const child = view.getComponent({ name: 'IntelligenceCandyDetailDialog' })
    const parent = view.getComponent({ name: 'BaseDialog' })
    expect(parent.props('closeOnEscape')).toBe(false)
    expect(cards(view)[0]!.props('visible')).toBe(false)
    parent.vm.$emit('close'); expect(view.emitted('close')).toBeUndefined()
    const completed = { ...candy, status: 'succeeded', correct: true, answer: '21' } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [plan(1, { candy_enabled: true, candy_latest_run: completed })] })
    await vi.advanceTimersByTimeAsync(2000)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    expect(child.props('run')).toEqual(completed)
    child.vm.$emit('close'); await flushPromises()
    expect(parent.props('closeOnEscape')).toBe(true)
    expect(cards(view)[0]!.props('visible')).toBe(true)
    expect(view.find('[data-testid="candy-detail"]').exists()).toBe(false)
  })

  it('preserves candy metadata when saved plan responses omit run summaries', async () => {
    const candy = { id: 102, plan_id: 1, test_kind: 'candy', status: 'succeeded', correct: true } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [plan(1, { candy_enabled: true, candy_latest_run: candy, candy_recent_runs: [candy] })] })
    const view = render(); await flushPromises()
    cards(view)[0]!.vm.$emit('edit'); await flushPromises()
    mocks.plans.mockRejectedValue(new Error('offline'))
    view.getComponent({ name: 'IntelligencePlanDialog' }).vm.$emit('saved', plan(1, { candy_enabled: false })); await flushPromises()
    expect(cards(view)[0]!.props('plan')).toMatchObject({ candy_enabled: false, candy_latest_run: candy, candy_recent_runs: [candy] })
  })
  it('loads only this group and displays every legacy linked plan without a new-plan button', async () => {
    mocks.plans.mockResolvedValue({ items: [plan(1), plan(2), plan(3, { upstream_target_id: 12 }), plan(4, { source_type: 'external' })] })
    const view = render()
    await flushPromises()
    expect(mocks.plans).toHaveBeenCalledWith(expect.any(AbortSignal), 11)
    expect(cards(view).map(card => card.props('plan').id)).toEqual([1, 2])
    expect(cards(view)[0]!.props('overview')).toEqual(overview)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    expect(view.getComponent({ name: 'BaseDialog' }).props('width')).toBe('extra-wide')
    expect(mocks.run).not.toHaveBeenCalled()
  })

  it('keeps loading and failed reads distinct from an empty list, and retries without caching the error', async () => {
    const response = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(response.promise)
    const view = render()
    // The first render reserves artwork space before onMounted starts the request.
    expect(view.find('[data-testid="group-intelligence-loading"]').exists()).toBe(true)
    const viewport = view.get('[data-testid="group-intelligence-viewport"]').element
    await flushPromises()
    expect(view.find('[data-testid="group-intelligence-loading"]').exists()).toBe(true)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    response.reject(new Error('offline'))
    await flushPromises()
    expect(view.find('[data-testid="group-intelligence-loading"]').exists()).toBe(false)
    expect(view.get('[data-testid="group-intelligence-viewport"]').element).toBe(viewport)
    expect(view.get('[role="alert"]').text()).toBeTruthy()
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    const retry = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(retry.promise)
    await refresh(view)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    retry.resolve({ items: [] })
    await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.get('[data-testid="group-intelligence-viewport"]').element).toBe(viewport)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(true)
    const empty = view.get('[data-testid="group-intelligence-empty"]').element
    const next = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(next.promise)
    await refresh(view)
    expect(view.get('[data-testid="group-intelligence-empty"]').element).toBe(empty)
    expect(view.get('[data-testid="group-intelligence-create"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    expect(view.find('[data-testid="plan-editor"]').exists()).toBe(false)
    next.resolve({ items: [plan()] })
    await flushPromises()
    expect(cards(view)).toHaveLength(1)
    expect(view.get('[data-testid="group-intelligence-viewport"]').element).toBe(viewport)
  })

  it('keeps an already-loaded empty layout mounted during background polling and disables creation until it completes', async () => {
    const view = render()
    await flushPromises()
    const empty = view.get('[data-testid="group-intelligence-empty"]').element
    const response = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(response.promise)
    await vi.advanceTimersByTimeAsync(5000)
    expect(view.get('[data-testid="group-intelligence-empty"]').element).toBe(empty)
    expect(view.get('[data-testid="group-intelligence-create"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    expect(view.find('[data-testid="plan-editor"]').exists()).toBe(false)
    response.resolve({ items: [] })
    await flushPromises()
    expect(view.get('[data-testid="group-intelligence-empty"]').element).toBe(empty)
    expect(view.get('[data-testid="group-intelligence-create"]').attributes('disabled')).toBeUndefined()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    expect(view.find('[data-testid="plan-editor"]').exists()).toBe(true)
  })

  it('opens a source-locked editor and shows its saved plan immediately without generating on save', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    const editor = view.getComponent({ name: 'IntelligencePlanDialog' })
    expect(editor.props()).toEqual({ show: true, plan: null, overview, upstreamTargetId: 11 })
    mocks.plans.mockRejectedValue(new Error('refresh unavailable'))
    editor.vm.$emit('saved', plan())
    editor.vm.$emit('close')
    await flushPromises()
    expect(view.find('[data-testid="plan-editor"]').exists()).toBe(false)
    expect(cards(view)).toHaveLength(1)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    expect(view.emitted('changed')).toHaveLength(1)
    expect(mocks.run).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    expect(view.get('[role="alert"]').text()).toBeTruthy()
  })

  it('accepts a saved event without a plan by refreshing the scoped list', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    mocks.plans.mockResolvedValue({ items: [plan()] })
    view.getComponent({ name: 'IntelligencePlanDialog' }).vm.$emit('saved')
    await flushPromises()
    expect(cards(view)).toHaveLength(1)
    expect(mocks.plans).toHaveBeenLastCalledWith(expect.any(AbortSignal), 11)
    expect(mocks.run).not.toHaveBeenCalled()
  })

  it('refreshes when closing settings so a plan created in another window is immediately shown', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-testid="group-intelligence-create"]').trigger('click')
    mocks.plans.mockResolvedValue({ items: [plan()] })
    view.getComponent({ name: 'IntelligencePlanDialog' }).vm.$emit('close')
    await flushPromises()
    expect(cards(view)).toHaveLength(1)
    expect(view.find('[data-testid="group-intelligence-create"]').exists()).toBe(false)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    expect(mocks.run).not.toHaveBeenCalled()
    expect(view.emitted('changed')).toBeUndefined()
  })

  it('polls once per dialog, at two seconds for active work and five seconds when idle, preserving unchanged cards', async () => {
    const running = { id: 91, status: 'running' } as IntelligenceRun
    mocks.plans.mockImplementation(() => Promise.resolve({ items: [plan(1, { latest_run: { ...running } }), plan(2)] }))
    const view = render()
    await flushPromises()
    const original = cards(view)[0]!.props('plan')
    const element = cards(view)[0]!.element
    await vi.advanceTimersByTimeAsync(2000)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    expect(cards(view)[0]!.props('plan')).toBe(original)
    expect(cards(view)[0]!.element).toBe(element)
    mocks.plans.mockResolvedValue({ items: [plan(1, { latest_run: { ...running, status: 'succeeded' } }), plan(2)] })
    await vi.advanceTimersByTimeAsync(2000)
    expect(mocks.plans).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(4999)
    expect(mocks.plans).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.plans).toHaveBeenCalledTimes(4)
  })

  it('manual refresh retries previews and overview while superseding an outstanding read', async () => {
    mocks.plans.mockResolvedValue({ items: [plan()] })
    const view = render()
    await flushPromises()
    const old = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(old.promise)
    await refresh(view)
    const signal = mocks.plans.mock.calls.at(-1)![0] as AbortSignal
    mocks.plans.mockResolvedValue({ items: [plan(2)] })
    await refresh(view)
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(view.emitted('refreshOverview')).toHaveLength(2)
    expect(cards(view)[0]!.attributes('data-revision')).toBe('2')
    old.resolve({ items: [plan()] })
    await flushPromises()
    expect(cards(view)[0]!.props('plan').id).toBe(2)
  })

  it('only runs through the explicit card action and retains its accepted run when refreshing fails', async () => {
    mocks.plans.mockResolvedValue({ items: [plan()] })
    const view = render()
    await flushPromises()
    const response = deferred<IntelligenceRun>()
    mocks.run.mockReturnValueOnce(response.promise)
    cards(view)[0]!.vm.$emit('run')
    cards(view)[0]!.vm.$emit('run')
    await flushPromises()
    expect(mocks.run).toHaveBeenCalledTimes(1)
    expect(mocks.run).toHaveBeenCalledWith(1)
    expect(cards(view)[0]!.props('busy')).toBe(true)
    const queued = { id: 91, plan_id: 1, status: 'pending' } as IntelligenceRun
    mocks.plans.mockRejectedValue(new Error('offline'))
    response.resolve(queued)
    await flushPromises()
    expect(cards(view)[0]!.props('plan').latest_run).toEqual(queued)
    expect(cards(view)[0]!.props('busy')).toBe(false)
    expect(view.emitted('changed')).toHaveLength(1)
  })

  it('updates the schedule while preserving results even when the list reload fails', async () => {
    const run = { id: 91, status: 'succeeded' } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [plan(1, { latest_run: run, recent_runs: [run] })] })
    const view = render()
    await flushPromises()
    mocks.update.mockResolvedValue(plan(1, { enabled: true, next_run_at: '2026-09-27T12:00:00Z' }))
    mocks.plans.mockRejectedValue(new Error('offline'))
    cards(view)[0]!.vm.$emit('toggle')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, { enabled: true })
    expect(cards(view)[0]!.props('plan')).toMatchObject({ enabled: true, latest_run: run, recent_runs: [run] })
    expect(mocks.run).not.toHaveBeenCalled()
  })

  it.each(['edit', 'history', 'archive'] as const)('keeps the parent open and pauses its previews while the %s dialog is open', async action => {
    mocks.plans.mockResolvedValue({ items: [plan()] })
    const view = render()
    await flushPromises()
    cards(view)[0]!.vm.$emit(action, action === 'history' ? 91 : undefined)
    await flushPromises()
    const parent = view.getComponent({ name: 'BaseDialog' })
    expect(parent.props('closeOnEscape')).toBe(false)
    expect(parent.props('showCloseButton')).toBe(true)
    expect(cards(view)[0]!.attributes('data-active')).toBe('false')
    parent.vm.$emit('close')
    expect(view.emitted('close')).toBeUndefined()
    const name = action === 'edit' ? 'IntelligencePlanDialog' : action === 'history' ? 'IntelligenceHistoryDialog' : 'UpstreamDeleteDialog'
    const child = view.getComponent({ name })
    if (action === 'history') {
      expect(child.props('initialRunId')).toBe(91)
      expect(child.attributes('data-active')).toBe('true')
    }
    if (action === 'edit') expect(child.props('upstreamTargetId')).toBe(11)
    child.vm.$emit('close')
    await flushPromises()
    expect(parent.props('closeOnEscape')).toBe(true)
    expect(parent.props('showCloseButton')).toBe(true)
    expect(cards(view)[0]!.attributes('data-active')).toBe('true')
    parent.vm.$emit('close')
    await flushPromises()
    expect(view.emitted('close')).toHaveLength(1)
  })

  it.each(['archive', 'purge'] as const)('removes only the selected linked plan through %s', async mode => {
    mocks.plans.mockResolvedValue({ items: [plan(1), plan(2)] })
    const view = render()
    await flushPromises()
    cards(view)[0]!.vm.$emit('archive')
    await flushPromises()
    expect(view.getComponent({ name: 'UpstreamDeleteDialog' }).props('item')).toEqual({ kind: 'intelligence', id: 1, name: 'Premium plan' })
    mocks.plans.mockResolvedValue({ items: [plan(2)] })
    view.getComponent({ name: 'UpstreamDeleteDialog' }).vm.$emit('confirm', mode)
    await flushPromises()
    if (mode === 'archive') expect(mocks.archive).toHaveBeenCalledWith(1)
    else expect(mocks.purge).toHaveBeenCalledWith({ kind: 'intelligence', id: 1, confirm_name: 'Premium plan' })
    expect(cards(view).map(card => card.props('plan').id)).toEqual([2])
    expect(view.emitted('changed')).toHaveLength(1)
    expect(view.find('[data-testid="plan-delete"]').exists()).toBe(false)
  })

  it('shows failed deletion inside its child dialog and keeps the parent protected', async () => {
    mocks.plans.mockResolvedValue({ items: [plan()] })
    const view = render()
    await flushPromises()
    cards(view)[0]!.vm.$emit('archive')
    await flushPromises()
    mocks.archive.mockRejectedValue(new Error('busy'))
    view.getComponent({ name: 'UpstreamDeleteDialog' }).vm.$emit('confirm', 'archive')
    await flushPromises()
    expect(view.getComponent({ name: 'UpstreamDeleteDialog' }).props('error')).toBeTruthy()
    expect(view.getComponent({ name: 'BaseDialog' }).props('closeOnEscape')).toBe(false)
    expect(cards(view)).toHaveLength(1)
    expect(view.emitted('changed')).toBeUndefined()
  })

  it.each(['close', 'unmount'] as const)('aborts reads on %s and ignores their late result', async event => {
    const response = deferred<{ items: IntelligencePlan[] }>()
    mocks.plans.mockReturnValueOnce(response.promise)
    const view = render()
    const signal = mocks.plans.mock.calls[0]![0] as AbortSignal
    if (event === 'close') view.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
    else { view.unmount(); wrapper = undefined }
    await flushPromises()
    expect(signal.aborted).toBe(true)
    response.resolve({ items: [plan()] })
    await flushPromises()
    if (event === 'close') expect(cards(view)).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(20000)
    expect(mocks.plans).toHaveBeenCalledTimes(1)
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it.each(['success', 'failure'] as const)('ignores a late action %s after the dialog unmounts', async result => {
    mocks.plans.mockResolvedValue({ items: [plan()] })
    const view = render()
    await flushPromises()
    const response = deferred<IntelligenceRun>()
    mocks.run.mockReturnValueOnce(response.promise)
    cards(view)[0]!.vm.$emit('run')
    await flushPromises()
    view.unmount()
    wrapper = undefined
    if (result === 'success') response.resolve({ id: 91, status: 'pending' } as IntelligenceRun)
    else response.reject(new Error('late network failure'))
    await flushPromises()
    expect(mocks.plans).toHaveBeenCalledTimes(1)
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(view.emitted('changed')).toBeUndefined()
  })
})
