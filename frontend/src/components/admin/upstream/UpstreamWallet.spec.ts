import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamWallet from './UpstreamWallet.vue'
import type { UpstreamBalanceSnapshot, UpstreamBillingSnapshot } from '@/api/admin/upstreamCenter'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { time?: string }) => `${key}${params?.time ? ` ${params.time}` : ''}` }) }))
describe('upstream wallet sync failure', () => {
  it('distinguishes unlimited New API key quota from a finite wallet balance', async () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'default', kind: 'key_quota', balance: null, quota_remaining: null, unlimited_quota: true, today_used: null, total_used: 10, currency: 'USD', status: 'ok', synced_at: null, error: '', billing: { source: 'newapi_token', error: 'newapi_account_auth_required' } as UpstreamBillingSnapshot }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet] }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="wallet-balance"]').text()).toBe('upstreamCenter.newapi.unlimited')
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).not.toContain('text-rose-600')
    expect(wrapper.text()).toContain('New API')
    expect(wrapper.text()).toContain('upstreamCenter.newapi.errors.authorizationRequired')
    expect(wrapper.get('[data-testid="newapi-wallet-notice"]').classes()).toContain('text-amber-700')
    expect(wrapper.text().match(/upstreamCenter\.newapi\.errors\.authorizationRequired/g)).toHaveLength(1)
    await wrapper.setProps({ showKeyUsage: true })
    expect(wrapper.text().match(/upstreamCenter\.newapi\.errors\.authorizationRequired/g)).toHaveLength(1)
    expect(wrapper.text()).toContain('upstreamCenter.newapi.usageHint')
    await wrapper.setProps({ wallets: [{ ...wallet, kind: 'wallet', balance: 3, billing: { source: 'newapi_account', error: '' } as UpstreamBillingSnapshot }] })
    expect(wrapper.get('[data-testid="wallet-balance"]').text()).toContain('3.00')
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).toContain('text-rose-600')
    expect(wrapper.get('[data-testid="wallet-key-quota"]').text()).toContain('upstreamCenter.newapi.unlimited')
    wrapper.unmount()
  })
  it('displays raw New API quota without currency or low-money warnings and leaves daily usage unknown', () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'default', kind: 'key_quota', balance: null, quota_remaining: 3, today_used: null, total_used: 1250, currency: 'QUOTA', status: 'ok', synced_at: null, error: 'newapi_quota_unit_unknown', billing: { source: 'newapi_token', error: 'newapi_account_auth_required' } as UpstreamBillingSnapshot }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet], targetId: 7, showKeyUsage: true }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="wallet-balance"]').text()).toBe('3 QUOTA')
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).not.toContain('text-rose-600')
    expect(wrapper.get('[data-testid="key-upstream-today"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="key-upstream-total"]').text()).toBe('1,250 QUOTA')
    expect(wrapper.text()).toContain('upstreamCenter.newapi.errors.quotaUnitUnknown')
    expect(wrapper.text()).toContain('upstreamCenter.newapi.rawUsageHint')
    wrapper.unmount()
  })
  it.each([
    { balance: 4.99, low: true },
    { balance: 0, low: true },
    { balance: -1, low: true },
    { balance: 5, low: false },
    { balance: 20, low: false },
    { balance: null, low: false },
    { balance: Number.NaN, low: false },
    { balance: Number.POSITIVE_INFINITY, low: false },
  ])('highlights a balance of $balance only when it is known and below five', ({ balance, low }) => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'default', kind: 'wallet', balance, quota_remaining: null, today_used: null, total_used: null, currency: 'USD', status: 'ok', synced_at: null, error: '' }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet] }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="wallet-balance"]').classes().includes('text-rose-600')).toBe(low)
    wrapper.unmount()
  })

  it('uses the displayed quota for quota wallets, and keeps the warning on a stale known balance', async () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'default', kind: 'key_quota', balance: 100, quota_remaining: 3, today_used: null, total_used: null, currency: 'USD', status: 'ok', synced_at: null, error: '' }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet] }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).toContain('text-rose-600')
    await wrapper.setProps({ wallets: [{ ...wallet, balance: 1, quota_remaining: 10 }] })
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).not.toContain('text-rose-600')
    await wrapper.setProps({ wallets: [{ ...wallet, kind: 'wallet', balance: 3, status: 'error' }] })
    expect(wrapper.get('[data-testid="wallet-balance"]').classes()).toContain('text-rose-600')
    wrapper.unmount()
  })

  it('retains the known balance and distinguishes its last successful sync from the failed attempt', () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'default', kind: 'wallet', balance: 42, quota_remaining: null, today_used: null, total_used: null, currency: 'USD', status: 'error', synced_at: '2026-09-22T01:00:00Z', last_attempt_at: '2026-09-23T01:00:00Z', error: 'Upstream connection timed out.' }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet] }, global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('42.00')
    expect(wrapper.text()).toContain(`upstreamCenter.wallet.syncedAt ${new Date(wallet.synced_at!).toLocaleString()}`)
    expect(wrapper.text()).toContain(`upstreamCenter.wallet.attemptAt ${new Date(wallet.last_attempt_at!).toLocaleString()}`)
    expect(wrapper.text()).toContain('Upstream connection timed out.')
    expect(wrapper.text()).not.toContain('0.00')
    wrapper.unmount()
  })
  it('shows per-key reported daily and lifetime spending only in the key detail view', async () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'shared', kind: 'wallet', balance: 42, quota_remaining: null, today_used: 3.25, total_used: 109.5, currency: 'USD', status: 'ok', synced_at: '2026-09-23T01:00:00Z', error: '' }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [wallet], targetId: 7 }, global: { stubs: { Icon: true } } })
    expect(wrapper.find('[data-testid="key-upstream-usage"]').exists()).toBe(false)
    await wrapper.setProps({ showKeyUsage: true })
    expect(wrapper.get('[data-testid="key-upstream-today"]').text()).toContain('3.25')
    expect(wrapper.get('[data-testid="key-upstream-total"]').text()).toContain('109.50')
    expect(wrapper.text()).toContain('upstreamCenter.wallet.usageHint')
    await wrapper.setProps({ wallets: [{ ...wallet, today_used: null, total_used: null }] })
    expect(wrapper.get('[data-testid="key-upstream-today"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="key-upstream-total"]').text()).toBe('—')
    await wrapper.setProps({ wallets: [] })
    expect(wrapper.get('[data-testid="key-upstream-today"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="key-upstream-total"]').text()).toBe('—')
    wrapper.unmount()
  })
  it('selects the matching key without summing spending from a shared wallet', () => {
    const wallet: UpstreamBalanceSnapshot = { target_id: 7, wallet_ref: 'shared', kind: 'wallet', balance: 42, quota_remaining: null, today_used: 0, total_used: 20, currency: 'USD', status: 'ok', synced_at: null, error: '' }
    const wrapper = mount(UpstreamWallet, { props: { wallets: [{ ...wallet, target_id: 8, today_used: 99, total_used: 999 }, wallet], targetId: 7, showKeyUsage: true }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="key-upstream-today"]').text()).toContain('0.00')
    expect(wrapper.get('[data-testid="key-upstream-total"]').text()).toContain('20.00')
    expect(wrapper.get('[data-testid="key-upstream-usage"]').text()).not.toContain('99.00')
    wrapper.unmount()
  })
})
