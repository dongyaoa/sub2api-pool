import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun, IntelligenceSource } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import IntelligenceMonitorPanel from './IntelligenceMonitorPanel.vue'

const mocks = vi.hoisted(() => ({
  plans: vi.fn(), create: vi.fn(), update: vi.fn(), archive: vi.fn(), run: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(),
  purge: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({
  intelligenceMonitorAPI: mocks, PELICAN_MODEL: 'gpt-6-astra', PELICAN_PROMPT: 'Pelican animation',
}))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { purge: mocks.purge } }))
vi.mock('./IntelligencePlanCard.vue', () => ({ default: {
  name: 'IntelligencePlanCard', props: ['plan', 'overview', 'busy', 'visible'],
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
function cardIDs(view: VueWrapper) { return view.findAll('[data-testid="plan-card"]').filter(card => card.attributes('hidden') === undefined).map(card => Number(card.attributes('data-id'))) }
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  mocks.plans.mockResolvedValue({ items: original() })
})

describe('intelligence monitoring site tabs', () => {
  const items = () => [
    { ...plan(1, 'Alpha premium', 'upstream'), upstream_target_id: 11 },
    { ...plan(2, 'Alpha standard', 'upstream'), upstream_target_id: 12 },
    { ...plan(3, 'Beta premium', 'upstream'), upstream_target_id: 21 },
    { ...plan(4, 'External A'), endpoint: 'https://relay.example/v1/responses' },
    { ...plan(5, 'External B'), endpoint: 'https://relay.example/v1' },
    { ...plan(6, 'Local A', 'local_group'), group_id: 1 },
    { ...plan(7, 'Local B', 'local_group'), group_id: 2 },
    { ...plan(8, 'Noted A'), endpoint: 'https://first.example/v1', supplier_note: 'Named relay' },
    { ...plan(9, 'Noted B'), endpoint: 'https://second.example/v1', supplier_note: 'Named relay' },
  ]
  async function renderSites() {
    mocks.plans.mockResolvedValue({ items: items() })
    const view = render()
    await view.setProps({ overview: { suppliers: [
      { id: 1, name: 'Alpha relay', targets: [{ id: 11 }, { id: 12 }] },
      { id: 2, name: 'Beta relay', targets: [{ id: 21 }] },
    ] } as UpstreamOverview })
    await flushPromises()
    return view
  }
  const site = (view: VueWrapper, key: string) => view.get(`[data-site="${key}"]`)

  it('groups plans by supplier, external provider note or origin, and local site with plan counts', async () => {
    const view = await renderSites()
    const tabs = view.findAll('[data-testid="intelligence-site-tabs"] [role="tab"]')
    expect(tabs.map(tab => tab.text())).toEqual(['intelligenceMonitor.allSites9', 'Alpha relay2', 'Beta relay1', 'relay.example2', 'intelligenceMonitor.source.local_group2', 'Named relay2'])
    await site(view, 'upstream:1').trigger('click')
    expect(cardIDs(view)).toEqual([1, 2])
    await site(view, 'external:https://relay.example').trigger('click')
    expect(cardIDs(view)).toEqual([4, 5])
    await site(view, 'external:note:Named relay').trigger('click')
    expect(cardIDs(view)).toEqual([8, 9])
    await site(view, 'local').trigger('click')
    expect(cardIDs(view)).toEqual([6, 7])
  })

  it('keeps the same cards while filtering, refreshing or leaving the panel, and retains the selected site', async () => {
    const view = await renderSites()
    const firstCard = view.get('[data-id="1"]').element
    await site(view, 'upstream:1').trigger('click')
    expect(view.findAllComponents({ name: 'IntelligencePlanCard' }).find(card => card.props('plan').id === 3)?.props('visible')).toBe(false)
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('premium')
    expect(cardIDs(view)).toEqual([1])
    await view.get('[data-testid="source-filter"]').setValue('external')
    expect(cardIDs(view)).toEqual([])
    expect(view.text()).toContain('intelligenceMonitor.noMatches')
    expect(view.get('[data-id="1"]').element).toBe(firstCard)
    await view.get('[data-testid="source-filter"]').setValue('')
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('')
    await view.setProps({ active: false })
    await view.setProps({ active: true })
    await flushPromises()
    expect(site(view, 'upstream:1').attributes('aria-selected')).toBe('true')
    expect(cardIDs(view)).toEqual([1, 2])
    expect(view.get('[data-id="1"]').element).toBe(firstCard)
    expect(orderButton(view).attributes('disabled')).toBeUndefined()
  })

  it('follows saved plan order and falls back to all sites when the selected site has no remaining plans', async () => {
    const view = await renderSites()
    await site(view, 'upstream:1').trigger('click')
    mocks.plans.mockResolvedValue({ items: items().filter(item => ![1, 2].includes(item.id)).reverse() })
    await refreshButton(view).trigger('click')
    await flushPromises()
    expect(site(view, 'all').attributes('aria-selected')).toBe('true')
    expect(view.find('[data-site="upstream:1"]').exists()).toBe(false)
    expect(cardIDs(view)).toEqual([9, 8, 7, 6, 5, 4, 3])
    expect(view.findAll('[role="tab"]')[1].text()).toBe('Named relay2')
  })

  it('supports keyboard tab switching and labels the corresponding results', async () => {
    const view = await renderSites()
    await site(view, 'all').trigger('keydown', { key: 'ArrowRight' })
    expect(cardIDs(view)).toEqual([1, 2])
    expect(site(view, 'upstream:1').attributes('tabindex')).toBe('0')
    expect(view.get('#intelligence-site-results').attributes('aria-labelledby')).toBe(site(view, 'upstream:1').attributes('id'))
    await site(view, 'upstream:1').trigger('keydown', { key: 'End' })
    expect(cardIDs(view)).toEqual([8, 9])
    await site(view, 'external:note:Named relay').trigger('keydown', { key: 'Home' })
    expect(cardIDs(view)).toHaveLength(9)
  })

  it('does not add site tabs to the OAuth account panel', async () => {
    const view = render(true)
    await flushPromises()
    expect(view.find('[data-testid="intelligence-site-tabs"]').exists()).toBe(false)
    expect(cardIDs(view)).toEqual([4, 5])
  })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers() })

describe('intelligence monitoring manual order', () => {
  it('refreshes live generations every two seconds and immediately retries an outstanding manual refresh', async () => {
    const running = { ...plan(1, 'Live'), latest_run: { id: 10, plan_id: 1, status: 'running' } as IntelligenceRun }
    mocks.plans.mockResolvedValue({ items: [running] })
    const view = render(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    let resolveOld!: (value: { items: IntelligencePlan[] }) => void
    mocks.plans.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await refreshButton(view).trigger('click')
    const oldSignal = mocks.plans.mock.calls.at(-1)![0] as AbortSignal
    const completed = { ...running, latest_run: { ...running.latest_run!, status: 'succeeded' } as IntelligenceRun }
    mocks.plans.mockResolvedValue({ items: [completed] })
    expect(refreshButton(view).attributes('disabled')).toBeUndefined()
    await refreshButton(view).trigger('click'); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(view.emitted('refreshOverview')).toHaveLength(2)
    resolveOld({ items: [running] }); await flushPromises()
    expect(view.getComponent({ name: 'IntelligencePlanCard' }).props('plan').latest_run.status).toBe('succeeded')
  })

  it('shows the accepted queued run even if the follow-up list request fails', async () => {
    const view = render(); await flushPromises()
    const queued = { id: 101, plan_id: 1, status: 'pending' } as IntelligenceRun
    mocks.run.mockResolvedValue(queued)
    mocks.plans.mockRejectedValue(new Error('temporary network failure'))
    view.findAllComponents({ name: 'IntelligencePlanCard' })[0]!.vm.$emit('run')
    await flushPromises()
    expect(view.getComponent({ name: 'IntelligencePlanCard' }).props('plan').latest_run).toEqual(queued)
    expect(view.get('[role="alert"]').text()).toBeTruthy()
  })
  it.each(['archive', 'purge'] as const)('supports %s for an OAuth monitor without removing its source account', async mode => {
    const view = render(true); await flushPromises()
    view.findAllComponents({ name: 'IntelligencePlanCard' })[0]!.vm.$emit('archive')
    await flushPromises()
    const removal = view.getComponent({ name: 'UpstreamDeleteDialog' })
    expect(removal.props('item')).toEqual({ kind: 'intelligence', id: 4, name: 'OAuth A' })
    removal.vm.$emit('confirm', mode); await flushPromises()
    if (mode === 'purge') { expect(mocks.purge).toHaveBeenCalledWith({ kind: 'intelligence', id: 4, confirm_name: 'OAuth A' }); expect(mocks.archive).not.toHaveBeenCalled() }
    else { expect(mocks.archive).toHaveBeenCalledWith(4); expect(mocks.purge).not.toHaveBeenCalled() }
    expect(removal.props('show')).toBe(false)
  })
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

  it('retains cards and pauses background reads when a tab is hidden, then refreshes in place', async () => {
    const view = render()
    await flushPromises()
    const firstCard = view.get('[data-testid="plan-card"]').element
    await view.setProps({ active: false })
    await vi.advanceTimersByTimeAsync(15000)
    expect(mocks.plans).toHaveBeenCalledTimes(1)
    expect(view.get('[data-testid="plan-card"]').element).toBe(firstCard)
    await view.setProps({ active: true })
    await flushPromises()
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    expect(view.get('[data-testid="plan-card"]').element).toBe(firstCard)
    expect(cardIDs(view)).toEqual([1, 2, 3])
  })

  it('aborts a pending read on hide and ignores its late response', async () => {
    const view = render()
    await flushPromises()
    let resolveOld!: (value: { items: IntelligencePlan[] }) => void
    mocks.plans.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await refreshButton(view).trigger('click')
    const signal = mocks.plans.mock.calls.at(-1)![0] as AbortSignal
    await view.setProps({ active: false })
    expect(signal.aborted).toBe(true)
    resolveOld({ items: [] })
    await flushPromises()
    expect(cardIDs(view)).toEqual([1, 2, 3])
    expect(refreshButton(view).attributes('disabled')).toBeUndefined()
  })
})
