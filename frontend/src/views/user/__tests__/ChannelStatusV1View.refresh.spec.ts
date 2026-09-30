import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserMonitorDetail, UserMonitorView } from '@/api/channelMonitor'
import MonitorHero from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import ChannelStatusV1View from '../ChannelStatusV1View.vue'

const { list, status, showError } = vi.hoisted(() => ({ list: vi.fn(), status: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list, status }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
const mountView = () => shallowMount(ChannelStatusV1View, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    MonitorHero: { props: ['autoRefresh', 'window', 'lastUpdated', 'refreshError', 'refreshing'], emits: ['refresh', 'update:window'], template: `<div>
      <button class="interval" @click="autoRefresh.setInterval(120)">120 seconds</button>
      <button class="refresh" @click="$emit('refresh')">Refresh</button>
      <button class="disable" @click="autoRefresh.setEnabled(false)">Disable</button>
      <button class="window-15d" @click="$emit('update:window', '15d')">15 days</button>
      <button class="window-30d" @click="$emit('update:window', '30d')">30 days</button>
    </div>` },
  } },
})
let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-19T01:00:00.000Z'))
  localStorage.clear()
  list.mockReset().mockResolvedValue({ items: [] })
  status.mockReset()
  showError.mockReset()
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); localStorage.clear() })

describe('channel monitor refresh interval', () => {
  it('keeps automatic refresh disabled after reopening the page', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.disable').trigger('click')
    wrapper.unmount()
    wrapper = mountView(); await flushPromises()
    const calls = list.mock.calls.length
    await vi.advanceTimersByTimeAsync(240000)
    expect(list).toHaveBeenCalledTimes(calls)
    expect(JSON.parse(localStorage.getItem('channel-status-auto-refresh')!).enabled).toBe(false)
    await wrapper.get('.refresh').trigger('click'); await flushPromises()
    expect(list).toHaveBeenCalledTimes(calls + 1)
  })

  it('starts automatic refresh for a first visit', async () => {
    wrapper = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(120000)
    expect(list.mock.calls.length).toBeGreaterThan(1)
  })

  it('refreshes at the selected interval on successive automatic refreshes', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.interval').trigger('click')
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('preserves the selected interval after a manual refresh', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.interval').trigger('click')
    await wrapper.get('.refresh').trigger('click'); await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('honors a persisted interval after the initial load', async () => {
    localStorage.setItem('channel-status-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 120 }))
    wrapper = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(119000)
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(list).toHaveBeenCalledTimes(2)
    await wrapper.get('.disable').trigger('click')
    await vi.advanceTimersByTimeAsync(120000)
    expect(list).toHaveBeenCalledTimes(2)
  })
})

const monitor: UserMonitorView = {
  id: 1, name: 'Main', provider: 'openai', group_name: 'Main',
  primary_model: 'test-model', primary_status: 'operational',
  primary_latency_ms: 120, primary_ping_latency_ms: 20,
  availability_7d: 100, extra_models: [], timeline: [],
  last_checked_at: '2026-09-19T01:00:00.000Z',
}

function detail(availability: number): UserMonitorDetail {
  return {
    id: 1, name: 'Main', provider: 'openai', group_name: 'Main',
    models: [{
      model: 'test-model', latest_status: 'operational', latest_latency_ms: 120,
      availability_7d: 100, availability_15d: availability, availability_30d: availability,
      avg_latency_7d_ms: 120,
    }],
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

describe('channel monitor refresh snapshots', () => {
  it.each(['15d', '30d'] as const)('refreshes %s availability on every automatic cycle', async window => {
    list.mockResolvedValue({ items: [monitor] })
    status.mockResolvedValueOnce(detail(98)).mockResolvedValueOnce(detail(92))
    wrapper = mountView(); await flushPromises()
    expect(status).not.toHaveBeenCalled()

    await wrapper.get(`.window-${window}`).trigger('click'); await flushPromises()
    expect(wrapper.findComponent(MonitorCardGrid).props('detailCache')[1].models[0][`availability_${window}`]).toBe(98)
    const firstUpdate = wrapper.findComponent(MonitorHero).props('lastUpdated')

    await vi.advanceTimersByTimeAsync(60_000)
    expect(status).toHaveBeenCalledTimes(2)
    expect(wrapper.findComponent(MonitorCardGrid).props('detailCache')[1].models[0][`availability_${window}`]).toBe(92)
    expect(wrapper.findComponent(MonitorHero).props('lastUpdated')).not.toBe(firstUpdate)
    expect(wrapper.findComponent(MonitorHero).props('refreshError')).toBe(false)
  })

  it('keeps the complete successful snapshot and its timestamp when a detail refresh fails', async () => {
    list.mockResolvedValue({ items: [monitor] })
    status.mockResolvedValue(detail(98))
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.window-15d').trigger('click'); await flushPromises()
    const lastUpdated = wrapper.findComponent(MonitorHero).props('lastUpdated')

    list.mockResolvedValue({ items: [{ ...monitor, name: 'Incomplete new snapshot' }] })
    status.mockRejectedValueOnce(new Error('Service unavailable'))
    await vi.advanceTimersByTimeAsync(60_000)

    expect(wrapper.findComponent(MonitorCardGrid).props('items')[0].name).toBe('Main')
    expect(wrapper.findComponent(MonitorCardGrid).props('detailCache')[1].models[0].availability_15d).toBe(98)
    expect(wrapper.findComponent(MonitorHero).props('lastUpdated')).toBe(lastUpdated)
    expect(wrapper.findComponent(MonitorHero).props('refreshError')).toBe(true)
    expect(wrapper.findComponent(MonitorHero).props('refreshing')).toBe(false)
    expect(showError).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(60_000)
    expect(wrapper.findComponent(MonitorCardGrid).props('items')[0].name).toBe('Incomplete new snapshot')
    expect(wrapper.findComponent(MonitorHero).props('lastUpdated')).not.toBe(lastUpdated)
    expect(wrapper.findComponent(MonitorHero).props('refreshError')).toBe(false)
  })

  it('prevents a superseded window request from overwriting the newer snapshot', async () => {
    const oldRequest = deferred<UserMonitorDetail>()
    list.mockResolvedValue({ items: [monitor] })
    status.mockReturnValueOnce(oldRequest.promise).mockResolvedValueOnce(detail(95))
    wrapper = mountView(); await flushPromises()
    await wrapper.get('.window-15d').trigger('click'); await flushPromises()
    const supersededSignal = status.mock.calls[0][1].signal as AbortSignal

    list.mockResolvedValue({ items: [{ ...monitor, name: 'Newest snapshot' }] })
    await wrapper.get('.window-30d').trigger('click'); await flushPromises()
    const latestTimestamp = wrapper.findComponent(MonitorHero).props('lastUpdated')
    expect(supersededSignal.aborted).toBe(true)
    expect(wrapper.findComponent(MonitorHero).props('window')).toBe('30d')

    vi.setSystemTime(new Date('2026-09-19T01:00:05.000Z'))
    oldRequest.resolve(detail(50)); await flushPromises()
    expect(wrapper.findComponent(MonitorCardGrid).props('items')[0].name).toBe('Newest snapshot')
    expect(wrapper.findComponent(MonitorCardGrid).props('detailCache')[1].models[0].availability_30d).toBe(95)
    expect(wrapper.findComponent(MonitorHero).props('lastUpdated')).toBe(latestTimestamp)
    expect(wrapper.findComponent(MonitorHero).props('refreshError')).toBe(false)
    expect(showError).not.toHaveBeenCalled()
  })
})
