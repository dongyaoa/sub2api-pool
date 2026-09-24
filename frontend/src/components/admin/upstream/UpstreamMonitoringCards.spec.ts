import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UpstreamTarget } from '@/api/admin/upstreamCenter'
import Select from '@/components/common/Select.vue'
import UpstreamGroupRow from './UpstreamGroupRow.vue'
import UpstreamTargetCard from './UpstreamTargetCard.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
const stubs = { Icon: true, UpstreamHistoryBar: true, UpstreamStatusBadge: true, UpstreamRateBadge: true, teleport: true }
function target(): UpstreamTarget {
  return {
    id: 1, name: 'Test upstream', models: ['model-a', 'model-b'], endpoint: 'https://example.com', enabled: true,
    degraded_threshold_ms: 6000, timeout_seconds: 45, interval_seconds: 300,
    statistics: [
      { model: 'model-a', status: 'operational', availability: 12, availability_7d: 98.25, avg_latency_ms: 8888, latest_latency_ms: 1234, last_checked_at: '2026-09-24T00:00:00Z', timeline: [] },
      { model: 'model-b', status: 'degraded', availability: 20, availability_7d: 70.5, avg_latency_ms: 1111, latest_latency_ms: 7000, last_checked_at: '2026-09-24T00:00:01Z', timeline: [] },
    ],
    finance: { revenue: 40, business_cost: 10, profit: 30, currency: 'USD', request_count: 123, total_tokens: 1250000, unknown_token_requests: 0, account_billed: 10 },
    balance: { today_used: 15, currency: 'USD' },
  } as unknown as UpstreamTarget
}

describe('upstream card headline measurements', () => {
  beforeEach(() => { vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-09-24T00:00:02Z')) })
  afterEach(() => { vi.restoreAllMocks() })

  it.each([['key group', UpstreamGroupRow], ['standalone monitor', UpstreamTargetCard]] as const)('uses fixed seven-day success and the latest latency in %s', async (_, component) => {
    const wrapper = mount(component, { props: { target: target(), busy: false, running: false }, global: { stubs } })
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('98.25%')
    expect(wrapper.get('[data-testid="upstream-latency"]').text()).toBe('1,234 ms')
    expect(wrapper.get('[data-testid="upstream-latency"]').classes()).toContain('text-emerald-600')
    expect(wrapper.text()).not.toContain('12.00%')
    expect(wrapper.text()).not.toContain('8,888 ms')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.getComponent(Select).props('searchable')).toBe(false)
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('[role="option"]').map(option => option.text())).toEqual([
      'model-a · upstreamCenter.status.operational',
      'model-b · upstreamCenter.status.degraded',
    ])
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')
    expect(wrapper.get('.select-trigger').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('70.50%')
    expect(wrapper.get('[data-testid="upstream-latency"]').text()).toBe('7,000 ms')
    expect(wrapper.get('[data-testid="upstream-latency"]').classes()).toContain('text-amber-600')
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('lastCheckedAt')).toBe('2026-09-24T00:00:01Z')
    wrapper.unmount()
  })

  it.each([['key group', UpstreamGroupRow], ['standalone monitor', UpstreamTargetCard]] as const)('keeps %s model selection through refreshes without showing a search field', async (_, component) => {
    const item = { ...target(), models: ['model-a', 'model-b', 'model-c', 'model-d', 'model-e', 'model-f'] }
    const wrapper = mount(component, { props: { target: item, busy: false, running: false }, global: { stubs } })
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.find('[role="listbox"] input').exists()).toBe(false)
    expect(wrapper.findAll('[role="option"]')).toHaveLength(6)
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')

    await wrapper.setProps({ target: { ...item, models: [...item.models], statistics: item.statistics.map(statistics => statistics.model === 'model-b' ? { ...statistics, status: 'failed', availability_7d: 49 } : statistics) } })
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('model-b')
    expect(wrapper.get('.select-trigger').text()).toBe('model-b · upstreamCenter.status.failed')
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('49.00%')

    await wrapper.setProps({ target: { ...item, models: ['model-a'] } })
    expect(wrapper.findComponent(Select).exists()).toBe(false)
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('98.25%')
    const details = wrapper.findAll('button').find(button => button.text() === 'upstreamCenter.details')!
    await details.trigger('click')
    expect(wrapper.emitted('details')?.[0]?.[1]).toBe('model-a')
    wrapper.unmount()
  })

  it('shows attributed daily requests, tokens and account charges without turning unknown tokens into zero', async () => {
    const item = target()
    const wrapper = mount(UpstreamGroupRow, { props: { target: item, busy: false, running: false }, global: { stubs } })
    expect(wrapper.get('[data-testid="upstream-request-count"]').text()).toBe('123')
    expect(wrapper.get('[data-testid="upstream-token-count"]').text()).toBe('1.25M')
    expect(wrapper.get('[data-testid="upstream-account-billed"]').text()).toBe('10.00')
    expect(wrapper.get('[data-testid="upstream-remote-spend"]').text()).toBe('15.00')
    expect(wrapper.get('[data-testid="upstream-user-spend"]').text()).toBe('40.00')
    expect(wrapper.get('[data-testid="upstream-profit"]').text()).toBe('30.00')
    await wrapper.setProps({ target: { ...item, finance: { ...item.finance, total_tokens: 1250000000 } } })
    expect(wrapper.get('[data-testid="upstream-token-count"]').text()).toBe('1.25B')
    await wrapper.setProps({ target: { ...item, finance: { ...item.finance, total_tokens: null, unknown_token_requests: 2 } } })
    expect(wrapper.get('[data-testid="upstream-token-count"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-token-count"]').attributes('title')).toBe('upstreamCenter.finance.tokensIncomplete')
    expect(wrapper.get('[data-testid="upstream-token-count"]').classes()).toContain('!text-amber-600')
    wrapper.unmount()
  })

  it('keeps standalone card actions tied to the selected model and missing statistics neutral', async () => {
    const item = target()
    const wrapper = mount(UpstreamTargetCard, { props: { target: item }, global: { stubs } })
    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')
    const details = wrapper.findAll('button').find(button => button.text() === 'upstreamCenter.details')
    expect(details).toBeDefined()
    await details!.trigger('click')
    expect(wrapper.emitted('details')?.[0]).toEqual([item, 'model-b'])
    await wrapper.get('[aria-label="upstreamCenter.pause"]').trigger('click')
    expect(wrapper.emitted('toggle')?.[0]).toEqual([item])

    await wrapper.setProps({ target: { ...item, statistics: [] } })
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-latency"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-latency"]').classes()).toContain('text-gray-400')
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('records')).toEqual([])
    wrapper.unmount()
  })
})
