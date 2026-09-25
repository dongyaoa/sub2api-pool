import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamRateBadge from './UpstreamRateBadge.vue'
import type { UpstreamBillingSnapshot } from '@/api/admin/upstreamCenter'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('upstream billing multiplier display', () => {
  it('identifies New API and explains missing authorization without inventing a multiplier', () => {
    const wrapper = mount(UpstreamRateBadge, { props: { billing: { source: 'newapi_token', effective_rate_multiplier: null, error: 'newapi_account_auth_required', status: 'unsupported', stale: false } as UpstreamBillingSnapshot }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="newapi-badge"]').text()).toBe('New API')
    expect(wrapper.text()).toContain('—')
    expect(wrapper.attributes('title')).toBe('upstreamCenter.newapi.errors.authorizationRequired')
    wrapper.unmount()
  })
  it.each([0.6, null])('includes the actual remote group in the tooltip when its rate is %s', effective_rate_multiplier => {
    const wrapper = mount(UpstreamRateBadge, { props: { billing: { source: 'newapi_account', group_name: 'premium', effective_rate_multiplier, error: effective_rate_multiplier == null ? 'newapi_auto_group' : '', status: 'ok', stale: false } as UpstreamBillingSnapshot }, global: { stubs: { Icon: true } } })
    expect(wrapper.attributes('title')).toContain('upstreamCenter.newapi.remoteGroup: premium')
    expect(wrapper.attributes('title')).toContain(effective_rate_multiplier == null ? 'upstreamCenter.newapi.errors.autoGroup' : '0.6×')
    expect(wrapper.text()).toContain(effective_rate_multiplier == null ? 'upstreamCenter.newapi.dynamicRate' : '0.6×')
    wrapper.unmount()
  })
  it('keeps zero as a real multiplier and missing values unknown instead of defaulting to one', async () => {
    const wrapper = mount(UpstreamRateBadge, { global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('—')
    expect(wrapper.attributes('title')).toBe('upstreamCenter.billing.unavailable')
    await wrapper.setProps({ billing: { effective_rate_multiplier: 0, group_rate_multiplier: 0.5, user_rate_multiplier: null, resolved_rate_multiplier: 0, status: 'ok', stale: false, synced_at: null } as UpstreamBillingSnapshot })
    expect(wrapper.text()).toContain('0×')
    expect(wrapper.text()).not.toContain('1×')
    wrapper.unmount()
  })
  it('retains a stale reported rate with a visible stale indicator', () => {
    const wrapper = mount(UpstreamRateBadge, { props: { billing: { effective_rate_multiplier: 0.6, group_rate_multiplier: 0.6, user_rate_multiplier: null, resolved_rate_multiplier: 0.6, status: 'error', stale: true, synced_at: '2026-09-23T00:00:00Z' } as UpstreamBillingSnapshot }, global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('0.6×')
    expect(wrapper.attributes('title')).toContain('upstreamCenter.billing.stale')
    expect(wrapper.find('icon-stub').exists()).toBe(true)
    wrapper.unmount()
  })
})
