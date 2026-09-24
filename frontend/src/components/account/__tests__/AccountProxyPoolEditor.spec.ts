import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import AccountProxyPoolEditor from '../AccountProxyPoolEditor.vue'
import type { AccountProxyPoolEntry, Proxy } from '@/types'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

const ProxySelectorStub = defineComponent({
  name: 'ProxySelector',
  props: ['modelValue', 'proxies', 'excludedIds', 'allowNone', 'placeholder', 'disabled'],
  emits: ['update:modelValue'],
  template: '<div class="proxy-selector-stub" />'
})

function proxy(id: number, overrides: Partial<Proxy> = {}): Proxy {
  return {
    id, name: `Proxy ${id}`, host: `proxy-${id}.example.com`, port: 8080,
    protocol: 'http', username: null, status: 'active', expires_at: null,
    fallback_mode: 'none', expiry_warn_days: 7, created_at: '', updated_at: '',
    ...overrides
  }
}

function mountEditor(proxies: Proxy[], modelValue: AccountProxyPoolEntry[] = []) {
  return mount(AccountProxyPoolEditor, {
    props: { proxies, modelValue },
    global: { stubs: { Icon: true, ProxySelector: ProxySelectorStub } }
  })
}

describe('AccountProxyPoolEditor', () => {
  it('requires an explicit selection and adds that proxy with 20 concurrent requests', async () => {
    const wrapper = mountEditor([proxy(1), proxy(2)])
    const picker = wrapper.findAllComponents(ProxySelectorStub)[0]
    const add = wrapper.get('[data-testid="pool-add"]')

    expect(add.attributes('disabled')).toBeDefined()
    expect((wrapper.get('[data-testid="pool-new-concurrency"]').element as HTMLInputElement).value).toBe('20')
    expect(picker.props('allowNone')).toBe(false)
    picker.vm.$emit('update:modelValue', 2)
    await nextTick()
    await add.trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 2, concurrency: 20 }]])
    expect(wrapper.get('[data-testid="pool-total"]').text()).toBe('20')
    expect(picker.props('modelValue')).toBeNull()
    expect(picker.props('excludedIds')).toEqual([2])
    expect(add.attributes('disabled')).toBeDefined()
  })

  it('bulk-adds only eligible unselected proxies and uses the configured concurrency', async () => {
    const wrapper = mountEditor([
      proxy(1), proxy(2), proxy(3, { status: 'inactive' }),
      proxy(4, { status: 'expired' }),
      proxy(5, { expires_at: '2000-01-01T00:00:00Z' }), proxy(6)
    ], [{ proxy_id: 1, concurrency: 7 }])

    await wrapper.get('[data-testid="pool-new-concurrency"]').setValue(12)
    await wrapper.get('[data-testid="pool-add-all"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      { proxy_id: 1, concurrency: 7 }, { proxy_id: 2, concurrency: 12 }, { proxy_id: 6, concurrency: 12 }
    ]])
    expect(wrapper.get('[data-testid="pool-total"]').text()).toBe('31')
    expect(wrapper.find('[data-testid="pool-add-all"]').exists()).toBe(false)
    expect(wrapper.findAllComponents(ProxySelectorStub)[0].props('disabled')).toBe(true)
  })

  it('rejects duplicate, unavailable, missing and stale additions even if a picker emits them', async () => {
    const wrapper = mountEditor([proxy(1), proxy(2), proxy(3, { status: 'inactive' })], [{ proxy_id: 1, concurrency: 5 }])
    const picker = wrapper.findAllComponents(ProxySelectorStub)[0]
    for (const id of [1, 3, 999, null]) {
      picker.vm.$emit('update:modelValue', id)
      await nextTick()
      expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeDefined()
    }

    picker.vm.$emit('update:modelValue', 2)
    await nextTick()
    expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ proxies: [proxy(1), proxy(2, { status: 'inactive' })] })
    expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="pool-add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps hydrated unavailable and missing entries visible until explicitly removed', async () => {
    const unavailable = proxy(8, { name: 'Expired Tokyo proxy', status: 'expired' })
    const modelValue = [{ proxy_id: 8, concurrency: 9, proxy: unavailable }, { proxy_id: 99, concurrency: 11 }]
    const wrapper = mountEditor([proxy(1)], modelValue)

    expect(wrapper.findAll('[data-testid="pool-entry"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="pool-unavailable"]')).toHaveLength(2)
    const picker = wrapper.findAllComponents(ProxySelectorStub)[1]
    expect(picker.props('proxies')).toContainEqual(unavailable)
    expect(wrapper.get('[data-testid="pool-total"]').text()).toBe('20')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.findAll('[data-testid="pool-remove"]')[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 99, concurrency: 11 }]])
    expect(modelValue).toHaveLength(2)
  })

  it('validates replacements and preserves concurrency without stale proxy metadata', async () => {
    const wrapper = mountEditor([proxy(1), proxy(2), proxy(3), proxy(4, { status: 'inactive' })], [
      { proxy_id: 1, concurrency: 8, current_concurrency: 3, proxy: proxy(1) },
      { proxy_id: 2, concurrency: 4 }
    ])
    const picker = wrapper.findAllComponents(ProxySelectorStub)[1]
    expect(picker.props('excludedIds')).toEqual([2])
    for (const id of [2, 4, 999, null]) {
      picker.vm.$emit('update:modelValue', id)
      await nextTick()
    }
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    picker.vm.$emit('update:modelValue', 3)
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      { proxy_id: 3, concurrency: 8, proxy: proxy(3) }, { proxy_id: 2, concurrency: 4 }
    ]])
  })

  it('normalizes concurrency after editing and leaves the incoming model untouched', async () => {
    const modelValue = [{ proxy_id: 1, concurrency: 20 }]
    const wrapper = mountEditor([proxy(1), proxy(2)], modelValue)
    const input = wrapper.get('[data-testid="pool-entry-concurrency"]')
    await input.setValue(6.9)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 1, concurrency: 6 }]])
    await input.setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 1, concurrency: 1 }]])
    expect(modelValue[0].concurrency).toBe(20)

    await wrapper.get('[data-testid="pool-new-concurrency"]').setValue(-2)
    await wrapper.get('[data-testid="pool-add-all"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      { proxy_id: 1, concurrency: 1 }, { proxy_id: 2, concurrency: 20 }
    ]])
  })

  it('refreshes bound entries when the parent changes account', async () => {
    const wrapper = mountEditor([proxy(1), proxy(2)], [{ proxy_id: 1, concurrency: 20 }])
    await wrapper.setProps({ modelValue: [{ proxy_id: 2, concurrency: 5 }] })
    expect(wrapper.findAllComponents(ProxySelectorStub)[1].props('modelValue')).toBe(2)
    expect(wrapper.get('[data-testid="pool-total"]').text()).toBe('5')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('rechecks expiry at add time even when the option was available when selected', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    const wrapper = mountEditor([proxy(1, { expires_at: '2026-01-01T00:00:01Z' }), proxy(2)])
    wrapper.findAllComponents(ProxySelectorStub)[0].vm.$emit('update:modelValue', 1)
    await nextTick()
    expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeUndefined()
    vi.setSystemTime(new Date('2026-01-01T00:00:02Z'))
    await wrapper.get('[data-testid="pool-add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="pool-add-all"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 2, concurrency: 20 }]])
  })

  it('disables an oversized bulk addition without partially changing the pool', async () => {
    const proxies = Array.from({ length: 258 }, (_, index) => proxy(index + 1))
    const wrapper = mountEditor(proxies)
    expect(wrapper.get('[data-testid="pool-add-all"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="pool-limits"]').text()).toContain('100000')
    await wrapper.get('[data-testid="pool-add-all"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    wrapper.findAllComponents(ProxySelectorStub)[0].vm.$emit('update:modelValue', 1)
    await nextTick()
    expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="pool-add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[{ proxy_id: 1, concurrency: 20 }]])
  })

  it('enforces the total concurrency budget on additions and row edits', async () => {
    const wrapper = mountEditor([proxy(1), proxy(2), proxy(3)], [{ proxy_id: 1, concurrency: 99985 }])
    wrapper.findAllComponents(ProxySelectorStub)[0].vm.$emit('update:modelValue', 2)
    await nextTick()
    expect(wrapper.get('[data-testid="pool-add"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="pool-add-all"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="pool-new-concurrency"]').setValue(15)
    await wrapper.get('[data-testid="pool-add"]').trigger('click')
    expect(wrapper.get('[data-testid="pool-total"]').text()).toBe('100000')

    await wrapper.findAll('[data-testid="pool-entry-concurrency"]')[1].setValue(30)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      { proxy_id: 1, concurrency: 99985 }, { proxy_id: 2, concurrency: 15 }
    ]])
    expect(wrapper.get('[data-testid="pool-limits"]').exists()).toBe(true)
  })
})
