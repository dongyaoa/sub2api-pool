import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan, IntelligenceRun, IntelligenceSource } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import IntelligenceMonitorPanel from './IntelligenceMonitorPanel.vue'

const mocks = vi.hoisted(() => ({
  plans: vi.fn(), create: vi.fn(), update: vi.fn(), archive: vi.fn(), run: vi.fn(), runCandy: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn(),
  purge: vi.fn(), permanentDelete: vi.fn(), setAllEnabled: vi.fn(),
  publicConfig: vi.fn(),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('@/stores/pelicanMonitor', () => ({ usePelicanMonitorStore: () => ({ load: mocks.publicConfig }) }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({
  intelligenceMonitorAPI: mocks, PELICAN_MODEL: 'gpt-6-astra', PELICAN_PROMPT: 'Pelican animation',
}))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { purge: mocks.purge } }))
vi.mock('./IntelligencePlanCard.vue', () => ({ default: {
  name: 'IntelligencePlanCard', props: ['plan', 'overview', 'busy', 'visible'],
  template: '<div data-testid="plan-card" :data-id="plan.id">{{ plan.name }}</div>',
} }))
vi.mock('./IntelligenceLocalCard.vue', () => ({ default: { name: 'IntelligenceLocalCard', props: ['plan', 'busy', 'visible'], template: '<div data-testid="plan-card" :data-id="plan.id">{{ plan.name }}</div>' } }))
vi.mock('./IntelligencePlanDialog.vue', () => ({ default: { name: 'IntelligencePlanDialog', props: ['monitoredPlans'], template: '<div />' } }))
vi.mock('./IntelligenceHistoryDialog.vue', () => ({ default: { name: 'IntelligenceHistoryDialog', template: '<div />' } }))
vi.mock('./IntelligencePublicDisplayDialog.vue', () => ({ default: { name: 'IntelligencePublicDisplayDialog', props: ['show'], emits: ['close', 'saved'], template: '<div v-if="show" data-testid="public-display-dialog" />' } }))
vi.mock('./IntelligenceCandyDetailDialog.vue', () => ({ default: { name: 'IntelligenceCandyDetailDialog', props: ['run'], emits: ['close'], template: '<div data-testid="candy-detail" />' } }))
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
function render(oauthOnly = false, localOnly = false) {
  wrapper = mount(IntelligenceMonitorPanel, {
    props: { overview: null, oauthOnly, localOnly },
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
  it.each([
    ['INTELLIGENCE_OAUTH_COOLING_DOWN', 'weekly_limited', 'cooldownHint'],
    ['INTELLIGENCE_OAUTH_UNAVAILABLE', 'unavailable', 'unavailableHint'],
  ] as const)('refreshes account status after a stale manual request receives %s', async (reason, status, hint) => {
    const item = { ...plan(4, 'OAuth A', 'openai_oauth'), account_id: 41 }
    mocks.plans.mockResolvedValueOnce({ items: [item] }).mockResolvedValue({ items: [{ ...item, oauth_account_status: { status, monitoring_paused: true } }] })
    mocks.run.mockRejectedValueOnce({ reason, message: 'Account paused' })
    const view = render(true); await flushPromises()
    view.getComponent({ name: 'IntelligencePlanCard' }).vm.$emit('run'); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith(`intelligenceMonitor.oauth.${hint}`)
    expect(view.getComponent({ name: 'IntelligencePlanCard' }).props('plan').oauth_account_status).toEqual({ status, monitoring_paused: true })
    expect(mocks.plans).toHaveBeenCalledTimes(2)
  })
  it('reserves paused OAuth accounts in the picker and prevents manual runs during weekly cooldown', async () => {
    const items = [
      { ...plan(4, 'Weekly account', 'openai_oauth'), account_id: 41, enabled: true, candy_enabled: true, oauth_account_status: { status: 'weekly_limited' as const, monitoring_paused: true } },
      { ...plan(5, 'Paused account', 'openai_oauth'), account_id: 51, enabled: false },
    ]
    mocks.plans.mockResolvedValue({ items })
    const view = render(true); await flushPromises()
    expect(view.getComponent({ name: 'IntelligencePlanDialog' }).props('monitoredPlans')).toEqual(items)
    const card = view.getComponent({ name: 'IntelligencePlanCard' })
    card.vm.$emit('run'); card.vm.$emit('candy-run'); await flushPromises()
    expect(mocks.run).not.toHaveBeenCalled(); expect(mocks.runCandy).not.toHaveBeenCalled()
    card.vm.$emit('toggle'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(4, { enabled: false })
    mocks.plans.mockResolvedValue({ items: [items[1]] })
    await refreshButton(view).trigger('click'); await flushPromises()
    expect(view.getComponent({ name: 'IntelligencePlanDialog' }).props('monitoredPlans')).toEqual([items[1]])
  })
  it.each([[false, false], [true, false]])('hides user-display management outside local monitoring (OAuth %s, local %s)', async (oauth, local) => {
    const view = render(oauth, local); await flushPromises()
    expect(view.find('[data-testid="open-public-display"]').exists()).toBe(false)
    expect(view.findComponent({ name: 'IntelligencePublicDisplayDialog' }).exists()).toBe(false)
  })
  it('opens user-display settings only on local monitoring and closes them when leaving the tab', async () => {
    const view = render(false, true); await flushPromises()
    await view.get('[data-testid="open-public-display"]').trigger('click')
    expect(view.getComponent({ name: 'IntelligencePublicDisplayDialog' }).props('show')).toBe(true)
    await view.setProps({ active: false })
    expect(view.getComponent({ name: 'IntelligencePublicDisplayDialog' }).props('show')).toBe(false)
  })
  it('refreshes user sidebar visibility immediately after saving display settings without scheduling a test', async () => {
    const view = render(false, true); await flushPromises()
    view.getComponent({ name: 'IntelligencePublicDisplayDialog' }).vm.$emit('saved')
    await flushPromises()
    expect(mocks.publicConfig).toHaveBeenCalledWith(true)
    expect(mocks.run).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
  })
  it('pauses retained card previews while the large history dialog is open and resumes on close', async () => {
    const view = render(); await flushPromises()
    const card = view.getComponent({ name: 'IntelligencePlanCard' })
    const element = card.element
    expect(card.props('visible')).toBe(true)
    card.vm.$emit('history', 91); await flushPromises()
    expect(card.props('visible')).toBe(false)
    view.getComponent({ name: 'IntelligenceHistoryDialog' }).vm.$emit('close'); await flushPromises()
    expect(card.props('visible')).toBe(true)
    expect(card.element).toBe(element)
  })
  it('renders local groups only in their independent workbench while retaining the shared refresh and action pipeline', async () => {
    const item = { ...plan(8, 'Primary local', 'local_group'), candy_enabled: true, candy_latest_run: { id: 101, plan_id: 8, status: 'running', test_kind: 'candy' } as IntelligenceRun }
    mocks.plans.mockResolvedValue({ items: [...original(), item] })
    const view = render(false, true); await flushPromises()
    expect(cardIDs(view)).toEqual([3, 8])
    expect(view.find('[data-testid="local-group-workbench"]').exists()).toBe(true)
    expect(view.find('[data-testid="intelligence-site-tabs"]').exists()).toBe(false)
    expect(view.findAllComponents({ name: 'IntelligencePlanCard' })).toHaveLength(0)
    expect(view.findAllComponents({ name: 'IntelligenceLocalCard' })).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(mocks.plans).toHaveBeenCalledTimes(2)
    view.findAllComponents({ name: 'IntelligenceLocalCard' })[1]!.vm.$emit('run'); await flushPromises()
    expect(mocks.run).toHaveBeenCalledWith(8)
    await view.findAll('button').find(button => button.text() === 'intelligenceMonitor.local.add')!.trigger('click')
    expect(view.getComponent({ name: 'IntelligencePlanDialog' }).attributes('local-only')).toBe('true')
  })
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
    expect(tabs.map(tab => tab.text())).toEqual(['intelligenceMonitor.allSites7', 'Alpha relay2', 'Beta relay1', 'relay.example2', 'Named relay2'])
    await site(view, 'upstream:1').trigger('click')
    expect(cardIDs(view)).toEqual([1, 2])
    await site(view, 'external:https://relay.example').trigger('click')
    expect(cardIDs(view)).toEqual([4, 5])
    await site(view, 'external:note:Named relay').trigger('click')
    expect(cardIDs(view)).toEqual([8, 9])
    expect(view.find('[data-site="local"]').exists()).toBe(false)
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
    expect(cardIDs(view)).toEqual([9, 8, 5, 4, 3])
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
    expect(cardIDs(view)).toHaveLength(7)
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
  it.each([false, true])('runs candy only on explicit action in the matching panel and blocks duplicate generation (OAuth: %s)', async oauthOnly => {
    const item = { ...plan(1, 'Candy plan', oauthOnly ? 'openai_oauth' : 'upstream'), candy_enabled: true }
    mocks.plans.mockResolvedValue({ items: [item] })
    const view = render(oauthOnly); await flushPromises()
    const card = view.getComponent({ name: 'IntelligencePlanCard' })
    expect(mocks.runCandy).not.toHaveBeenCalled()
    let resolve!: (run: IntelligenceRun) => void
    mocks.runCandy.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    card.vm.$emit('candyRun'); card.vm.$emit('candyRun'); card.vm.$emit('run')
    await flushPromises()
    expect(mocks.runCandy).toHaveBeenCalledTimes(1)
    expect(mocks.runCandy).toHaveBeenCalledWith(1)
    expect(mocks.run).not.toHaveBeenCalled()
    expect(card.props('busy')).toBe(true)
    const queued = { id: 101, plan_id: 1, status: 'pending', test_kind: 'candy' } as IntelligenceRun
    mocks.plans.mockRejectedValue(new Error('offline')); resolve(queued); await flushPromises()
    expect(card.props('plan')).toMatchObject({ candy_latest_run: queued, latest_run: null })
    expect(card.props('busy')).toBe(false)
    card.vm.$emit('candyRun'); card.vm.$emit('run'); await flushPromises()
    expect(mocks.runCandy).toHaveBeenCalledTimes(1)
    expect(mocks.run).toHaveBeenCalledTimes(1)
  })

  it('ignores candy action while disabled and polls candy-only active work every second', async () => {
    const item = plan(1, 'Candy plan')
    mocks.plans.mockResolvedValue({ items: [item] })
    const view = render(); await flushPromises()
    view.getComponent({ name: 'IntelligencePlanCard' }).vm.$emit('candyRun'); await flushPromises()
    expect(mocks.runCandy).not.toHaveBeenCalled()
    const candy = { id: 101, plan_id: 1, status: 'running', test_kind: 'candy' } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [{ ...item, candy_enabled: true, candy_latest_run: candy }] })
    await refreshButton(view).trigger('click'); await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    expect(mocks.plans).toHaveBeenCalledTimes(3)
    mocks.plans.mockResolvedValue({ items: [{ ...item, candy_enabled: true, candy_latest_run: { ...candy, status: 'succeeded', correct: true } }] })
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(4999)
    expect(mocks.plans).toHaveBeenCalledTimes(4)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.plans).toHaveBeenCalledTimes(5)
  })

  it('updates the selected candy record from polling, pauses previews, and removes its detail when the panel hides', async () => {
    const candy = { id: 101, plan_id: 1, status: 'running', test_kind: 'candy' } as IntelligenceRun
    const item = { ...plan(1, 'Candy plan'), candy_enabled: true, candy_latest_run: candy }
    mocks.plans.mockResolvedValue({ items: [item] })
    const view = render(); await flushPromises()
    const card = view.getComponent({ name: 'IntelligencePlanCard' })
    card.vm.$emit('candySelect', candy); await flushPromises()
    expect(card.props('visible')).toBe(false)
    expect(view.getComponent({ name: 'IntelligenceCandyDetailDialog' }).props('run')).toEqual(candy)
    const completed = { ...candy, status: 'succeeded', correct: false, answer: '20' } as IntelligenceRun
    mocks.plans.mockResolvedValue({ items: [{ ...item, candy_latest_run: completed }] })
    await vi.advanceTimersByTimeAsync(1000)
    expect(view.getComponent({ name: 'IntelligenceCandyDetailDialog' }).props('run')).toEqual(completed)
    await view.setProps({ active: false })
    expect(view.find('[data-testid="candy-detail"]').exists()).toBe(false)
  })

  it.each(['resolve', 'reject'] as const)('ignores late candy %s after hiding the panel', async result => {
    mocks.plans.mockResolvedValue({ items: [{ ...plan(1, 'Candy'), candy_enabled: true }] })
    const view = render(); await flushPromises()
    let resolve!: (run: IntelligenceRun) => void, reject!: (error: Error) => void
    mocks.runCandy.mockReturnValueOnce(new Promise((yes, no) => { resolve = yes; reject = no }))
    view.getComponent({ name: 'IntelligencePlanCard' }).vm.$emit('candyRun'); await flushPromises()
    await view.setProps({ active: false })
    if (result === 'resolve') resolve({ id: 101, status: 'pending', test_kind: 'candy' } as IntelligenceRun)
    else reject(new Error('offline'))
    await flushPromises()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.plans).toHaveBeenCalledTimes(1)
  })
  it('refreshes live generations every second and immediately retries an outstanding manual refresh', async () => {
    const running = { ...plan(1, 'Live'), latest_run: { id: 10, plan_id: 1, status: 'running' } as IntelligenceRun }
    mocks.plans.mockResolvedValue({ items: [running] })
    const view = render(); await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
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
  it('permanently deletes OAuth monitoring through the quick dialog without a typed name', async () => {
    const view = render(true); await flushPromises()
    view.findAllComponents({ name: 'IntelligencePlanCard' })[0]!.vm.$emit('archive')
    await flushPromises()
    expect(view.findComponent({ name: 'UpstreamDeleteDialog' }).exists()).toBe(false)
    const removal = view.getComponent({ name: 'IntelligencePermanentDeleteDialog' })
    expect(removal.props('name')).toBe('OAuth A')
    removal.vm.$emit('confirm'); await flushPromises()
    expect(mocks.permanentDelete).toHaveBeenCalledWith(4)
    expect(mocks.archive).not.toHaveBeenCalled()
    expect(mocks.purge).not.toHaveBeenCalled()
    expect(view.findComponent({ name: 'IntelligencePermanentDeleteDialog' }).exists()).toBe(false)
  })
  it('stops and starts all sources regardless of the current tab and search filter', async () => {
    let items = original().map(item => ({ ...item, enabled: true }))
    mocks.plans.mockImplementation(async () => ({ items }))
    mocks.setAllEnabled.mockImplementation(async (enabled: boolean) => {
      items = items.map(item => ({ ...item, enabled }))
      return { total: 5, enabled: enabled ? 5 : 0, updated: 5 }
    })
    const view = render(true); await flushPromises()
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('nothing matches')
    expect(cardIDs(view)).toEqual([])
    await view.get('[data-testid="stop-all-monitoring"]').trigger('click'); await flushPromises()
    expect(mocks.setAllEnabled).toHaveBeenCalledWith(false)
    expect(items.every(item => !item.enabled)).toBe(true)
    expect(view.get('[data-testid="stop-all-monitoring"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="start-all-monitoring"]').trigger('click'); await flushPromises()
    expect(mocks.setAllEnabled).toHaveBeenLastCalledWith(true)
    expect(items.every(item => item.enabled)).toBe(true)
    expect(mocks.run).not.toHaveBeenCalled()
    expect(mocks.runCandy).not.toHaveBeenCalled()
  })
  it('does not issue duplicate bulk requests or claim success after a failure', async () => {
    const view = render(); await flushPromises()
    let reject!: (cause: unknown) => void
    mocks.setAllEnabled.mockReturnValueOnce(new Promise((_resolve, no) => { reject = no }))
    const start = view.get('[data-testid="start-all-monitoring"]')
    await start.trigger('click'); await start.trigger('click')
    expect(mocks.setAllEnabled).toHaveBeenCalledTimes(1)
    reject(new Error('offline')); await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('offline')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(view.get('[data-testid="start-all-monitoring"]').attributes('disabled')).toBeUndefined()
  })
  it.each([{ oauthOnly: false, scope: 'intelligence', ids: [1, 2] }, { oauthOnly: true, scope: 'oauth', ids: [4, 5] }])('opens the full $scope list with an independent scope', async ({ oauthOnly, scope, ids }) => {
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
    expect(cardIDs(view)).toEqual([1, 2])
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    resolveOlder({ items: original() })
    await flushPromises()
    expect(cardIDs(view)).toEqual([1, 2])
    await view.get('[aria-label="intelligenceMonitor.search"]').setValue('a')
    expect(cardIDs(view)).toEqual([1, 2])
    await view.get('[data-testid="source-filter"]').setValue('external')
    expect(cardIDs(view)).toEqual([1])
    await view.get('[data-testid="source-filter"]').setValue('')
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.plans).toHaveBeenCalledTimes(4)
    expect(cardIDs(view)).toEqual([1, 2])
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
    expect(cardIDs(view)).toEqual([1, 2])
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
    expect(cardIDs(view)).toEqual([1, 2])
    expect(refreshButton(view).attributes('disabled')).toBeUndefined()
  })
})
