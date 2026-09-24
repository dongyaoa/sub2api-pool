import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
import UpstreamDetailDialog from './UpstreamDetailDialog.vue'

const history = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { history } }))
vi.mock('./UpstreamHistoryBar.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /></div>' })
const finance = defineComponent({ template: '<div data-testid="finance-panel" />' })
function record(model: string): UpstreamHistoryRecord {
  return { id: model === 'model-a' ? 1 : 2, target_id: 9, model, status: 'operational', latency_ms: 10, ping_latency_ms: null, http_status: 200, message: `${model} record detail`, checked_at: '2026-09-24T00:00:00Z', cost: null, cost_source: 'unknown' }
}
function target(): UpstreamTarget {
  return { id: 9, supplier_id: 2, name: 'Group', endpoint: 'https://upstream.example', api_key_masked: 'sk-***', models: ['model-a', 'model-b'], interval_seconds: 300, statistics: [], balance: null } as unknown as UpstreamTarget
}
let wrapper: VueWrapper | undefined
function render(selected: UpstreamHistoryRecord | null = null) {
  wrapper = mount(UpstreamDetailDialog, { props: { show: true, supplier: null, target: target(), model: 'model-a', record: selected, window: '24h', busy: false }, global: { stubs: { BaseDialog: dialog, UpstreamFinancePanel: finance, UpstreamHistoryBar: true, UpstreamWallet: true, UpstreamStatusBadge: true, Icon: true, Pagination: true, teleport: true, transition: true } } })
  return wrapper
}
async function choose(view: VueWrapper, selector: string, label: string) {
  const trigger = view.get(selector)
  await trigger.trigger('click')
  expect(trigger.attributes('aria-expanded')).toBe('true')
  expect(view.find('.select-search-input').exists()).toBe(false)
  await view.findAll('[role="option"]').find(option => option.text() === label)!.trigger('click')
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(trigger.text()).toBe(label)
}
beforeEach(() => { vi.resetAllMocks(); history.mockResolvedValue({ items: [record('model-a')], total: 1, page: 1, page_size: 50 }) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('upstream history filter lifecycle', () => {
  it('keeps a clicked initial record, then clears it and old rows when the model changes', async () => {
    const view = render(record('model-a'))
    await flushPromises()
    expect(view.text()).toContain('upstreamCenter.history.selected')
    let rejectHistory: (error: unknown) => void = () => undefined
    history.mockReturnValueOnce(new Promise((_, reject) => { rejectHistory = reject }))
    await choose(view, '#history-model', 'model-b')
    expect(history).toHaveBeenLastCalledWith(9, expect.objectContaining({ model: 'model-b', page: 1 }), expect.any(AbortSignal))
    expect(view.text()).not.toContain('upstreamCenter.history.selected')
    expect(view.text()).not.toContain('model-a record detail')

    rejectHistory({ message: 'New model history failed' })
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('New model history failed')
    expect(view.findAll('tbody tr')).toHaveLength(1)
    expect(view.get('tbody').text()).toContain('upstreamCenter.history.noRecords')
  })
  it('cancels a history request when switching to finance and ignores its later error', async () => {
    let rejectHistory: (error: unknown) => void = () => undefined
    history.mockReturnValueOnce(new Promise((_, reject) => { rejectHistory = reject }))
    const view = render()
    const signal = history.mock.calls[0]![2] as AbortSignal
    const financeTab = view.findAll('button').find(button => button.text() === 'upstreamCenter.finance.title')!
    await financeTab.trigger('click')
    expect(signal.aborted).toBe(true)
    rejectHistory({ message: 'Hidden history failed' })
    await flushPromises()
    expect(view.find('[data-testid="finance-panel"]').exists()).toBe(true)
    expect(view.text()).not.toContain('Hidden history failed')

    const historyTab = view.findAll('button').find(button => button.text() === 'upstreamCenter.history.title')!
    await historyTab.trigger('click')
    await flushPromises()
    expect(view.get('tbody').text()).toContain('model-a record detail')
    expect(view.find('[role="alert"]').exists()).toBe(false)
  })
  it('preserves a newly clicked initial record on reopen without duplicate requests', async () => {
    const view = render(record('model-a'))
    await flushPromises()
    await view.setProps({ show: false })
    history.mockResolvedValue({ items: [record('model-b')], total: 1, page: 1, page_size: 50 })
    await view.setProps({ show: true, model: 'model-b', record: record('model-b') })
    await flushPromises()
    expect(history).toHaveBeenCalledTimes(2)
    expect(view.text()).toContain('upstreamCenter.history.selected')
    expect(view.text()).toContain('model-b record detail')
    expect(view.get('#history-model').text()).toBe('model-b')
  })
  it('keeps the chosen model and time range when an overview refresh replaces the same target', async () => {
    const view = render()
    await flushPromises()
    history.mockResolvedValue({ items: [record('model-b')], total: 1, page: 1, page_size: 50 })
    await choose(view, '#history-model', 'model-b')
    await flushPromises()
    await choose(view, '#history-window', 'upstreamCenter.ranges.7d')
    expect(view.emitted('window-change')?.at(-1)).toEqual(['7d'])
    await flushPromises()
    await view.get('tbody button').trigger('click')
    const requestCount = history.mock.calls.length

    await view.setProps({ target: { ...target(), name: 'Refreshed group' } })
    await flushPromises()
    expect(view.get('#history-model').text()).toBe('model-b')
    expect(view.get('#history-window').text()).toBe('upstreamCenter.ranges.7d')
    expect(view.text()).toContain('upstreamCenter.history.selected')
    expect(history).toHaveBeenCalledTimes(requestCount)
  })
})
