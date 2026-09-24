import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IntelligencePlanDialog from './IntelligencePlanDialog.vue'

const mocks = vi.hoisted(() => ({ accounts: vi.fn(), groups: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/accounts', () => ({ list: mocks.accounts }))
vi.mock('@/api/admin/groups', () => ({ getAll: mocks.groups }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: { create: mocks.create, update: mocks.update }, PELICAN_MODEL: 'gpt-6-astra', PELICAN_REASONING: 'high', PELICAN_PROMPT: 'Pelican animation' }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const oauthAccount = (id: number, name: string) => ({ id, name, platform: 'openai', type: 'oauth' })
let wrapper: VueWrapper | undefined
function render() {
  wrapper = mount(IntelligencePlanDialog, { props: { show: true, plan: null, overview: null, oauthOnly: true }, global: { stubs: { BaseDialog: dialog, Toggle: true, Icon: true } } })
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.accounts.mockResolvedValue({ items: [oauthAccount(7, 'OAuth Seven')], total: 1 })
  mocks.groups.mockResolvedValue([])
  mocks.create.mockResolvedValue({})
  mocks.update.mockResolvedValue({})
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })

describe('OAuth intelligence plan dialog', () => {
  it('requests lite OpenAI OAuth accounts and submits only the selected reference with scheduling off', async () => {
    const view = render()
    await flushPromises()
    expect(mocks.accounts).toHaveBeenCalledWith(1, 30, { platform: 'openai', type: 'oauth', lite: '1', search: '' }, { signal: expect.any(AbortSignal) })
    expect(mocks.groups).not.toHaveBeenCalled()
    expect(view.find('#intelligence-name').exists()).toBe(false)
    expect(view.find('#intelligence-key').exists()).toBe(false)
    expect(view.find('#intelligence-endpoint').exists()).toBe(false)
    await view.get('form').trigger('submit')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.form.requiredSource')
    await view.findAll('button').find(button => button.text().includes('OAuth Seven'))!.trigger('click')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ source_type: 'openai_oauth', account_id: 7, name: '', enabled: false, api_mode: 'responses', endpoint: undefined, api_key: undefined, upstream_target_id: null, group_id: null }))
    expect(view.emitted('saved')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('filters unexpected account types, paginates and debounces searches back to page one', async () => {
    vi.useFakeTimers()
    mocks.accounts.mockResolvedValueOnce({ items: [oauthAccount(7, 'OAuth Seven'), { id: 8, name: 'API Secret', platform: 'openai', type: 'apikey' }, { id: 9, name: 'Claude OAuth', platform: 'anthropic', type: 'oauth' }], total: 65 })
    const view = render()
    await flushPromises()
    expect(view.text()).not.toContain('API Secret')
    expect(view.text()).not.toContain('Claude OAuth')
    mocks.accounts.mockResolvedValueOnce({ items: [oauthAccount(40, 'OAuth Forty')], total: 65 })
    await view.findAll('button').find(button => button.text() === 'intelligenceMonitor.oauth.more')!.trigger('click')
    await flushPromises()
    expect(mocks.accounts.mock.calls[1][0]).toBe(2)
    await view.get('#intelligence-oauth-search').setValue('  desired account  ')
    expect(mocks.accounts).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(mocks.accounts).toHaveBeenLastCalledWith(1, 30, expect.objectContaining({ search: 'desired account' }), { signal: expect.any(AbortSignal) })
  })

  it('ignores an aborted account response after close and reopen', async () => {
    let resolveOld!: (value: unknown) => void
    mocks.accounts.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const view = render()
    const firstSignal = mocks.accounts.mock.calls[0][3].signal as AbortSignal
    await view.setProps({ show: false })
    expect(firstSignal.aborted).toBe(true)
    mocks.accounts.mockResolvedValueOnce({ items: [oauthAccount(9, 'New account')], total: 1 })
    await view.setProps({ show: true })
    await flushPromises()
    resolveOld({ items: [oauthAccount(8, 'Stale account')], total: 1 })
    await flushPromises()
    expect(view.text()).toContain('New account')
    expect(view.text()).not.toContain('Stale account')
    expect(view.find('[role="alert"]').exists()).toBe(false)
  })

  it('keeps loading and save failures visible without closing the editor or duplicating saves', async () => {
    mocks.accounts.mockRejectedValueOnce(new Error('Private upstream details'))
    const view = render()
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.loadFailed')
    expect(view.text()).not.toContain('Private upstream details')
    await view.get('[aria-label="intelligenceMonitor.refresh"]').trigger('click')
    await flushPromises()
    await view.findAll('button').find(button => button.text().includes('OAuth Seven'))!.trigger('click')
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

  it('shows the actual OAuth 400 metadata detail as text instead of the generic error', async () => {
    const detail = 'the selected account must support the fixed gpt-6-astra model without remapping'
    const actualError = { status: 400, code: 400, reason: 'INTELLIGENCE_MONITOR_INVALID', message: 'invalid intelligence monitoring configuration', metadata: { detail } }
    mocks.create.mockRejectedValueOnce(actualError)
    const view = render()
    await flushPromises()
    await view.findAll('button').find(button => button.text().includes('OAuth Seven'))!.trigger('click')
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe('intelligenceMonitor.oauth.fixedModelRequired')
    expect(view.get('[role="alert"]').text()).not.toContain(actualError.message)
    expect(view.emitted('close')).toBeUndefined()
    const unsafeDetail = '<img src=x onerror="alert(1)">'
    mocks.create.mockRejectedValueOnce({ ...actualError, metadata: { detail: unsafeDetail } })
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toBe(unsafeDetail)
    expect(view.get('[role="alert"]').find('img').exists()).toBe(false)
  })
})
