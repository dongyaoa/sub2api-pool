import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'
import type { AccountUpstreamMonitor, UpstreamSupplier, UpstreamTarget } from '@/api/admin/upstreamCenter'
import AccountMonitorDialog from '../AccountMonitorDialog.vue'

const mocks = vi.hoisted(() => ({
  accountMonitor: vi.fn(), ensureAccountMonitor: vi.fn(), childRefresh: vi.fn(), showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: mocks }))
vi.mock('../../upstream/UpstreamIntelligenceDialog.vue', () => ({ default: {
  name: 'UpstreamIntelligenceDialog', props: { account: Object, target: Object, overview: Object, embedded: Boolean, active: Boolean }, emits: ['close', 'childOpen', 'changed', 'refreshOverview'],
  setup(_: unknown, { expose }: { expose: (value: unknown) => void }) {
    expose({ refresh: mocks.childRefresh })
  },
  template: '<div data-testid="shared-pelican-monitor" />'
} }))
vi.mock('../../upstream/AccountUpstreamStatus.vue', () => ({ default: {
  name: 'AccountUpstreamStatus', props: ['target', 'supplierName', 'busy'], emits: ['configure'],
  template: '<div data-testid="shared-upstream-status">{{ target.name }}</div>'
} }))
vi.mock('../../upstream/UpstreamTargetDialog.vue', () => ({ default: {
  name: 'UpstreamTargetDialog', props: ['show', 'target', 'supplier'], emits: ['close', 'saved'],
  template: '<div data-testid="status-editor" />'
} }))

const baseDialog = {
  name: 'BaseDialog', props: ['show', 'title', 'width', 'closeOnEscape', 'showCloseButton'], emits: ['close'],
  template: '<div data-testid="account-monitor-dialog"><slot /><slot name="footer" /></div>'
}
const account = { id: 42, name: 'Relay account', platform: 'openai', type: 'apikey', extra: {} } as Account
const target = {
  id: 71, name: 'Premium', supplier_id: 7, provider: 'openai', api_mode: 'responses', endpoint: 'https://relay.example/v1',
  models: ['gpt-5.6-sol'], enabled: true, interval_seconds: 30, statistics: [], account_ids: [42],
  finance: { revenue: 0 }, balance: null,
} as UpstreamTarget
const supplier = { id: 7, name: 'Relay', website: 'https://relay.example', targets: [target], finance: target.finance, wallets: [] } as unknown as UpstreamSupplier
const snapshot = (fields: Partial<AccountUpstreamMonitor> = {}): AccountUpstreamMonitor => ({
  account_id: 42, account_name: 'Relay account', provider: 'openai', pelican_supported: true, target, supplier, ...fields
})
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let wrapper: VueWrapper | undefined
function render(value = account) {
  wrapper = mount(AccountMonitorDialog, { props: { account: value }, global: { stubs: { BaseDialog: baseDialog, Icon: true } } })
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  mocks.accountMonitor.mockResolvedValue(snapshot())
  mocks.ensureAccountMonitor.mockResolvedValue(snapshot())
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('shared account monitoring dialog', () => {
  it('reuses the OAuth account monitor without requesting APIKey upstream data', async () => {
    const view = render({ ...account, type: 'oauth' }); await flushPromises()
    const child = view.getComponent({ name: 'UpstreamIntelligenceDialog' })
    expect(child.props('account')).toMatchObject({ id: 42, name: 'Relay account' })
    expect(mocks.accountMonitor).not.toHaveBeenCalled()
    expect(mocks.ensureAccountMonitor).not.toHaveBeenCalled()
    child.vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('shares the original APIKey target and its overview between health and pelican monitoring', async () => {
    const view = render(); await flushPromises()
    expect(mocks.accountMonitor).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(mocks.ensureAccountMonitor).not.toHaveBeenCalled()
    const status = view.getComponent({ name: 'AccountUpstreamStatus' })
    const child = view.getComponent({ name: 'UpstreamIntelligenceDialog' })
    expect(status.props('target')).toEqual(target)
    expect(child.props('target')).toEqual(target)
    expect(child.props('embedded')).toBe(true)
    expect(child.props('overview').suppliers[0].targets).toEqual([target])
    expect(child.props('overview').suppliers[0].id).toBe(supplier.id)
    expect(view.html().indexOf('shared-upstream-status')).toBeLessThan(view.html().indexOf('shared-pelican-monitor'))
  })

  it('only creates an account binding after Connect, blocks duplicate submissions, and reuses the returned target', async () => {
    mocks.accountMonitor.mockResolvedValue(snapshot({ target: null, supplier: null }))
    const response = deferred<AccountUpstreamMonitor>()
    mocks.ensureAccountMonitor.mockReturnValueOnce(response.promise)
    const view = render(); await flushPromises()
    expect(mocks.ensureAccountMonitor).not.toHaveBeenCalled()
    expect(view.find('[data-testid="shared-pelican-monitor"]').exists()).toBe(false)
    const connect = view.get('[data-testid="account-monitor-ensure"]')
    await connect.trigger('click'); await connect.trigger('click')
    expect(mocks.ensureAccountMonitor).toHaveBeenCalledTimes(1)
    expect(mocks.ensureAccountMonitor).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(connect.attributes('disabled')).toBeDefined()
    const parent = view.getComponent({ name: 'BaseDialog' })
    parent.vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toBeUndefined()
    mocks.accountMonitor.mockResolvedValue(snapshot())
    response.resolve(snapshot()); await flushPromises()
    expect(view.getComponent({ name: 'UpstreamIntelligenceDialog' }).props('target').id).toBe(71)
    expect(view.find('[data-testid="account-monitor-ensure"]').exists()).toBe(false)
  })

  it('aborts a pending creation when unmounted and ignores its delayed response', async () => {
    mocks.accountMonitor.mockResolvedValue(snapshot({ target: null, supplier: null }))
    const response = deferred<AccountUpstreamMonitor>()
    mocks.ensureAccountMonitor.mockReturnValueOnce(response.promise)
    const view = render(); await flushPromises()
    await view.get('[data-testid="account-monitor-ensure"]').trigger('click')
    const signal = mocks.ensureAccountMonitor.mock.calls[0]![1] as AbortSignal
    view.unmount(); wrapper = undefined
    expect(signal.aborted).toBe(true)
    response.resolve(snapshot()); await flushPromises()
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(1)
    expect(mocks.childRefresh).not.toHaveBeenCalled()
  })

  it('ignores an old account creation response after switching account IDs', async () => {
    mocks.accountMonitor.mockResolvedValue(snapshot({ target: null, supplier: null }))
    const response = deferred<AccountUpstreamMonitor>()
    mocks.ensureAccountMonitor.mockReturnValueOnce(response.promise)
    const view = render(); await flushPromises()
    await view.get('[data-testid="account-monitor-ensure"]').trigger('click')
    const signal = mocks.ensureAccountMonitor.mock.calls[0]![1] as AbortSignal
    const secondTarget = { ...target, id: 72, name: 'Another group', account_ids: [43] }
    mocks.accountMonitor.mockResolvedValue(snapshot({ account_id: 43, account_name: 'Another account', target: secondTarget }))
    await view.setProps({ account: { ...account, id: 43, name: 'Another account' } })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    response.resolve(snapshot()); await flushPromises()
    expect(view.getComponent({ name: 'AccountUpstreamStatus' }).props('target').id).toBe(72)
    expect(view.getComponent({ name: 'UpstreamIntelligenceDialog' }).props('target').id).toBe(72)
  })

  it('refreshes both the status snapshot and the existing pelican child without remounting it', async () => {
    const view = render(); await flushPromises()
    const child = view.getComponent({ name: 'UpstreamIntelligenceDialog' })
    const previousElement = child.element
    mocks.accountMonitor.mockResolvedValue(snapshot({ target: { ...target, name: 'Updated group' } }))
    await view.get('[data-testid="account-monitor-refresh"]').trigger('click'); await flushPromises()
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(2)
    expect(mocks.childRefresh).toHaveBeenCalledTimes(1)
    expect(view.getComponent({ name: 'AccountUpstreamStatus' }).props('target').name).toBe('Updated group')
    expect(view.getComponent({ name: 'UpstreamIntelligenceDialog' }).element).toBe(previousElement)
  })

  it('polls only the lightweight snapshot and suspends polling while hidden or closed', async () => {
    const visibility = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const view = render(); await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(2)
    expect(mocks.childRefresh).not.toHaveBeenCalled()
    visibility.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(3)
    view.getComponent({ name: 'BaseDialog' }).vm.$emit('close'); await flushPromises()
    await vi.advanceTimersByTimeAsync(15000)
    expect(view.emitted('close')).toHaveLength(1)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(3)
  })

  it('discards the previous target on a conflicting binding instead of showing stale monitoring data', async () => {
    const view = render(); await flushPromises()
    mocks.accountMonitor.mockRejectedValue({ response: { status: 409, data: { code: 'UPSTREAM_ACCOUNT_MONITOR_AMBIGUOUS' } } })
    await view.get('[data-testid="account-monitor-refresh"]').trigger('click'); await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(true)
    expect(view.find('[data-testid="shared-upstream-status"]').exists()).toBe(false)
    expect(view.find('[data-testid="shared-pelican-monitor"]').exists()).toBe(false)
    expect(view.find('[data-testid="account-monitor-ensure"]').exists()).toBe(false)
    expect(mocks.ensureAccountMonitor).not.toHaveBeenCalled()
  })

  it('keeps the parent open and suspends snapshot reads while a nested artwork or status editor is open', async () => {
    const view = render(); await flushPromises()
    const parent = view.getComponent({ name: 'BaseDialog' })
    const child = view.getComponent({ name: 'UpstreamIntelligenceDialog' })
    child.vm.$emit('childOpen', true); await flushPromises()
    expect(parent.props('closeOnEscape')).toBe(false)
    parent.vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(6000)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(1)
    child.vm.$emit('childOpen', false); await flushPromises()
    view.getComponent({ name: 'AccountUpstreamStatus' }).vm.$emit('configure'); await flushPromises()
    const editor = view.getComponent({ name: 'UpstreamTargetDialog' })
    expect(editor.props('target')).toEqual(target)
    expect(editor.props('supplier')).toEqual(supplier)
    expect(parent.props('closeOnEscape')).toBe(false)
    expect(child.props('active')).toBe(false)
    parent.vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(6000)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(1)
    editor.vm.$emit('saved'); await flushPromises()
    expect(view.find('[data-testid="status-editor"]').exists()).toBe(false)
    expect(parent.props('closeOnEscape')).toBe(true)
    expect(mocks.accountMonitor).toHaveBeenCalledTimes(2)
    expect(child.props('active')).toBe(true)
  })

  it('keeps non-OpenAI APIKey health visible without exposing an unsupported pelican creator', async () => {
    mocks.accountMonitor.mockResolvedValue(snapshot({ provider: 'anthropic', pelican_supported: false, target: { ...target, provider: 'anthropic' } }))
    const view = render({ ...account, platform: 'anthropic' }); await flushPromises()
    expect(view.find('[data-testid="shared-upstream-status"]').exists()).toBe(true)
    expect(view.find('[data-testid="shared-pelican-monitor"]').exists()).toBe(false)
    expect(view.find('[data-testid="account-monitor-unsupported"]').exists()).toBe(true)
  })
})
