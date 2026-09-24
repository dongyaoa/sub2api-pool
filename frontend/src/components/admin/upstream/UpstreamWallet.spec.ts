import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamWallet from './UpstreamWallet.vue'
import type { UpstreamBalanceSnapshot } from '@/api/admin/upstreamCenter'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { time?: string }) => `${key}${params?.time ? ` ${params.time}` : ''}` }) }))
describe('upstream wallet sync failure', () => {
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
