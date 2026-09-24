import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProxySelector from '../ProxySelector.vue'
import type { Proxy } from '@/types'

const testProxy = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { testProxy } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks() })

function proxy(id: number, overrides: Partial<Proxy> = {}): Proxy {
  return { id, name: `Proxy ${id}`, host: 'localhost', port: 8080, protocol: 'http', status: 'active', expires_at: null, ...overrides } as Proxy
}

async function openSelector(props: Partial<InstanceType<typeof ProxySelector>['$props']> = {}) {
  const wrapper = mount(ProxySelector, {
    props: { modelValue: null, proxies: [proxy(1), proxy(2)], ...props },
    global: { stubs: { Icon: true, Teleport: true } }
  })
  await wrapper.get('.select-trigger').trigger('click')
  return wrapper
}

describe('proxy connection tests', () => {
  it('does not restart an individual test when a batch is started', async () => {
    let finish!: (result: object) => void
    testProxy.mockImplementation((id: number) => id === 1
      ? new Promise(resolve => { finish = resolve })
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy.mock.calls.map(([id]) => id)).toEqual([1, 2])
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeDefined()
    expect(wrapper.get('.batch-test-btn').attributes('disabled')).toBeDefined()
    finish({ success: true, country: 'US' })
    await flushPromises()
    expect(wrapper.text()).toContain('US')
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeUndefined()
  })

  it('shows per-proxy outcomes and allows another batch after a failure', async () => {
    testProxy.mockImplementation((id: number) => id === 1
      ? Promise.reject(new Error('offline'))
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('admin.proxies.testFailed')
    expect(wrapper.text()).toContain('GB')
    expect(wrapper.get('.batch-test-btn').attributes('disabled')).toBeUndefined()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(4)
  })

  it('tests only matching usable proxies and exposes address, exit IP and persisted latency', async () => {
    testProxy.mockResolvedValue({ success: true })
    const wrapper = await openSelector({ proxies: [
      proxy(1, { host: 'us.proxy.invalid', ip_address: '192.0.2.11', country: 'US', latency_ms: 65, latency_status: 'success' }),
      proxy(2, { country: 'GB' }),
      proxy(3, { country: 'US', status: 'inactive' })
    ] })
    expect(wrapper.text()).toContain('192.0.2.11')
    expect(wrapper.text()).toContain('65ms')
    await wrapper.get('input').setValue('US')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(2)
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy.mock.calls.map(([id]) => id)).toEqual([1])
  })

  it('limits batch testing to five simultaneous requests', async () => {
    const resolvers: Array<() => void> = []
    let active = 0
    let peak = 0
    testProxy.mockImplementation(() => new Promise(resolve => {
      active++
      peak = Math.max(peak, active)
      resolvers.push(() => { active--; resolve({ success: true }) })
    }))
    const wrapper = await openSelector({ proxies: Array.from({ length: 12 }, (_, index) => proxy(index + 1)) })
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(5)
    while (resolvers.length) {
      resolvers.splice(0).forEach(resolve => resolve())
      await flushPromises()
    }
    expect(testProxy).toHaveBeenCalledTimes(12)
    expect(peak).toBe(5)
    expect(wrapper.get('.batch-test-btn').attributes('disabled')).toBeUndefined()
  })
})

describe('proxy selection', () => {
  it('excludes already-bound proxies and disables unavailable choices', async () => {
    const wrapper = await openSelector({ allowNone: false, excludedIds: [1], proxies: [
      proxy(1), proxy(2), proxy(3, { status: 'inactive' }), proxy(4, { expires_at: '2020-01-01T00:00:00Z' })
    ] })
    const options = wrapper.findAll('[role="option"]')
    expect(options).toHaveLength(3)
    expect(options[0].text()).toContain('Proxy 2')
    expect(options[1].attributes('aria-disabled')).toBe('true')
    expect(options[2].attributes('aria-disabled')).toBe('true')
    await options[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await options[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]])
  })

  it('keeps a missing bound proxy visible instead of showing direct connection', async () => {
    const wrapper = await openSelector({ modelValue: 99 })
    expect(wrapper.get('.select-trigger').text()).toContain('admin.accounts.proxyPool.missingProxy')
    const missing = wrapper.findAll('[role="option"]').find(option => option.text().includes('missingProxy'))!
    expect(missing.attributes('aria-disabled')).toBe('true')
  })

  it('searches by port and supports keyboard selection', async () => {
    const wrapper = await openSelector({ allowNone: false, proxies: [proxy(1), proxy(2, { port: 9050 })] })
    await wrapper.get('input').setValue('9050')
    await wrapper.get('input').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([[2]])
  })

  it('revalidates expiry when selecting an option that was available when opened', async () => {
    const now = Date.now()
    const wrapper = await openSelector({ allowNone: false, proxies: [proxy(1, { expires_at: new Date(now + 1000).toISOString() })] })
    const clock = vi.spyOn(Date, 'now').mockReturnValue(now + 2000)
    try {
      await wrapper.get('[role="option"]').trigger('click')
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    } finally {
      clock.mockRestore()
    }
  })
})
