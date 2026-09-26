import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UpstreamHistoryRecord } from '@/api/admin/upstreamCenter'
import UpstreamHistoryBar from './UpstreamHistoryBar.vue'

const copy = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, string>) => values?.seconds ? `${key}:${values.seconds}` : key }) }))
const record: UpstreamHistoryRecord = { id: 1, target_id: 2, model: 'test-model', status: 'error', latency_ms: 1234, ping_latency_ms: null, http_status: 429, message: 'Rate limit exceeded · try later', checked_at: '2026-09-24T00:00:00Z', cost: null, cost_source: 'unknown' }
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-24T00:00:05Z')); copy.mockClear() })
afterEach(() => { vi.useRealTimers(); document.body.innerHTML = '' })

describe('upstream check history strip', () => {
  it('pads 60 compact bars on the left and exposes copyable error details using HelpTooltip', async () => {
    const wrapper = mount(UpstreamHistoryBar, { props: { records: [record] }, global: { stubs: { Icon: true } } })
    expect(wrapper.findAll('[data-status]')).toHaveLength(60)
    expect(wrapper.findAll('[data-status="unknown"]')).toHaveLength(59)
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    const bar = wrapper.get('[data-status="error"]')
    expect(bar.classes()).toContain('history-bar--failure')
    await bar.trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([record])
    const trigger = wrapper.findComponent({ name: 'HelpTooltip' })
    await trigger.trigger('mouseenter')
    const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('HTTP 429')
    expect(tooltip.textContent).toContain('test-model')
    expect(tooltip.textContent).toContain('1,234 ms')
    expect(tooltip.textContent).toContain(record.message)
    tooltip.querySelector('button')?.click()
    expect(copy).toHaveBeenCalledWith(expect.stringContaining('HTTP 429'))
    expect(copy.mock.calls[0]?.[0]).toContain(new Date(record.checked_at).toLocaleString())
    expect(copy.mock.calls[0]?.[0]).toContain('test-model')
    expect(copy.mock.calls[0]?.[0]).toContain('1,234 ms')
    expect(copy.mock.calls[0]?.[0]).toContain(record.message)
    await trigger.trigger('mouseleave')
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull()
    wrapper.unmount()
  })

  it('ticks elapsed seconds independently of server polling and clears the timer on unmount', async () => {
    const wrapper = mount(UpstreamHistoryBar, { props: { records: [record] }, global: { stubs: { HelpTooltip: true, Icon: true } } })
    expect(wrapper.get('[data-testid="upstream-last-updated"]').text()).toContain(':5')
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.get('[data-testid="upstream-last-updated"]').text()).toContain(':8')
    await wrapper.setProps({ lastCheckedAt: '2026-09-24T00:00:07Z' })
    expect(wrapper.get('[data-testid="upstream-last-updated"]').text()).toContain(':1')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
