import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { UpstreamBillingSnapshot, UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
import UpstreamTargetCard from './UpstreamTargetCard.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
const stubs = { Icon: true, UpstreamHistoryBar: true, UpstreamStatusBadge: true }

function target(): UpstreamTarget {
  return {
    id: 1, name: 'Test upstream', endpoint: 'https://example.com/v1',
    models: ['model-a', 'model-b'], enabled: true, interval_seconds: 30,
    timeout_seconds: 45, degraded_threshold_ms: 6000, balance: null,
    statistics: [
      { model: 'model-a', status: 'operational', availability_7d: 98.25, latest_latency_ms: 1234, last_checked_at: '2026-09-24T00:00:00Z', timeline: [] },
      { model: 'model-b', status: 'degraded', availability_7d: 70.5, latest_latency_ms: 7000, last_checked_at: '2026-09-24T00:00:01Z', timeline: [] },
    ],
  } as unknown as UpstreamTarget
}

function billing(overrides: Partial<UpstreamBillingSnapshot> = {}): UpstreamBillingSnapshot {
  return {
    group_id: 1, group_name: 'Test group', group_rate_multiplier: 2,
    user_rate_multiplier: null, resolved_rate_multiplier: 2,
    effective_rate_multiplier: 2, billing_scope: 'token', source: 'sub2api_billing',
    status: 'ok', stale: false, synced_at: '2026-09-24T00:00:00Z',
    last_attempt_at: '2026-09-24T00:00:00Z', observed_at: '2026-09-24T00:00:00Z', error: '',
    ...overrides,
  }
}

function withBilling(value: UpstreamBillingSnapshot): UpstreamTarget {
  return { ...target(), balance: { billing: value } } as UpstreamTarget
}

describe('standalone upstream monitor card', () => {
  it('opens only the website origin without exposing endpoint credentials elsewhere in the card', () => {
    const item = { ...target(), endpoint: 'https://user:secret@example.com:8443/private/key-secret/v1?api_key=secret#secret' }
    const wrapper = mount(UpstreamTargetCard, { props: { target: item }, global: { stubs } })
    const link = wrapper.get('[data-testid="upstream-website"]')
    expect(link.attributes('href')).toBe('https://example.com:8443')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(link.attributes('referrerpolicy')).toBe('no-referrer')
    expect(link.attributes('aria-label')).toBe('upstreamCenter.visitWebsite')
    expect(link.text()).toBe('upstreamCenter.visitWebsite')
    expect(wrapper.get('[data-testid="upstream-website-label"]').text()).toBe('example.com:8443')
    expect(wrapper.get('[data-testid="upstream-website-label"]').attributes('title')).toBe('https://example.com:8443')
    expect(wrapper.html()).not.toContain('secret')
    expect(wrapper.html()).not.toContain('/private/')
    wrapper.unmount()
  })

  it.each(['javascript:alert(1)', 'data:text/html,secret', '/private/key-secret', 'not-a-url'])('hides invalid website links and endpoint text: %s', endpoint => {
    const wrapper = mount(UpstreamTargetCard, { props: { target: { ...target(), endpoint } }, global: { stubs } })
    expect(wrapper.find('[data-testid="upstream-website"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="upstream-website-label"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="upstream-website-label"]').attributes('title')).toBe('upstreamCenter.websiteUnavailable')
    expect(wrapper.html()).not.toContain(endpoint)
    wrapper.unmount()
  })

  it('keeps unavailable rates unknown, accepts zero and marks a stale saved rate', async () => {
    const wrapper = mount(UpstreamTargetCard, { props: { target: target() }, global: { stubs } })
    expect(wrapper.get('[data-testid="upstream-rate"]').text()).toBe('upstreamCenter.billing.rate —')
    expect(wrapper.get('[data-testid="upstream-rate"]').attributes('title')).toBe('upstreamCenter.billing.unavailable')

    await wrapper.setProps({ target: withBilling(billing({ effective_rate_multiplier: 0 })) })
    expect(wrapper.get('[data-testid="upstream-rate"]').text()).toContain('0×')
    expect(wrapper.get('[data-testid="upstream-rate"]').classes()).not.toContain('text-amber-700')

    await wrapper.setProps({ target: withBilling(billing({ stale: true, status: 'error' })) })
    expect(wrapper.get('[data-testid="upstream-rate"]').text()).toContain('2×')
    expect(wrapper.get('[data-testid="upstream-rate"]').classes()).toContain('text-amber-700')
    expect(wrapper.get('[data-testid="upstream-rate"]').attributes('title')).toContain('upstreamCenter.billing.stale')
    wrapper.unmount()
  })

  it('keeps the selected model, latest metrics and existing actions available alongside the new badge', async () => {
    const item = withBilling(billing())
    const wrapper = mount(UpstreamTargetCard, { props: { target: item }, global: { stubs } })
    await wrapper.get('select').setValue('model-b')
    expect(wrapper.get('[data-testid="upstream-availability"]').text()).toBe('70.50%')
    expect(wrapper.get('[data-testid="upstream-latency"]').text()).toBe('7,000 ms')
    expect(wrapper.getComponent({ name: 'UpstreamHistoryBar' }).props('lastCheckedAt')).toBe('2026-09-24T00:00:01Z')
    await wrapper.findAll('button').find(button => button.text() === 'upstreamCenter.details')!.trigger('click')
    expect(wrapper.emitted('details')?.[0]).toEqual([item, 'model-b'])
    await wrapper.findAll('button').find(button => button.text() === 'upstreamCenter.run')!.trigger('click')
    expect(wrapper.emitted('run')?.[0]).toEqual([item])
    await wrapper.get('[aria-label="upstreamCenter.pause"]').trigger('click')
    await wrapper.get('[aria-label="upstreamCenter.edit"]').trigger('click')
    await wrapper.get('[aria-label="upstreamCenter.remove"]').trigger('click')
    expect(wrapper.emitted('toggle')?.[0]).toEqual([item])
    expect(wrapper.emitted('edit')?.[0]).toEqual([item])
    expect(wrapper.emitted('delete')?.[0]).toEqual([item])
    wrapper.unmount()
  })

  it.each([{ running: true, busy: false }, { running: false, busy: true }])('prevents another run while the target is unavailable: %o', async state => {
    const wrapper = mount(UpstreamTargetCard, { props: { target: target(), ...state }, global: { stubs } })
    const run = wrapper.findAll('button').find(button => button.text() === (state.running ? 'upstreamCenter.running' : 'upstreamCenter.run'))!
    expect((run.element as HTMLButtonElement).disabled).toBe(true)
    await run.trigger('click')
    expect(wrapper.emitted('run')).toBeUndefined()
    wrapper.unmount()
  })

  it('forwards the selected history record with its model and preserves history timestamps and legend', async () => {
    const item = target()
    const record: UpstreamHistoryRecord = { id: 44, target_id: item.id, model: 'model-b', status: 'failed', latency_ms: 123, ping_latency_ms: null, http_status: 503, message: 'upstream unavailable', checked_at: '2026-09-24T00:00:01Z', cost: null, cost_source: 'unknown' }
    item.statistics[1]!.timeline = [record]
    const wrapper = mount(UpstreamTargetCard, { props: { target: item }, global: { stubs } })
    await wrapper.get('select').setValue('model-b')
    const history = wrapper.getComponent({ name: 'UpstreamHistoryBar' })
    expect(history.props('records')).toEqual([record])
    expect(history.props('lastCheckedAt')).toBe(record.checked_at)
    expect(history.props('legend')).toBe(true)
    history.vm.$emit('select', record)
    expect(wrapper.emitted('details')?.[0]).toEqual([item, 'model-b', record])
    wrapper.unmount()
  })

  it('shows the next scheduled check and changes to paused without retaining a stale schedule', async () => {
    const item = { ...target(), next_check_at: '2026-09-24T00:01:00Z' }
    const wrapper = mount(UpstreamTargetCard, { props: { target: item }, global: { stubs } })
    expect(wrapper.get('[data-testid="upstream-next-check"]').attributes('datetime')).toBe(item.next_check_at)
    expect(wrapper.get('[data-testid="upstream-next-check"]').text()).toBe(new Date(item.next_check_at).toLocaleString())
    await wrapper.setProps({ target: { ...item, enabled: false } })
    expect(wrapper.get('[data-testid="upstream-next-check"]').text()).toBe('upstreamCenter.status.paused')
    expect(wrapper.get('[data-testid="upstream-next-check"]').attributes('datetime')).toBeUndefined()
    expect(wrapper.find('[aria-label="upstreamCenter.resume"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
