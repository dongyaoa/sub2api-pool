import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IntelligencePlan } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import Select from '@/components/common/Select.vue'
import IntelligencePlanDialog from './IntelligencePlanDialog.vue'

const mocks = vi.hoisted(() => ({ accounts: vi.fn(), groups: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { count?: number }) => params?.count === undefined ? key : `${key}:${params.count}` }) }))
vi.mock('@/api/admin/accounts', () => ({ list: mocks.accounts }))
vi.mock('@/api/admin/groups', () => ({ getAll: mocks.groups }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { create: mocks.create, update: mocks.update }, PELICAN_MODEL: 'gpt-6-astra', PELICAN_REASONING: 'high', PELICAN_PROMPT: 'Pelican animation' }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const oauthAccount = (id: number, name: string, fields: Record<string, unknown> = {}) => ({ id, name, platform: 'openai', type: 'oauth', status: 'active', schedulable: true, ...fields })
const page = (items: unknown[], number = 1, pages = 1) => ({ items, total: pages === 1 ? items.length : pages * 100, page: number, page_size: 100, pages })
const savedPlan = (fields: Partial<IntelligencePlan> = {}) => ({ id: 3, name: 'OAuth Seven', source_type: 'openai_oauth', account_id: 7, api_mode: 'responses', enabled: true, interval_seconds: 3600, timeout_seconds: 900, supplier_note: '', group_note: '', rate_note: '', notes: '', ...fields }) as IntelligencePlan
let wrapper: VueWrapper | undefined
function render(props: Partial<{ show: boolean; plan: IntelligencePlan | null; overview: UpstreamOverview | null; oauthOnly: boolean }> = {}) {
  wrapper = mount(IntelligencePlanDialog, { attachTo: document.body, props: { show: true, plan: null, overview: null, oauthOnly: true, ...props }, global: { stubs: { BaseDialog: dialog, Icon: true, transition: true } } })
  return wrapper
}
async function selectOption(view: VueWrapper, selector: string, text: string) {
  await view.get(selector).trigger('click')
  await flushPromises()
  const option = [...document.body.querySelectorAll<HTMLElement>('[role="option"]')].find(item => item.textContent?.includes(text))
  expect(option, `Missing option ${text}`).toBeDefined()
  option!.click()
  await flushPromises()
}
async function selectOAuth(view: VueWrapper, name = 'OAuth Seven') { await selectOption(view, '#intelligence-oauth-account', name) }
beforeEach(() => {
  vi.clearAllMocks()
  mocks.accounts.mockResolvedValue(page([oauthAccount(7, 'OAuth Seven')]))
  mocks.groups.mockResolvedValue([])
  mocks.create.mockResolvedValue({})
  mocks.update.mockResolvedValue({})
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ''; vi.useRealTimers() })

describe('OAuth intelligence plan dialog', () => {
  it('requests active lite accounts and collapses the non-searchable selection before saving', async () => {
    mocks.accounts.mockResolvedValue(page(Array.from({ length: 7 }, (_, i) => oauthAccount(i + 1, i === 6 ? 'OAuth Seven' : `OAuth ${i + 1}`))))
    const view = render()
    await flushPromises()
    expect(mocks.accounts).toHaveBeenCalledWith(1, 100, { platform: 'openai', type: 'oauth', status: 'active', lite: '1' }, { signal: expect.any(AbortSignal) })
    expect(mocks.groups).not.toHaveBeenCalled()
    expect(view.find('#intelligence-name').exists()).toBe(false)
    expect(view.find('#intelligence-key').exists()).toBe(false)
    expect(view.find('#intelligence-endpoint').exists()).toBe(false)
    expect(view.find('#intelligence-oauth-search').exists()).toBe(false)
    expect(view.find('select').exists()).toBe(false)
    await view.get('form').trigger('submit')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.form.requiredSource')
    const trigger = view.get('#intelligence-oauth-account')
    await trigger.trigger('click')
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.body.querySelector('.select-search-input')).toBeNull()
    const account = [...document.body.querySelectorAll<HTMLElement>('[role="option"]')].find(item => item.textContent?.includes('OAuth Seven'))!
    account.click()
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(view.get('.oauth-account-select').classes()).toContain('oauth-account-selected')
    expect(trigger.text()).toContain('OAuth Seven')
    await trigger.trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[aria-selected="true"] .intelligence-oauth-option')!.classList.contains('bg-emerald-50')).toBe(true)
    await trigger.trigger('click')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ source_type: 'openai_oauth', account_id: 7, name: '', enabled: false, interval_seconds: 3600, timeout_seconds: 900, api_mode: 'responses', endpoint: undefined, api_key: undefined, upstream_target_id: null, group_id: null }))
    expect(view.emitted('saved')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('loads every page before enabling selection and filters unavailable accounts', async () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    let resolveSecond!: (value: unknown) => void
    mocks.accounts.mockResolvedValueOnce(page([
      oauthAccount(1, 'Paused', { schedulable: false }), oauthAccount(2, 'Inactive', { status: 'inactive' }),
      oauthAccount(3, 'Error', { status: 'error' }), oauthAccount(4, 'API Secret', { type: 'apikey' }),
      oauthAccount(5, 'Claude OAuth', { platform: 'anthropic' }), oauthAccount(6, 'Overloaded', { overload_until: future }),
      oauthAccount(7, 'Rate limited', { rate_limit_reset_at: future }), oauthAccount(8, 'Cooling', { temp_unschedulable_until: future }),
      oauthAccount(9, 'Expired', { auto_pause_on_expired: true, expires_at: Math.floor(Date.now() / 1000) - 1 }),
      oauthAccount(10, 'Shadow', { parent_account_id: 1 }), oauthAccount(11, 'Synthetic', { extra: { synthetic_ui_test: true } })
    ], 1, 2))
    mocks.accounts.mockReturnValueOnce(new Promise(resolve => { resolveSecond = resolve }))
    const view = render()
    await flushPromises()
    expect(mocks.accounts.mock.calls.map(call => call[0])).toEqual([1, 2])
    expect(view.get('#intelligence-oauth-account').attributes('disabled')).toBeDefined()
    expect(view.get('[role="status"]').text()).toBe('intelligenceMonitor.oauth.loading')
    await view.get('form').trigger('submit')
    expect(mocks.create).not.toHaveBeenCalled()
    resolveSecond(page([oauthAccount(20, 'Second page account'), oauthAccount(20, 'Updated second page account')], 2, 2))
    await flushPromises()
    expect(view.get('#intelligence-oauth-account').attributes('disabled')).toBeUndefined()
    await view.get('#intelligence-oauth-account').trigger('click')
    await flushPromises()
    const options = [...document.body.querySelectorAll('[role="option"]')]
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toContain('Updated second page account')
  })

  it('ignores a cancelled later page after close and reopen', async () => {
    let resolveOld!: (value: unknown) => void
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(8, 'Old first page')], 1, 2))
    mocks.accounts.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const view = render()
    await flushPromises()
    const oldSignal = mocks.accounts.mock.calls[1][3].signal as AbortSignal
    await view.setProps({ show: false })
    expect(oldSignal.aborted).toBe(true)
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(9, 'New account')]))
    await view.setProps({ show: true })
    await flushPromises()
    resolveOld(page([oauthAccount(10, 'Stale account')], 2, 2))
    await flushPromises()
    await view.get('#intelligence-oauth-account').trigger('click')
    await flushPromises()
    const dropdown = document.body.querySelector('[role="listbox"]')!
    expect(dropdown.textContent).toContain('New account')
    expect(dropdown.textContent).not.toContain('Stale account')
    expect(dropdown.textContent).not.toContain('Old first page')
    expect(view.find('[role="alert"]').exists()).toBe(false)
  })

  it('cancels loading immediately when cancel is pressed', async () => {
    mocks.accounts.mockReturnValueOnce(new Promise(() => {}))
    const view = render()
    const signal = mocks.accounts.mock.calls[0][3].signal as AbortSignal
    await view.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    expect(signal.aborted).toBe(true)
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('does not offer a partial list when a later page fails and allows a clean retry', async () => {
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(8, 'Partial account')], 1, 2))
    mocks.accounts.mockRejectedValueOnce(new Error('Private upstream details'))
    const view = render()
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.loadFailed')
    expect(view.text()).not.toContain('Private upstream details')
    expect(view.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await view.get('#intelligence-oauth-account').trigger('click')
    await flushPromises()
    expect(document.body.querySelectorAll('[role="option"]')).toHaveLength(0)
    await view.get('[aria-label="intelligenceMonitor.refresh"]').trigger('click')
    await flushPromises()
    await selectOAuth(view)
    expect(view.get('#intelligence-oauth-account').text()).toContain('OAuth Seven')
  })

  it('updates an edited account name and clears a selection made unavailable by refresh', async () => {
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(7, 'Renamed OAuth')]))
    const view = render({ plan: savedPlan() })
    await flushPromises()
    expect(view.get('#intelligence-oauth-account').text()).toContain('Renamed OAuth')
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(7, 'Renamed OAuth', { schedulable: false })]))
    await view.get('[aria-label="intelligenceMonitor.refresh"]').trigger('click')
    await flushPromises()
    expect(view.get('#intelligence-oauth-account').text()).not.toContain('Renamed OAuth')
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.unavailable')
    expect(view.get('.oauth-account-select').classes()).not.toContain('oauth-account-selected')
    await view.get('form').trigger('submit')
    expect(mocks.update).not.toHaveBeenCalled()
  })

  it('does not silently replace a missing account when editing', async () => {
    const view = render({ plan: savedPlan({ account_id: 99 }) })
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.unavailable')
    expect(view.get('#intelligence-oauth-account').text()).not.toContain('OAuth Seven')
    await view.get('form').trigger('submit')
    expect(mocks.update).not.toHaveBeenCalled()
    await selectOAuth(view)
    expect(view.findAll('[role="alert"]')).toHaveLength(0)
  })

  it('rechecks expiry before saving an account selected earlier', async () => {
    vi.useFakeTimers()
    mocks.accounts.mockResolvedValueOnce(page([oauthAccount(7, 'OAuth Seven', { auto_pause_on_expired: true, expires_at: Math.floor(Date.now() / 1000) + 1 })]))
    const view = render()
    await flushPromises()
    await selectOAuth(view)
    vi.setSystemTime(Date.now() + 2000)
    await view.get('form').trigger('submit')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.unavailable')
  })

  it('keeps save failures visible and prevents duplicate saves', async () => {
    const view = render()
    await flushPromises()
    await selectOAuth(view)
    let rejectSave!: (reason: unknown) => void
    mocks.create.mockReturnValueOnce(new Promise((_, reject) => { rejectSave = reject }))
    await view.get('form').trigger('submit')
    await view.get('form').trigger('submit')
    expect(mocks.create).toHaveBeenCalledTimes(1)
    rejectSave({ message: 'Account no longer supports this model.' })
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('Account no longer supports this model.')
    expect(view.emitted('saved')).toBeUndefined()
    expect(view.emitted('close')).toBeUndefined()
  })

  it('shows OAuth metadata safely and refreshes eligibility after account rejection', async () => {
    const actualError = { status: 400, code: 400, reason: 'INTELLIGENCE_MONITOR_INVALID', message: 'invalid intelligence monitoring configuration', metadata: { detail: 'the selected account must support the fixed gpt-6-astra model without remapping' } }
    mocks.create.mockRejectedValueOnce(actualError)
    const view = render()
    await flushPromises()
    await selectOAuth(view)
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.fixedModelRequired')
    const unsafeDetail = '<img src=x onerror="alert(1)">'
    mocks.create.mockRejectedValueOnce({ ...actualError, metadata: { detail: unsafeDetail } })
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe(unsafeDetail)
    expect(view.get('[role="alert"]').find('img').exists()).toBe(false)
    mocks.create.mockRejectedValueOnce({ ...actualError, metadata: { field: 'account_id', detail: 'the selected OAuth account is disabled, paused, expired, rate limited or cooling down' } })
    mocks.accounts.mockResolvedValueOnce(page([]))
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.accounts).toHaveBeenCalledTimes(2)
    expect(view.text()).toContain('intelligenceMonitor.oauth.unavailable')
    expect(view.get('#intelligence-oauth-account').text()).not.toContain('OAuth Seven')
    expect(view.emitted('close')).toBeUndefined()
  })
})

describe('intelligence plan choices and interval validation', () => {
  it('uses shared non-searchable selects and disabled supplier group headers', async () => {
    mocks.groups.mockResolvedValue([{ id: 5, name: 'Local GPT', platform: 'openai', rate_multiplier: 1.2 }])
    const overview = { suppliers: [{ id: 1, name: 'Supplier One', targets: [{ id: 11, name: 'GPT Key', provider: 'openai', endpoint: 'https://relay.example' }, { id: 12, name: 'Claude Key', provider: 'anthropic' }] }] } as UpstreamOverview
    const view = render({ oauthOnly: false, overview })
    await flushPromises()
    expect(view.find('select').exists()).toBe(false)
    expect(view.findAllComponents(Select)).toHaveLength(2)
    for (const select of view.findAllComponents(Select)) expect(select.props('searchable')).toBe(false)
    await view.get('#intelligence-upstream').trigger('click')
    await flushPromises()
    const header = document.body.querySelector<HTMLElement>('.select-option-group')!
    expect(header.textContent).toBe('Supplier One')
    expect(header.getAttribute('aria-disabled')).toBe('true')
    header.click()
    await flushPromises()
    expect(view.get('#intelligence-upstream').attributes('aria-expanded')).toBe('true')
    expect(document.body.querySelector('[role="listbox"]')?.textContent).not.toContain('Claude Key')
    await view.get('#intelligence-upstream').trigger('click')
    await selectOption(view, '#intelligence-upstream', 'GPT Key')
    expect(view.get('#intelligence-upstream').attributes('aria-expanded')).toBe('false')
    await view.findAll('button').find(button => button.text() === 'intelligenceMonitor.source.local_group')!.trigger('click')
    await selectOption(view, '#intelligence-group', 'Local GPT')
    await selectOption(view, '#intelligence-api-mode', 'Chat Completions')
    await view.get('#intelligence-name').setValue('Local plan')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ source_type: 'local_group', group_id: 5, api_mode: 'chat_completions' }))
  })

  it('defaults to 15 minutes and selects minute timeout presets while submitting seconds', async () => {
    const view = render()
    await flushPromises()
    await selectOAuth(view)
    expect(view.find('#intelligence-timeout').exists()).toBe(false)
    expect(view.get('[data-timeout="900"]').attributes('aria-pressed')).toBe('true')
    expect(view.find('[data-timeout="180"]').exists()).toBe(false)
    expect(view.find('[data-timeout="240"]').exists()).toBe(false)
    for (const seconds of [300, 600, 900]) {
      await view.get(`[data-timeout="${seconds}"]`).trigger('click')
      expect(view.get(`[data-timeout="${seconds}"]`).attributes('aria-pressed')).toBe('true')
      expect(view.get(`[data-timeout="${seconds}"]`).text()).toBe(`intelligenceMonitor.minutes:${seconds / 60}`)
    }
    await view.get('#intelligence-enabled').trigger('click')
    expect(view.get('[data-interval="3600"]').attributes('aria-pressed')).toBe('true')
    await view.get('[data-interval="300"]').trigger('click')
    expect(view.get('[data-interval="300"]').text()).toBe('intelligenceMonitor.minutes:5')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, interval_seconds: 300, timeout_seconds: 900 }))
  })

  it.each([180, 240])('keeps a legacy %s-second timeout selected and unchanged until explicitly edited', async seconds => {
    const view = render({ plan: savedPlan({ timeout_seconds: seconds }) })
    await flushPromises()
    const legacyChoice = view.get(`[data-timeout="${seconds}"]`)
    expect(legacyChoice.attributes('aria-pressed')).toBe('true')
    expect(legacyChoice.text()).toBe(`intelligenceMonitor.minutes:${seconds / 60}`)
    expect(view.findAll('[data-timeout]')).toHaveLength(4)
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(3, expect.objectContaining({ timeout_seconds: seconds }))
    await view.get('[data-timeout="900"]').trigger('click')
    expect(legacyChoice.attributes('aria-pressed')).toBe('false')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(3, expect.objectContaining({ timeout_seconds: 900 }))
  })

  it('restores custom intervals and preserves drafts across preset and schedule toggles', async () => {
    const view = render({ plan: savedPlan({ interval_seconds: 45 }) })
    await flushPromises()
    expect(view.get('[data-interval="custom"]').attributes('aria-pressed')).toBe('true')
    expect((view.get('#intelligence-interval-custom').element as HTMLInputElement).value).toBe('45')
    await view.get('#intelligence-interval-custom').setValue('75')
    await view.get('[data-interval="3600"]').trigger('click')
    await view.get('[data-interval="custom"]').trigger('click')
    expect((view.get('#intelligence-interval-custom').element as HTMLInputElement).value).toBe('75')
    await view.get('#intelligence-enabled').trigger('click')
    await view.get('#intelligence-enabled').trigger('click')
    expect((view.get('#intelligence-interval-custom').element as HTMLInputElement).value).toBe('75')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(3, expect.objectContaining({ interval_seconds: 75 }))
  })

  it('preserves a valid custom interval while saving with scheduling disabled', async () => {
    const view = render({ plan: savedPlan({ interval_seconds: 45 }) })
    await flushPromises()
    await view.get('#intelligence-interval-custom').setValue('90')
    await view.get('#intelligence-enabled').trigger('click')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(3, expect.objectContaining({ enabled: false, interval_seconds: 90 }))
  })

  it.each(['', ' ', '29', '86401', '30.5', '300.0', '1e3', '-30', 'abc'])('rejects invalid custom interval %j', async value => {
    const view = render({ plan: savedPlan({ interval_seconds: 45 }) })
    await flushPromises()
    await view.get('#intelligence-interval-custom').setValue(value)
    await view.get('form').trigger('submit')
    expect(mocks.update).not.toHaveBeenCalled()
    expect(view.get('#intelligence-interval-error').text()).toBe('intelligenceMonitor.form.validInterval')
    expect(view.get('#intelligence-interval-custom').attributes('aria-invalid')).toBe('true')
  })

  it.each(['30', '86400'])('accepts the custom interval boundary %s', async value => {
    const view = render({ plan: savedPlan({ interval_seconds: 45 }) })
    await flushPromises()
    await view.get('#intelligence-interval-custom').setValue(value)
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(3, expect.objectContaining({ interval_seconds: Number(value) }))
  })
})
