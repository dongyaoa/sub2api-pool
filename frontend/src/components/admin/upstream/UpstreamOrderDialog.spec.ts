import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UpstreamOrderDialog from './UpstreamOrderDialog.vue'

const mocks = vi.hoisted(() => ({ overview: vi.fn(), plans: vi.fn(), reorderUpstream: vi.fn(), reorderIntelligence: vi.fn(), success: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key}:${Object.values(values).join(',')}` : key }) }))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { overview: mocks.overview, reorder: mocks.reorderUpstream } }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { plans: mocks.plans, reorder: mocks.reorderIntelligence } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success }) }))
vi.mock('vue-draggable-plus', () => ({ VueDraggable: defineComponent({ name: 'VueDraggable', props: ['modelValue', 'disabled', 'handle'], emits: ['update:modelValue'], template: '<div><slot /></div>' }) }))

const dialog = defineComponent({ props: ['show', 'title', 'closeOnEscape', 'showCloseButton'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const target = (id: number, name: string, enabled = true) => ({ id, name, endpoint: `https://target-${id}.example`, enabled })
const suppliers = [
  { id: 9, name: 'Third upstream', website: 'https://third.example', targets: [target(31, 'First key'), target(12, 'Paused key', false)] },
  { id: 2, name: 'First upstream', website: 'https://first.example', targets: [target(88, 'Other upstream key')] },
  { id: 7, name: 'Second upstream', website: 'https://second.example', targets: [] },
]
const monitorItems = [target(5, 'First monitor'), target(3, 'Paused monitor', false), target(8, 'Hidden by parent search')]
const planItems = [
  { id: 42, name: 'External plan', source_type: 'external', endpoint: 'https://external.example', enabled: true },
  { id: 6, name: 'OAuth Six', source_type: 'openai_oauth', source_name: 'Account six', enabled: true },
  { id: 21, name: 'Upstream plan', source_type: 'upstream', source_name: 'Upstream key', enabled: false },
  { id: 19, name: 'OAuth Nineteen', source_type: 'openai_oauth', source_name: 'Account nineteen', enabled: false },
  { id: 2, name: 'Local plan', source_type: 'local_group', source_name: 'Local group', enabled: true },
]

type Props = InstanceType<typeof UpstreamOrderDialog>['$props']
let wrapper: VueWrapper | undefined
function render(props: Partial<Props> = {}) {
  wrapper = mount(UpstreamOrderDialog, { props: { show: true, scope: 'suppliers', ...props }, global: { stubs: { BaseDialog: dialog, Icon: true } } })
  return wrapper
}
function ids(view: VueWrapper) { return view.findAll('[data-order-item]').map(item => Number(item.attributes('data-order-item'))) }
function pending<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.overview.mockResolvedValue({ suppliers, monitors: monitorItems })
  mocks.plans.mockResolvedValue({ items: planItems })
  mocks.reorderUpstream.mockResolvedValue(undefined)
  mocks.reorderIntelligence.mockResolvedValue(undefined)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('upstream manual order dialog', () => {
  it('loads the full server-ordered upstream list with explanatory text and accessible boundary buttons', async () => {
    const view = render()
    await flushPromises()
    expect(mocks.overview).toHaveBeenCalledWith('24h', expect.any(AbortSignal))
    expect(mocks.plans).not.toHaveBeenCalled()
    expect(ids(view)).toEqual([9, 2, 7])
    expect(view.text()).toContain('upstreamCenter.order.description')
    expect(view.text()).toContain('upstreamCenter.order.newItemsHint')
    expect(view.text()).toContain('upstreamCenter.order.allItems:3')
    expect(view.get('[data-order-item="9"] [data-move-order="up"]').attributes('disabled')).toBeDefined()
    expect(view.get('[data-order-item="7"] [data-move-order="down"]').attributes('disabled')).toBeDefined()
    expect(view.get('[data-order-item="9"] [data-move-order="down"]').attributes('aria-label')).toContain('Third upstream')
    expect(view.text()).toContain('https://third.example')
  })

  it.each([
    ['suppliers', undefined, [9, 2, 7], [2, 9, 7]],
    ['monitors', undefined, [5, 3, 8], [3, 5, 8]],
    ['groups', 9, [31, 12], [12, 31]],
  ] as const)('saves every %s ID and only includes a supplier ID for groups', async (scope, supplierId, before, after) => {
    const view = render({ scope, supplierId, supplierName: scope === 'groups' ? 'Third upstream' : undefined })
    await flushPromises()
    expect(ids(view)).toEqual(before)
    await view.get(`[data-order-item="${before[0]}"] [data-move-order="down"]`).trigger('click')
    expect(ids(view)).toEqual(after)
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(mocks.reorderUpstream).toHaveBeenCalledWith(scope === 'groups' ? { scope, supplier_id: supplierId, ids: after } : { scope, ids: after })
    expect(mocks.reorderIntelligence).not.toHaveBeenCalled()
    expect(view.emitted('saved')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
    expect(mocks.success).toHaveBeenCalledWith('upstreamCenter.order.saved')
    expect(suppliers[0].targets.map(item => item.id)).toEqual([31, 12])
  })

  it.each([
    ['intelligence', [42, 21, 2], [21, 42, 2]],
    ['oauth', [6, 19], [19, 6]],
  ] as const)('loads all plans but saves only the complete %s scope', async (scope, before, after) => {
    const view = render({ scope })
    await flushPromises()
    expect(mocks.plans).toHaveBeenCalledWith(expect.any(AbortSignal))
    expect(mocks.overview).not.toHaveBeenCalled()
    expect(ids(view)).toEqual(before)
    expect(view.text()).toContain('upstreamCenter.status.paused')
    await view.get(`[data-order-item="${before[1]}"] [data-move-order="up"]`).trigger('click')
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(mocks.reorderIntelligence).toHaveBeenCalledWith({ scope, ids: after })
    expect(mocks.reorderUpstream).not.toHaveBeenCalled()
  })

  it('uses the draft emitted by dragging when saving', async () => {
    const view = render()
    await flushPromises()
    const draggable = view.getComponent({ name: 'VueDraggable' })
    expect(draggable.props('handle')).toBe('.upstream-order-handle')
    const order = [...draggable.props('modelValue')].reverse()
    draggable.vm.$emit('update:modelValue', order)
    await flushPromises()
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(mocks.reorderUpstream).toHaveBeenCalledWith({ scope: 'suppliers', ids: [7, 2, 9] })
  })

  it('discards changes on cancel and reloads server order on reopen', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-order-item="9"] [data-move-order="down"]').trigger('click')
    await view.get('[data-cancel-order]').trigger('click')
    expect(mocks.reorderUpstream).not.toHaveBeenCalled()
    expect(view.emitted('close')).toHaveLength(1)
    await view.setProps({ show: false })
    await view.setProps({ show: true })
    await flushPromises()
    expect(ids(view)).toEqual([9, 2, 7])
  })

  it('retains a failed draft, reports the save error, and retries the same complete order', async () => {
    mocks.reorderUpstream.mockRejectedValueOnce({ message: 'Temporary storage error' }).mockResolvedValueOnce(undefined)
    const view = render()
    await flushPromises()
    await view.get('[data-order-item="9"] [data-move-order="down"]').trigger('click')
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('Temporary storage error')
    expect(ids(view)).toEqual([2, 9, 7])
    expect(view.emitted('close')).toBeUndefined()
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(mocks.reorderUpstream.mock.calls).toEqual([[{ scope: 'suppliers', ids: [2, 9, 7] }], [{ scope: 'suppliers', ids: [2, 9, 7] }]])
    expect(view.emitted('saved')).toHaveLength(1)
  })

  it.each([{ status: 409 }, { response: { status: 409 } }])('requires a fresh complete list after a concurrent membership change (%j)', async error => {
    mocks.reorderUpstream.mockRejectedValueOnce(error)
    const view = render({ scope: 'monitors' })
    await flushPromises()
    await view.get('[data-order-item="5"] [data-move-order="down"]').trigger('click')
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('upstreamCenter.order.stale')
    expect(ids(view)).toEqual([3, 5, 8])
    expect(view.get('[data-save-order]').attributes('disabled')).toBeDefined()
    mocks.overview.mockResolvedValueOnce({ suppliers, monitors: [...monitorItems, target(10, 'Newly added')] })
    await view.get('[data-reload-order]').trigger('click')
    await flushPromises()
    expect(ids(view)).toEqual([5, 3, 8, 10])
    expect(view.find('[role="alert"]').exists()).toBe(false)
    await view.get('[data-save-order]').trigger('click')
    await flushPromises()
    expect(mocks.reorderUpstream).toHaveBeenLastCalledWith({ scope: 'monitors', ids: [5, 3, 8, 10] })
  })

  it('locks reordering, repeat saves, cancel and Escape while saving', async () => {
    const request = pending<void>()
    mocks.reorderUpstream.mockReturnValueOnce(request.promise)
    const view = render()
    await flushPromises()
    await view.get('[data-save-order]').trigger('click')
    await view.get('[data-save-order]').trigger('click')
    await view.get('[data-cancel-order]').trigger('click')
    view.getComponent(dialog).vm.$emit('close')
    expect(mocks.reorderUpstream).toHaveBeenCalledTimes(1)
    expect(view.emitted('close')).toBeUndefined()
    expect(view.getComponent(dialog).props('closeOnEscape')).toBe(false)
    expect(view.getComponent(dialog).props('showCloseButton')).toBe(false)
    expect(view.getComponent({ name: 'VueDraggable' }).props('disabled')).toBe(true)
    expect(view.findAll('[data-move-order]').every(button => button.attributes('disabled') !== undefined)).toBe(true)
    request.resolve()
    await flushPromises()
    expect(view.emitted('saved')).toHaveLength(1)
  })

  it('cancels a loading request on close and ignores its late result after reopening', async () => {
    const old = pending<unknown>()
    const current = pending<unknown>()
    mocks.overview.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const view = render()
    const oldSignal = mocks.overview.mock.calls[0][1] as AbortSignal
    await view.get('[data-cancel-order]').trigger('click')
    expect(oldSignal.aborted).toBe(true)
    await view.setProps({ show: false })
    await view.setProps({ show: true })
    old.resolve({ suppliers: [{ id: 99, name: 'Stale item', website: '' }], monitors: [] })
    await flushPromises()
    expect(view.find('[role="status"]').exists()).toBe(true)
    expect(view.text()).not.toContain('Stale item')
    current.resolve({ suppliers, monitors: monitorItems })
    await flushPromises()
    expect(ids(view)).toEqual([9, 2, 7])
  })

  it('aborts and isolates a previous scope when switching to OAuth', async () => {
    const old = pending<unknown>()
    mocks.overview.mockReturnValueOnce(old.promise)
    const view = render()
    const oldSignal = mocks.overview.mock.calls[0][1] as AbortSignal
    await view.setProps({ scope: 'oauth' })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(ids(view)).toEqual([6, 19])
    old.reject({ message: 'Old list failed' })
    await flushPromises()
    expect(view.text()).not.toContain('Old list failed')
    expect(ids(view)).toEqual([6, 19])
  })

  it('does not close or overwrite a new scope when an old save finishes', async () => {
    const old = pending<void>()
    mocks.reorderUpstream.mockReturnValueOnce(old.promise)
    const view = render()
    await flushPromises()
    await view.get('[data-save-order]').trigger('click')
    await view.setProps({ scope: 'oauth' })
    await flushPromises()
    old.resolve()
    await flushPromises()
    expect(ids(view)).toEqual([6, 19])
    expect(view.emitted('saved')).toBeUndefined()
    expect(view.emitted('close')).toBeUndefined()
    expect(view.get('[data-save-order]').attributes('disabled')).toBeUndefined()
  })

  it('aborts a pending load on unmount', async () => {
    const request = pending<unknown>()
    mocks.plans.mockReturnValueOnce(request.promise)
    const view = render({ scope: 'intelligence' })
    const signal = mocks.plans.mock.calls[0][0] as AbortSignal
    view.unmount()
    wrapper = undefined
    expect(signal.aborted).toBe(true)
    request.reject(new Error('cancelled'))
    await flushPromises()
    expect(mocks.reorderIntelligence).not.toHaveBeenCalled()
  })

  it('shows a reload action when initial loading fails and never saves an incomplete list', async () => {
    mocks.overview.mockRejectedValueOnce({ message: 'Cannot load list' })
    const view = render()
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('Cannot load list')
    expect(view.get('[data-save-order]').attributes('disabled')).toBeDefined()
    await view.get('[data-reload-order]').trigger('click')
    await flushPromises()
    expect(ids(view)).toEqual([9, 2, 7])
  })

  it.each([
    { suppliers: [{ ...suppliers[0] }, { ...suppliers[0] }], monitors: [] },
    { suppliers: [{ ...suppliers[0], id: -1 }], monitors: [] },
    { monitors: [] },
  ])('does not allow malformed or duplicate list IDs to overwrite saved order', async response => {
    mocks.overview.mockResolvedValueOnce(response)
    const view = render()
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('upstreamCenter.order.loadError')
    expect(view.get('[data-save-order]').attributes('disabled')).toBeDefined()
    expect(mocks.reorderUpstream).not.toHaveBeenCalled()
  })

  it('rejects a missing upstream instead of treating it as an empty group order', async () => {
    const view = render({ scope: 'groups', supplierId: 100, supplierName: 'Removed upstream' })
    await flushPromises()
    expect(view.getComponent(dialog).props('title')).toBe('upstreamCenter.order.groupTitle:Removed upstream')
    expect(view.get('[role="alert"]').text()).toContain('upstreamCenter.order.supplierMissing')
    expect(view.get('[data-save-order]').attributes('disabled')).toBeDefined()
  })

  it('does not load while closed and renders an empty order without enabling save', async () => {
    const view = render({ show: false, scope: 'groups', supplierId: 7 })
    expect(mocks.overview).not.toHaveBeenCalled()
    await view.setProps({ show: true })
    await flushPromises()
    expect(view.text()).toContain('upstreamCenter.order.empty')
    expect(view.get('[data-save-order]').attributes('disabled')).toBeDefined()
  })
})
