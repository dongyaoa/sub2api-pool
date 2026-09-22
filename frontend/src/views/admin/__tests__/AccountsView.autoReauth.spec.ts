import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountsView from '../AccountsView.vue'
import type { OpenAIAutoReauthAccount } from '@/api/admin/openaiAutoReauth'

const api = vi.hoisted(() => ({ list: vi.fn(), getById: vi.fn(), reauth: vi.fn() }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: api.list,
      getById: api.getById,
      getBatchTodayStats: async () => ({ stats: {} }),
      getBatchRecentRequests: async () => ({ requests: {} }),
      getUpstreamBillingProbeSettings: async () => ({ enabled: false })
    },
    proxies: { getAll: async () => [] },
    groups: { getAll: async () => [] }
  }
}))
vi.mock('@/api/admin/openaiAutoReauth', () => ({ openaiAutoReauthAPI: { list: api.reauth } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: 'test-token', isSimpleMode: false }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `<div><slot v-if="!data.length" name="empty" /><div v-for="row in data" :key="row.id" :data-testid="'row-' + row.id"><slot name="cell-name" :row="row" :value="row.name" /><slot name="cell-status" :row="row" /><span data-testid="schedule-state">{{ row.schedulable }}</span></div></div>`
})
const PanelStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  emits: ['close', 'changed'],
  template: '<div v-if="show" data-testid="reauth-panel"><button data-testid="close-panel" @click="$emit(\'close\')" /></div>'
})
const row = { id: 42, name: 'fixture account', platform: 'openai', type: 'oauth', status: 'active', schedulable: true, concurrency: 1, priority: 1, proxy_id: 8, group_ids: [], extra: {}, credentials: {} }
const status = (overrides: Partial<OpenAIAutoReauthAccount> = {}): OpenAIAutoReauthAccount => ({ account_id: 42, email: 'fixture@example.invalid', enabled: true, status: 'idle', attempts: 0, ...overrides })
const overview = (accounts: OpenAIAutoReauthAccount[]) => ({ worker_configured: true, encryption_key_configured: true, accounts })
let wrapper: VueWrapper | undefined

function mountView() {
  wrapper = mount(AccountsView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
    DataTable: DataTableStub,
    OpenAIAutoReauthPanel: PanelStub,
    AccountTableActions: { template: '<div><slot name="after" /></div>' },
    AccountTableFilters: true, AccountBulkActionsBar: true, Pagination: true, ConfirmDialog: true,
    AccountActionMenu: true, ImportDataModal: true, ReAuthAccountModal: true, AccountTestModal: true,
    AccountStatsModal: true, ScheduledTestsPanel: true, SyncFromCrsModal: true, TempUnschedStatusModal: true,
    ErrorPassthroughRulesModal: true, TLSFingerprintProfilesModal: true, CreateAccountModal: true,
    EditAccountModal: true, BulkEditAccountModal: true, AccountStatusIndicator: true, HelpTooltip: true,
    Icon: true, TotpStepUpDialog: true, Teleport: true
  } } })
  return wrapper
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  localStorage.clear()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  api.list.mockResolvedValue({ items: [row], total: 1, page: 1, page_size: 20, pages: 1 })
  api.getById.mockResolvedValue(row)
  api.reauth.mockResolvedValue(overview([]))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks() })

describe('account-list automatic 2FA authorization', () => {
  it('opens credentials for the selected existing account and excludes shadows and other providers', async () => {
    api.list.mockResolvedValue({ items: [row, { ...row, id: 43, parent_account_id: 42 }, { ...row, id: 44, platform: 'anthropic' }], total: 3, page: 1, page_size: 20, pages: 1 })
    const page = mountView()
    await flushPromises()
    expect(page.get('[data-testid="row-42"]').text()).toContain('admin.accounts.autoReauth.notBound')
    expect(page.find('[data-testid="account-2fa-43"]').exists()).toBe(false)
    expect(page.find('[data-testid="account-2fa-44"]').exists()).toBe(false)
    await page.get('[data-testid="account-2fa-42"]').trigger('click')
    expect(page.getComponent(PanelStub).props('account')).toMatchObject({ id: 42, proxy_id: 8 })
    expect(page.getComponent(PanelStub).props('show')).toBe(true)
    expect(api.getById).not.toHaveBeenCalled()
  })

  it('provides a create-and-authorize action when there are no accounts', async () => {
    api.list.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    const page = mountView()
    await flushPromises()
    await page.get('[data-testid="empty-import-openai-account"]').trigger('click')
    expect(page.getComponent(PanelStub).props()).toMatchObject({ show: true, account: null })
  })

  it('updates authorization progress and reloads only the changed account once after recovery', async () => {
    api.list.mockResolvedValue({ items: [{ ...row, schedulable: false }], total: 1, page: 1, page_size: 20, pages: 1 })
    api.reauth.mockResolvedValueOnce(overview([status({ status: 'running', stage: 'logging_in', attempts: 1 })]))
      .mockResolvedValue(overview([status({ status: 'success', attempts: 1, last_success_at: '2026-09-22T01:00:00Z' })]))
    const page = mountView()
    await flushPromises()
    expect(page.get('[data-testid="row-42"]').text()).toContain('admin.accounts.autoReauth.statuses.logging_in')
    expect(page.get('[data-testid="schedule-state"]').text()).toBe('false')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(page.get('[data-testid="row-42"]').text()).toContain('admin.accounts.autoReauth.statuses.success')
    expect(page.get('[data-testid="schedule-state"]').text()).toBe('true')
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.getById).toHaveBeenCalledTimes(1)
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it('pauses list polling while the credentials panel is open and while the page is hidden', async () => {
    const page = mountView()
    await flushPromises()
    expect(api.reauth).toHaveBeenCalledTimes(1)
    await page.get('[data-testid="account-2fa-42"]').trigger('click')
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.reauth).toHaveBeenCalledTimes(1)
    await page.get('[data-testid="close-panel"]').trigger('click')
    await flushPromises()
    expect(api.reauth).toHaveBeenCalledTimes(2)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.reauth).toHaveBeenCalledTimes(2)
    page.unmount()
    wrapper = undefined
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.reauth).toHaveBeenCalledTimes(2)
  })

  it('does not describe a failed status request as an unbound account', async () => {
    api.reauth.mockRejectedValue(new Error('fixture failure'))
    const page = mountView()
    await flushPromises()
    expect(page.get('[data-testid="row-42"]').text()).toContain('admin.accounts.autoReauth.rowStatusUnavailable')
    expect(page.get('[data-testid="row-42"]').text()).not.toContain('admin.accounts.autoReauth.notBound')
  })
})
