import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceSource } from '@/api/admin/intelligenceMonitor'
import IntelligenceMonitorPanel from './IntelligenceMonitorPanel.vue'

const mocks = vi.hoisted(() => ({
  plans: vi.fn(), create: vi.fn(), update: vi.fn(), archive: vi.fn(), run: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({
  intelligenceMonitorAPI: mocks, PELICAN_MODEL: 'gpt-6-astra', PELICAN_PROMPT: 'Pelican animation',
}))
vi.mock('./IntelligencePlanCard.vue', () => ({ default: {
  name: 'IntelligencePlanCard', props: ['plan', 'overview', 'busy'],
  template: '<div data-testid="plan-card" :data-id="plan.id">{{ plan.name }}</div>',
} }))
vi.mock('./IntelligencePlanDialog.vue', () => ({ default: { name: 'IntelligencePlanDialog', template: '<div />' } }))
vi.mock('./IntelligenceHistoryDialog.vue', () => ({ default: { name: 'IntelligenceHistoryDialog', template: '<div />' } }))
vi.mock('./UpstreamOrderDialog.vue', () => ({ default: {
  name: 'UpstreamOrderDialog', props: ['show', 'scope'], emits: ['close', 'saved'],
  template: '<div v-if="show" data-testid="order-dialog" :data-scope="scope"><button data-testid="save-order" @click="$emit(\'saved\')">Save order</button><button data-testid="cancel-order" @click="$emit(\'close\')">Cancel order</button></div>',
} }))

function plan(id: number, name: string, source_type: IntelligenceSource = 'external'): IntelligencePlan {
  return {
    id, name, source_type, supplier_note: '', group_note: '', rate_note: '', notes: '',
    api_mode: 'responses', enabled: false, interval_seconds: 3600, timeout_seconds: 900,
    model: 'gpt-6-astra', reasoning_effort: 'high', prompt: 'Pelican animation', api_key_masked: '',
    source_name: name, rate_snapshot: null, created_by: 1, created_at: '', updated_at: '',
    last_run_at: null, next_run_at: null, latest_run: null,
  }
}
const original = () => [plan(1, 'Alpha external'), plan(2, 'Beta upstream', 'upstream'), plan(3, 'Gamma local', 'local_group'), plan(4, 'OAuth A', 'openai_oauth'), plan(5, 'OAuth B', 'openai_oauth')]
let wrapper: VueWrapper | undefined
function render(oauthOnly = false) {
  wrapper = mount(IntelligenceMonitorPanel, {
    props: { overview: null, oauthOnly },
    global: { stubs: { Icon: true, BaseDialog: true, Select: { props: ['modelValue'], emits: ['update:modelValue'], template: '<input data-testid="source-filter" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' } } },
  })
  return wrapper
}
function orderButton(view: VueWrapper) { return view.findAll('button').find(button => button.text() === 'upstreamCenter.order.open')! }
function refreshButton(view: VueWrapper) { return view.findAll('button').find(button => button.text() === 'intelligenceMonitor.refresh')! }
function cardIDs(view: VueWrapper) { return view.findAll('[data-testid="plan-card"]').map(card => Number(card.attributes('data-id'))) }
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  mocks.plans.mockResolvedValue({ items: original() })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers() })

describe('intelligence monitoring manual order', () => {
  it.each([{ oauthOnly: false, scope: 'intelligence', ids: [1, 2, 3] }, { oauthOnly: true, scope: 'oauth', ids: [4, 5] }])('opens the full $scope list with an independent scope', async ({ oauthOnly, scope, ids }) => {
    const view = render(oauthOnly)
    await flushPromises()
    expect(cardIDs(view)).toEqual(ids)
    expect(orderButton(view).attributes('disabled')).toBeUndefined()
    await orderButton(view).trigger('click')
    expect(view.get('[data-testid="order-dialog"]').attributes('data-scope')).toBe(scope)
    expect(view.getComponent({ name: 'UpstreamOrderDialog' }).props()).toEqual({ show: true, scope })
  })

  it('keeps sorting available for the full scope even when search and source filters hide every card', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-testid="source-filter"]').setValue('upstream')
    expect(cardIDs(view)).toEqual([2])
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('no matching plan')
    expect(cardIDs(view)).toEqual([])
    expect(orderButton(view).attributes('disabled')).toBeUndefined()
    await orderButton(view).trigger('click')
    expect(view.getComponent({ name: 'UpstreamOrderDialog' }).props()).toEqual({ show: true, scope: 'intelligence' })
  })

  it.each([false, true])('disables sorting when the current scope has fewer than two plans (OAuth: %s)', async oauthOnly => {
    mocks.plans.mockResolvedValue({ items: oauthOnly ? original().filter(item => item.id !== 5) : original().filter(item => ![2, 3].includes(item.id)) })
    const view = render(oauthOnly)
    await flushPromises()
    expect(cardIDs(view)).toHaveLength(1)
    expect(orderButton(view).attributes('disabled')).toBeDefined()
    await orderButton(view).trigger('click')
    expect(view.find('[data-testid="order-dialog"]').exists()).toBe(false)
  })

  it('reloads the saved server order without repeating the dialog success toast or accepting an older read', async () => {
    const view = render()
    await flushPromises()
    let resolveOlder!: (value: { items: IntelligencePlan[] }) => void
    mocks.plans.mockReturnValueOnce(new Promise(resolve => { resolveOlder = resolve }))
    await refreshButton(view).trigger('click')
    const olderSignal = mocks.plans.mock.calls[1][0] as AbortSignal
    await orderButton(view).trigger('click')
    const reordered = [original()[2], original()[0], original()[1], original()[3], original()[4]]
    mocks.plans.mockResolvedValue({ items: reordered })
    await view.get('[data-testid="save-order"]').trigger('click')
    await flushPromises()
    expect(olderSignal.aborted).toBe(true)
    expect(view.find('[data-testid="order-dialog"]').exists()).toBe(false)
    expect(cardIDs(view)).toEqual([3, 1, 2])
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    resolveOlder({ items: original() })
    await flushPromises()
    expect(cardIDs(view)).toEqual([3, 1, 2])
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('a')
    expect(cardIDs(view)).toEqual([3, 1, 2])
    await view.get('[data-testid="source-filter"]').setValue('external')
    expect(cardIDs(view)).toEqual([1])
    await view.get('[data-testid="source-filter"]').setValue('')
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.plans).toHaveBeenCalledTimes(4)
    expect(cardIDs(view)).toEqual([3, 1, 2])
  })

  it('pauses polling while sorting and cancels without writing or reloading the list', async () => {
    const view = render(true)
    await flushPromises()
    await orderButton(view).trigger('click')
    await vi.advanceTimersByTimeAsync(15000)
    expect(mocks.plans).toHaveBeenCalledTimes(1)
    await view.get('[data-testid="cancel-order"]').trigger('click')
    expect(view.find('[data-testid="order-dialog"]').exists()).toBe(false)
    expect(cardIDs(view)).toEqual([4, 5])
    expect(mocks.plans).toHaveBeenCalledTimes(1)
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.archive).not.toHaveBeenCalled()
    expect(mocks.run).not.toHaveBeenCalled()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
  })
})
