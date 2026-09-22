import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import OpenAIAutoReauthPanel from '../OpenAIAutoReauthPanel.vue'
import { validateReauthImport } from '../openaiAutoReauthImport'
import { testMessageCompiler } from '@/__tests__/i18n'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import enCommon from '@/i18n/locales/en/common'
import type { OpenAIAutoReauthOverview } from '@/api/admin/openaiAutoReauth'

const api = vi.hoisted(() => ({ list: vi.fn(), importAccounts: vi.fn(), saveCredentials: vi.fn(), setEnabled: vi.fn(), run: vi.fn(), proxies: vi.fn() }))
vi.mock('@/api/admin/openaiAutoReauth', () => ({ openaiAutoReauthAPI: api }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { getAll: api.proxies } } }))

// Deliberately synthetic fixtures, never real account credentials.
const fakeSecret = 'JBSWY3DPEHPK3PXP'
const fakePassword = '  fixture\\password  '
const source = `test@example.invalid----${fakePassword}----${fakeSecret}`
const overview = (): OpenAIAutoReauthOverview => ({
  worker_configured: true,
  encryption_key_configured: true,
  accounts: [{ account_id: 42, email: 'test@example.invalid', enabled: true, status: 'failed', attempts: 1 }]
})
let wrapper: VueWrapper | undefined

function mountPanel(show = true, account?: { id: number; name: string; proxy_id: number | null; credentials?: Record<string, unknown> }) {
  wrapper = mount(OpenAIAutoReauthPanel, {
    props: { show, groups: [], account },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messageCompiler: testMessageCompiler, messages: { en: { ...enCommon, admin: enAccounts } } })],
      stubs: {
        BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /><button data-testid="dialog-close" @click="$emit(\'close\')">Close dialog</button></div>' },
        GroupSelector: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button type="button" data-testid="select-group" @click="$emit(\'update:modelValue\', [7])">Select group</button>' },
        Select: {
          props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
          template: '<select :disabled="disabled" :value="modelValue ?? \'\'" @change="$emit(\'update:modelValue\', $event.target.value ? Number($event.target.value) : null)"><option v-for="option in options" :key="String(option.value)" :value="option.value ?? \'\'">{{ option.label }}</option></select>'
        }
      }
    }
  })
  return wrapper
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  api.list.mockResolvedValue(overview())
  api.proxies.mockResolvedValue([
    { id: 8, name: 'Fixed proxy', status: 'active', expires_at: null },
    { id: 9, name: 'Inactive proxy', status: 'inactive', expires_at: null },
    { id: 10, name: 'Expired proxy', status: 'active', expires_at: '2000-01-01' }
  ])
  api.importAccounts.mockResolvedValue({ results: [{ line: 1, email: 'test@example.invalid', account_id: 42, status: 'queued' }] })
  api.setEnabled.mockResolvedValue(undefined)
  api.saveCredentials.mockResolvedValue(overview().accounts[0])
  api.run.mockResolvedValue(undefined)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks() })

describe('OpenAIAutoReauthPanel', () => {
  it('preserves password bytes, creates new accounts through a required proxy, and clears secrets after import', async () => {
    const page = mountPanel()
    await flushPromises()
    expect(page.findAll('option').map(option => option.text())).toEqual(['Select a fixed proxy (required)', 'Fixed proxy (#8)'])
    await page.get('textarea').setValue(source)
    expect(page.get('[data-testid="reauth-validation"]').text()).not.toContain(fakePassword)
    expect(page.get('[data-testid="reauth-validation"]').text()).not.toContain(fakeSecret)
    expect(page.get('[data-testid="reauth-import"]').attributes('disabled')).toBeDefined()
    await page.get('form').trigger('submit')
    expect(api.importAccounts).not.toHaveBeenCalled()
    await page.get('select').setValue('8')
    await page.get('[data-testid="select-group"]').trigger('click')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(api.importAccounts).toHaveBeenCalledWith({ content: source, mode: 'create', proxy_id: 8, group_ids: [7] })
    expect((page.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(page.get('[data-testid="reauth-results"]').text()).toContain('Queued')
    expect(page.text()).not.toContain(fakePassword)
    expect(page.text()).not.toContain(fakeSecret)
    expect(page.emitted('changed')).toHaveLength(1)
  })

  it('displays per-line conflicts so new import cannot silently replace an existing account', async () => {
    api.importAccounts.mockResolvedValue({ results: [
      { line: 1, email: 'test@example.invalid', account_id: 42, status: 'queued' },
      { line: 3, email: 'new@example.invalid', status: 'failed', error_code: 'account_exists' }
    ] })
    const page = mountPanel()
    await flushPromises()
    const input = `${source}\n\nnew@example.invalid----fixture----${fakeSecret}`
    await page.get('textarea').setValue(input)
    await page.get('select').setValue('8')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(api.importAccounts).toHaveBeenCalledWith({ content: input, mode: 'create', proxy_id: 8 })
    expect(page.get('[data-testid="reauth-results"]').text()).toContain('Line 3')
    expect(page.get('[data-testid="reauth-results"]').text()).toContain('The account already exists.')
  })

  it('shows configuration gaps and blocks import, enabling and retry, while allowing disable', async () => {
    api.list.mockResolvedValue({ ...overview(), worker_configured: false, encryption_key_configured: false, accounts: [
      ...overview().accounts,
      { ...overview().accounts[0], account_id: 43, enabled: false }
    ] })
    const page = mountPanel()
    await flushPromises()
    await page.get('textarea').setValue(source)
    expect(page.text()).toContain('The automatic login service is not configured.')
    expect(page.text()).toContain('The login credential encryption key is not configured.')
    expect(page.get('[data-testid="reauth-import"]').attributes('disabled')).toBeDefined()
    expect(page.get('[data-account-id="43"] [data-testid="reauth-toggle"]').attributes('disabled')).toBeDefined()
    expect(page.get('[data-account-id="42"] [data-testid="reauth-run"]').attributes('disabled')).toBeDefined()
    await page.get('[data-account-id="42"] [data-testid="reauth-toggle"]').trigger('click')
    await flushPromises()
    expect(api.setEnabled).toHaveBeenCalledWith(42, false)
  })

  it('enables accounts and queues retry with one outstanding mutation', async () => {
    api.list.mockResolvedValue({ ...overview(), accounts: [{ ...overview().accounts[0], enabled: false }] })
    const page = mountPanel()
    await flushPromises()
    api.list.mockResolvedValue(overview())
    await page.get('[data-testid="reauth-toggle"]').trigger('click')
    await flushPromises()
    expect(api.setEnabled).toHaveBeenCalledWith(42, true)
    let finish!: () => void
    api.run.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    await page.get('[data-testid="reauth-run"]').trigger('click')
    await page.get('[data-testid="reauth-run"]').trigger('click')
    expect(api.run).toHaveBeenCalledTimes(1)
    expect(api.run).toHaveBeenCalledWith(42)
    expect(page.get('[data-testid="reauth-toggle"]').attributes('disabled')).toBeDefined()
    finish()
    await flushPromises()
    expect(page.get('[data-testid="reauth-run"]').attributes('disabled')).toBeUndefined()
  })

  it('clears source on close and ignores an import completing after reopening', async () => {
    let finish!: (value: { results: unknown[] }) => void
    api.importAccounts.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const page = mountPanel()
    await flushPromises()
    await page.get('textarea').setValue(source)
    await page.get('select').setValue('8')
    await page.get('form').trigger('submit')
    await page.get('[data-testid="dialog-close"]').trigger('click')
    expect((page.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    await page.setProps({ show: false })
    await page.setProps({ show: true })
    await flushPromises()
    await page.get('textarea').setValue('new incomplete input')
    finish({ results: [{ line: 1, status: 'queued' }] })
    await flushPromises()
    expect((page.get('textarea').element as HTMLTextAreaElement).value).toBe('new incomplete input')
    expect(page.find('[data-testid="reauth-results"]').exists()).toBe(false)
  })

  it('never renders raw errors or unknown error codes that might contain credentials', async () => {
    api.list.mockResolvedValue({ ...overview(), accounts: [{ ...overview().accounts[0], last_error_code: fakePassword, status: fakeSecret }] })
    api.importAccounts.mockRejectedValue(new Error(`POST failed: ${source}`))
    const page = mountPanel()
    await flushPromises()
    expect(page.text()).toContain('Awaiting status update')
    expect(page.text()).not.toContain(fakePassword)
    expect(page.text()).not.toContain(fakeSecret)
    await page.get('textarea').setValue(source)
    await page.get('select').setValue('8')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(page.get('[role="alert"]').text()).toContain('Submission was not confirmed.')
    expect(page.text()).not.toContain(source)
  })

  it('validates every source line without copying secrets into results', async () => {
    const page = mountPanel()
    await flushPromises()
    const input = `${source}\n\nbad format\nwrong-email----fixture----${fakeSecret}\nempty@example.invalid--------${fakeSecret}\ncode@example.invalid----fixture----123456\nTEST@example.invalid----fixture----${fakeSecret}`
    await page.get('textarea').setValue(input)
    expect(validateReauthImport(input).map(row => [row.line, row.error])).toEqual([
      [1, undefined], [3, 'invalid_format'], [4, 'invalid_email'], [5, 'password_required'], [6, 'invalid_totp_secret'], [7, 'duplicate_email']
    ])
    expect(page.get('[data-testid="reauth-import"]').attributes('disabled')).toBeDefined()
    await page.get('form').trigger('submit')
    expect(api.importAccounts).not.toHaveBeenCalled()
    expect(JSON.stringify(validateReauthImport(input))).not.toContain(fakeSecret)
    expect(JSON.stringify(validateReauthImport(input))).not.toContain(fakePassword)
  })

  it('clears pasted details before opening the manual recovery action', async () => {
    const page = mountPanel()
    await flushPromises()
    await page.get('textarea').setValue(source)
    await page.get('[data-testid="reauth-manual"]').trigger('click')
    expect((page.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(page.emitted('close')).toHaveLength(1)
    expect(page.emitted('manual')).toEqual([[42]])
  })

  it('binds one existing account by ID without login or any settings mutation', async () => {
    const page = mountPanel(true, { id: 42, name: 'Account label', proxy_id: 8, credentials: { email: 'test@example.invalid' } })
    await flushPromises()
    expect(page.find('textarea').exists()).toBe(false)
    expect(api.proxies).not.toHaveBeenCalled()
    expect(page.get('[data-testid="reauth-email"]').element).toHaveProperty('value', 'test@example.invalid')
    expect(page.get('[data-testid="reauth-password"]').element).toHaveProperty('value', '')
    expect(page.get('[data-testid="reauth-totp"]').element).toHaveProperty('value', '')
    await page.get('[data-testid="reauth-paste"]').setValue(source)
    await page.get('[data-testid="reauth-paste"]').trigger('keydown', { key: 'Enter' })
    expect(page.get('[data-testid="reauth-paste"]').element).toHaveProperty('value', '')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(api.saveCredentials).toHaveBeenCalledWith(42, { email: 'test@example.invalid', password: fakePassword, totp_secret: fakeSecret, enabled: true })
    expect(api.importAccounts).not.toHaveBeenCalled()
    expect(api.run).not.toHaveBeenCalled()
    expect(page.get('[data-testid="reauth-password"]').element).toHaveProperty('value', '')
    expect(page.get('[data-testid="reauth-totp"]').element).toHaveProperty('value', '')
    expect(page.get('[data-testid="reauth-binding-saved"]').text()).toContain('Login details saved.')
    await page.get('[data-testid="reauth-run"]').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledWith(42)
  })

  it('preserves a disabled binding and clears secrets when switching accounts', async () => {
    api.list.mockResolvedValue({ ...overview(), accounts: [{ ...overview().accounts[0], enabled: false }] })
    const page = mountPanel(true, { id: 42, name: 'test@example.invalid', proxy_id: 8 })
    await flushPromises()
    await page.get('[data-testid="reauth-password"]').setValue(fakePassword)
    await page.get('[data-testid="reauth-totp"]').setValue(fakeSecret)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(api.saveCredentials).toHaveBeenCalledWith(42, expect.objectContaining({ enabled: false }))
    await page.get('[data-testid="reauth-password"]').setValue(fakePassword)
    await page.setProps({ account: { id: 43, name: 'other@example.invalid', proxy_id: 8 } })
    expect(page.get('[data-testid="reauth-password"]').element).toHaveProperty('value', '')
    expect(page.get('[data-testid="reauth-totp"]').element).toHaveProperty('value', '')
    expect(page.get('[data-testid="reauth-email"]').element).toHaveProperty('value', 'other@example.invalid')
  })

  it('treats an empty account list as normal and explains unavailable backend endpoints', async () => {
    api.list.mockResolvedValue({ ...overview(), accounts: [] })
    const page = mountPanel()
    await flushPromises()
    expect(page.text()).toContain('No accounts configured for automatic authorization.')
    expect(page.find('[role="alert"]').exists()).toBe(false)
    api.list.mockRejectedValue({ status: 404 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(page.get('[role="alert"]').text()).toContain('Update and restart the backend')
    const calls = api.list.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.list).toHaveBeenCalledTimes(calls)
  })
})
