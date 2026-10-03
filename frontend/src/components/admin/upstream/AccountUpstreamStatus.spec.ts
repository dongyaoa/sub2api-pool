import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { UpstreamBalanceSnapshot, UpstreamHistoryRecord, UpstreamModelStatistics, UpstreamTarget } from '@/api/admin/upstreamCenter'
import AccountUpstreamStatus from './AccountUpstreamStatus.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${JSON.stringify(values)}` : key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
const stubs = { Icon: true, UpstreamHistoryBar: true, teleport: true }

function target(overrides: Partial<UpstreamTarget> = {}): UpstreamTarget {
  const checkedAt = new Date().toISOString()
  const statistics: UpstreamModelStatistics[] = [
    { model: 'model-a', status: 'operational', availability: 100, availability_7d: 99.5, latest_latency_ms: 256, avg_latency_ms: 256, p95_latency_ms: 300, sample_count: 5, success_count: 5, last_checked_at: checkedAt, timeline: [] },
    { model: 'model-b', status: 'error', availability: 75, availability_7d: 82.75, latest_latency_ms: 3200, avg_latency_ms: 3000, p95_latency_ms: 4000, sample_count: 4, success_count: 3, last_checked_at: checkedAt, timeline: [] },
  ]
  return {
    id: 10, name: 'Premium group', supplier_id: 8, provider: 'openai', api_mode: 'responses',
    endpoint: 'https://secret.example/v1', api_key_masked: 'sk-secret-masked', models: ['model-a', 'model-b'],
    enabled: true, interval_seconds: 30, timeout_seconds: 45, degraded_threshold_ms: 2000,
    account_ids: [5], wallet_ref: 'private-wallet', notes: '', created_at: checkedAt, updated_at: checkedAt,
    last_checked_at: checkedAt, next_check_at: checkedAt, balance: null, statistics,
    finance: {} as UpstreamTarget['finance'], ...overrides,
  }
}

async function selectSecondModel(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('.select-trigger').trigger('click')
  await wrapper.findAll('[role="option"]')[1]!.trigger('click')
}

function balance(overrides: Partial<UpstreamBalanceSnapshot> = {}): UpstreamBalanceSnapshot {
  return { target_id: 10, wallet_ref: 'private-wallet', kind: 'wallet', balance: 16.25, quota_remaining: 4, today_used: 0, total_used: 0, currency: 'USD', status: 'ok', synced_at: '2026-10-03T02:00:00Z', error: '', ...overrides }
}

describe('API key account shared upstream status', () => {
  it('shows the shared upstream wallet and updates low balances with the refreshed target', async () => {
    const wrapper = mount(AccountUpstreamStatus, { props: { target: target({ balance: balance() }) }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-wallet-label"]').text()).toBe('upstreamCenter.wallet.title')
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toContain('16.25')
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).not.toContain('text-rose-600')
    expect(wrapper.get('[data-testid="account-status-wallet"]').attributes('title')).toContain('upstreamCenter.wallet.syncedAt')
    await wrapper.setProps({ target: target({ balance: balance({ balance: 0, status: 'error', last_attempt_at: '2026-10-03T03:00:00Z', error: 'upstream temporarily unavailable' }) }) })
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toContain('0.00')
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).toContain('text-rose-600')
    const hint = wrapper.get('[data-testid="account-status-wallet"]').attributes('title')
    expect(hint).toContain('upstreamCenter.wallet.error')
    expect(hint).toContain('upstreamCenter.wallet.attemptAt')
    expect(hint).toContain('upstream temporarily unavailable')
    await wrapper.setProps({ target: target({ balance: balance({ balance: 5 }) }) })
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).not.toContain('text-rose-600')
    wrapper.unmount()
  })

  it('distinguishes key quotas and subscriptions from wallets and handles unlimited or raw quota units', async () => {
    const wrapper = mount(AccountUpstreamStatus, { props: { target: target({ balance: balance({ kind: 'key_quota' }) }) }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-wallet-label"]').text()).toBe('upstreamCenter.wallet.quota')
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toContain('4.00')
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).toContain('text-rose-600')
    await wrapper.setProps({ target: target({ balance: balance({ kind: 'key_quota', unlimited_quota: true }) }) })
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toBe('upstreamCenter.newapi.unlimited')
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).not.toContain('text-rose-600')
    await wrapper.setProps({ target: target({ balance: balance({ kind: 'key_quota', currency: 'QUOTA' }) }) })
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toBe('4 QUOTA')
    expect(wrapper.get('[data-testid="account-status-balance"]').classes()).not.toContain('text-rose-600')
    await wrapper.setProps({ target: target({ balance: balance({ kind: 'subscription', quota_remaining: 20 }) }) })
    expect(wrapper.get('[data-testid="account-status-wallet-label"]').text()).toBe('upstreamCenter.wallet.subscription')
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toContain('20.00')
    await wrapper.setProps({ target: target({ balance: balance({ unlimited_quota: true }) }) })
    expect(wrapper.get('[data-testid="account-status-wallet-label"]').text()).toBe('upstreamCenter.wallet.title')
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toContain('16.25')
    wrapper.unmount()
  })

  it('keeps missing wallet balances distinct from zero and explains unsupported or pending syncs', async () => {
    const wrapper = mount(AccountUpstreamStatus, { props: { target: target() }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-balance"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="account-status-wallet"]').attributes('title')).toBe('upstreamCenter.wallet.unknown')
    for (const status of ['unsupported', 'pending'] as const) {
      await wrapper.setProps({ target: target({ balance: balance({ balance: null, status }) }) })
      expect(wrapper.get('[data-testid="account-status-balance"]').text()).toBe('—')
      expect(wrapper.get('[data-testid="account-status-balance"]').classes()).not.toContain('text-rose-600')
      expect(wrapper.get('[data-testid="account-status-wallet"]').attributes('title')).toContain(`upstreamCenter.wallet.${status}`)
    }
    wrapper.unmount()
  })

  it('uses the shared group finance values without deriving profit from remote usage or filtering by the health model', async () => {
    const item = target({ balance: { today_used: 9.3, currency: 'USD' } as UpstreamTarget['balance'], finance: { revenue: 12.5, business_cost: 5, monitor_cost: 1, profit: 6.5, currency: 'USD' } as UpstreamTarget['finance'] })
    const wrapper = mount(AccountUpstreamStatus, { props: { target: item }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-upstream-spend"]').text()).toBe('9.30')
    expect(wrapper.get('[data-testid="account-status-user-spend"]').text()).toBe('12.50')
    expect(wrapper.get('[data-testid="account-status-profit"]').text()).toBe('6.50')
    await selectSecondModel(wrapper)
    expect(wrapper.get('[data-testid="account-status-user-spend"]').text()).toBe('12.50')
    expect(wrapper.get('[data-testid="account-status-profit"]').text()).toBe('6.50')
    await wrapper.setProps({ target: { ...item, balance: { ...item.balance!, today_used: 0 }, finance: { ...item.finance, revenue: 0, profit: -5 } } })
    expect(wrapper.get('[data-testid="account-status-upstream-spend"]').text()).toBe('0.00')
    expect(wrapper.get('[data-testid="account-status-user-spend"]').text()).toBe('0.00')
    expect(wrapper.get('[data-testid="account-status-profit"]').text()).toBe('-5.00')
    expect(wrapper.get('[data-testid="account-status-profit"]').classes()).toContain('text-rose-600')
    wrapper.unmount()
  })

  it('keeps unavailable amounts distinct from zero and explains incomplete profit', async () => {
    const wrapper = mount(AccountUpstreamStatus, { props: { target: target() }, global: { stubs } })
    for (const metric of ['upstream-spend', 'user-spend', 'profit']) expect(wrapper.get(`[data-testid="account-status-${metric}"]`).text()).toBe('—')
    await wrapper.setProps({ target: target({ balance: { today_used: 0 } as UpstreamTarget['balance'], finance: { revenue: 0, profit: null } as UpstreamTarget['finance'] }) })
    expect(wrapper.get('[data-testid="account-status-upstream-spend"]').text()).toBe('0.00')
    expect(wrapper.get('[data-testid="account-status-user-spend"]').text()).toBe('0.00')
    expect(wrapper.get('[data-testid="account-status-profit"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="account-status-profit"]').attributes('title')).toBe('upstreamCenter.finance.pending')
    expect(wrapper.get('[data-testid="account-status-profit"]').classes()).toContain('text-amber-600')
    wrapper.unmount()
  })

  it('uses each selected model’s availability, latency and original history', async () => {
    const item = target()
    const record: UpstreamHistoryRecord = { id: 77, target_id: 10, model: 'model-b', status: 'error', latency_ms: 3200, ping_latency_ms: null, http_status: 502, message: 'Gateway timed out', checked_at: item.last_checked_at!, cost: null, cost_source: 'unknown' }
    item.statistics[1]!.timeline = [record]
    const wrapper = mount(AccountUpstreamStatus, { props: { target: item, supplierName: 'Example upstream' }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('99.50%')
    expect(wrapper.get('[data-testid="account-status-latency"]').text()).toBe('256 ms')
    await selectSecondModel(wrapper)
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('82.75%')
    expect(wrapper.get('[data-testid="account-status-latency"]').text()).toBe('3,200 ms')
    expect(wrapper.get('[data-testid="account-status-latency"]').classes()).toContain('text-rose-600')
    expect(wrapper.getComponent({ name: 'UpstreamStatusBadge' }).props('status')).toBe('error')
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('records')).toEqual([record])
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('lastCheckedAt')).toBe(record.checked_at)
    expect(wrapper.text()).toContain('Example upstream')
    expect(wrapper.text()).toContain('Premium group')
    expect(wrapper.html()).not.toContain('sk-secret-masked')
    expect(wrapper.html()).not.toContain('private-wallet')
    expect(wrapper.html()).not.toContain('secret.example')
    wrapper.unmount()
  })

  it('keeps model selection across real-time target refreshes and resets removed options', async () => {
    const item = target()
    const wrapper = mount(AccountUpstreamStatus, { props: { target: item }, global: { stubs } })
    await selectSecondModel(wrapper)
    const refreshed = { ...item, models: [...item.models], statistics: item.statistics.map(stat => stat.model === 'model-b' ? { ...stat, availability_7d: 91.5, latest_latency_ms: 678 } : stat) }
    await wrapper.setProps({ target: refreshed })
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('91.50%')
    expect(wrapper.get('[data-testid="account-status-latency"]').text()).toBe('678 ms')
    await wrapper.setProps({ target: { ...refreshed, models: ['model-a'] } })
    expect(wrapper.find('.select-trigger').exists()).toBe(false)
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('99.50%')
    await wrapper.setProps({ target: { ...refreshed, models: [] } })
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('—')
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('records')).toEqual([])
    wrapper.unmount()
  })

  it('resets model selection when switching to another bound upstream target', async () => {
    const item = target()
    const wrapper = mount(AccountUpstreamStatus, { props: { target: item }, global: { stubs } })
    await selectSecondModel(wrapper)
    await wrapper.setProps({ target: { ...item, id: 11 } })
    expect(wrapper.get('[data-testid="account-status-availability"]').text()).toBe('99.50%')
    wrapper.unmount()
  })

  it('shows the actual paused schedule and only delegates configuration to its parent', async () => {
    const wrapper = mount(AccountUpstreamStatus, { props: { target: target({ enabled: false, interval_seconds: 60 }) }, global: { stubs } })
    expect(wrapper.get('[data-testid="account-status-schedule"]').text()).toContain('upstreamCenter.status.paused')
    expect(wrapper.get('[data-testid="account-status-schedule"]').text()).toContain('"seconds":60')
    expect(wrapper.getComponent({ name: 'UpstreamStatusBadge' }).props('status')).toBe('paused')
    await wrapper.get('[data-testid="account-status-configure"]').trigger('click')
    expect(wrapper.emitted('configure')).toEqual([[]])
    await wrapper.setProps({ busy: true })
    await wrapper.get('[data-testid="account-status-configure"]').trigger('click')
    expect(wrapper.emitted('configure')).toHaveLength(1)
    wrapper.unmount()
  })
})
