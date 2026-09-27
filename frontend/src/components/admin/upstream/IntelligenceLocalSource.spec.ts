import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AdminGroup, ApiKey } from '@/types'
import IntelligenceLocalSource from './IntelligenceLocalSource.vue'
const mocks = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/keys', () => ({ list: mocks.list }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const groups = [{ id: 4, name: 'Primary', rate_multiplier: 0.3 }, { id: 5, name: 'Backup', rate_multiplier: 0.5 }] as AdminGroup[]
const key = (id: number, fields: Partial<ApiKey> = {}) => ({ id, name: `Key ${id}`, key: 'sk-private-never-render-this-secret-1234', status: 'active', group_id: 4, quota: 0, quota_used: 0, expires_at: null, ...fields }) as ApiKey
const page = (items: ApiKey[], number = 1, pages = 1) => ({ items, total: pages * 100, page: number, page_size: 100, pages })
let view: VueWrapper<InstanceType<typeof IntelligenceLocalSource>> | undefined
function render(props: Partial<InstanceType<typeof IntelligenceLocalSource>['$props']> = {}) {
  view = mount(IntelligenceLocalSource, { props: { groups, groupsLoading: false, groupId: null, localKeyId: null, ...props }, global: { stubs: { Icon: true, Select: { name: 'Select', props: ['modelValue', 'options', 'disabled', 'loading', 'searchable'], emits: ['update:modelValue'], template: '<div><span v-for="option in options">{{ option.label }}</span></div>' } } } })
  return view
}
beforeEach(() => { vi.resetAllMocks(); mocks.list.mockResolvedValue(page([key(1)])) })
afterEach(() => view?.unmount())
describe('local administrator key source', () => {
  it('reads only the signed-in user endpoint, follows all pages, deduplicates and masks raw keys', async () => {
    mocks.list.mockResolvedValueOnce(page([key(1)], 1, 2)).mockResolvedValueOnce(page([key(1), key(2, { group_id: 5 }), key(3, { status: 'inactive' }), key(4, { expires_at: '2000-01-01' }), key(5, { quota: 1, quota_used: 1 }), key(6, { group_id: 99 }), key(7, { group_id: null })], 2, 2))
    const wrapper = render(); await flushPromises()
    expect(mocks.list).toHaveBeenNthCalledWith(1, 1, 100, { status: 'active' }, { signal: expect.any(AbortSignal) })
    expect(mocks.list).toHaveBeenNthCalledWith(2, 2, 100, { status: 'active' }, { signal: expect.any(AbortSignal) })
    const picker = wrapper.getComponent('#intelligence-local-key')
    expect(picker.props('options').map((item: { value: number | null }) => item.value)).toEqual([null, 1, 2])
    expect(wrapper.text()).toContain('sk-••••1234')
    expect(wrapper.html()).not.toContain('private-never-render')
    expect(picker.props('searchable')).toBe(false)
  })
  it('selects an existing key’s group, locks it and allows returning to automatic mode without rebinding a key', async () => {
    const wrapper = render(); await flushPromises()
    wrapper.getComponent('#intelligence-local-key').vm.$emit('update:modelValue', 1)
    expect(wrapper.emitted('select')).toEqual([[{ groupId: 4, keyId: 1, name: 'Primary' }]])
    await wrapper.setProps({ groupId: 4, localKeyId: 1 })
    expect(wrapper.getComponent('#intelligence-group').props('disabled')).toBe(true)
    expect(wrapper.vm.validate()).toBe(true)
    wrapper.getComponent('#intelligence-group').vm.$emit('update:modelValue', 5)
    expect(wrapper.emitted('select')).toHaveLength(1)
    wrapper.getComponent('#intelligence-local-key').vm.$emit('update:modelValue', null)
    expect(wrapper.emitted('select')?.[1]).toEqual([{ groupId: 4, keyId: null }])
    await wrapper.setProps({ localKeyId: null })
    wrapper.getComponent('#intelligence-group').vm.$emit('update:modelValue', 5)
    expect(wrapper.emitted('select')?.[2]).toEqual([{ groupId: 5, keyId: null, name: 'Backup' }])
  })
  it('refuses missing keys or mismatched bindings instead of falling back silently', async () => {
    const wrapper = render({ localKeyId: 1, groupId: 5 }); await flushPromises()
    expect(wrapper.vm.validate()).toBe(false)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[role="alert"]').text()).toContain('keyUnavailable')
    await wrapper.setProps({ localKeyId: 99, savedKeyName: 'Previous key', savedKeyMask: 'sk-••••5678' })
    expect(wrapper.text()).toContain('Previous key')
    expect(wrapper.vm.validate()).toBe(false)
    await wrapper.setProps({ localKeyId: null })
    expect(wrapper.vm.validate()).toBe(true)
  })
  it('excludes known automatic keys belonging to monitoring plans from the existing-key choices', async () => {
    mocks.list.mockResolvedValue(page([key(1), key(2)]))
    const wrapper = render({ managedKeyIds: [1] }); await flushPromises()
    expect(wrapper.getComponent('#intelligence-local-key').props('options').map((option: { value: number | null }) => option.value)).toEqual([null, 2])
  })
  it('cancels an old read and does not accept its late result after refreshing', async () => {
    let resolve!: (value: ReturnType<typeof page>) => void
    mocks.list.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const wrapper = render(); const signal = mocks.list.mock.calls[0]![3].signal as AbortSignal
    wrapper.unmount(); view = undefined
    expect(signal.aborted).toBe(true)
    resolve(page([key(9)])); await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(1)
  })
  it('keeps automatic mode usable on read failure and retries the key inventory explicitly', async () => {
    mocks.list.mockRejectedValueOnce(new Error('offline'))
    const wrapper = render(); await flushPromises()
    expect(wrapper.vm.validate()).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Key 1')
  })
})
