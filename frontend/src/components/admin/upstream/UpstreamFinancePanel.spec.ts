import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UpstreamFinancePanel from './UpstreamFinancePanel.vue'
import type { UpstreamSupplier } from '@/api/admin/upstreamCenter'
const finance = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { finance } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
beforeEach(() => finance.mockReset())
describe('upstream finance unknown-cost state', () => {
  it('shows incomplete costs instead of a fabricated zero profit and uses the backend default date window', async () => {
    finance.mockResolvedValue({ summary: { revenue: 12, business_cost: 4, monitor_cost: null, profit: null, request_count: 2, cost_source: 'estimated', currency: 'USD', from: '2026-09-23T00:00:00Z', to: '2026-09-24T00:00:00Z', remote_used: null, reconciliation_delta: null, unpriced_monitor_count: 3 }, items: [], total: 0, page: 1, page_size: 50 })
    const wrapper = mount(UpstreamFinancePanel, { props: { supplier: { id: 2, targets: [] } as unknown as UpstreamSupplier, target: null }, global: { stubs: { Pagination: true, Icon: true } } })
    await flushPromises()
    expect(finance).toHaveBeenCalledWith(expect.objectContaining({ supplier_id: 2, from: undefined, to: undefined }), expect.any(AbortSignal))
    expect(wrapper.text()).toContain('upstreamCenter.finance.pending')
    expect(wrapper.text()).toContain('upstreamCenter.finance.unpriced')
    expect(wrapper.text()).toContain('upstreamCenter.finance.noRows')
    expect(wrapper.text()).not.toContain('$0.00')
    wrapper.unmount()
  })
  it('preserves a supplier group and date filter when the same supplier refreshes', async () => {
    finance.mockResolvedValue({ summary: { revenue: 12, business_cost: 4, monitor_cost: 0, profit: 8, request_count: 2, cost_source: 'estimated', currency: 'USD', from: '2026-09-23T00:00:00Z', to: '2026-09-24T00:00:00Z', remote_used: null, reconciliation_delta: null, unpriced_monitor_count: 0 }, items: [], total: 0, page: 1, page_size: 50 })
    const supplier = { id: 2, name: 'Supplier', targets: [{ id: 9, name: 'Group 9' }] } as unknown as UpstreamSupplier
    const wrapper = mount(UpstreamFinancePanel, { props: { supplier, target: null }, global: { stubs: { Pagination: true, Icon: true } } })
    await flushPromises()
    await wrapper.get('#finance-target').setValue('9')
    await wrapper.get('#finance-from').setValue('2026-09-20')
    await wrapper.get('#finance-to').setValue('2026-09-21')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(finance).toHaveBeenCalledTimes(2)

    await wrapper.setProps({ supplier: { ...supplier, name: 'Updated supplier' } })
    await flushPromises()
    expect((wrapper.get('#finance-target').element as HTMLSelectElement).value).toBe('9')
    expect((wrapper.get('#finance-from').element as HTMLInputElement).value).toBe('2026-09-20')
    expect((wrapper.get('#finance-to').element as HTMLInputElement).value).toBe('2026-09-21')
    expect(finance).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
