import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ReAuthAccountModal from '../ReAuthAccountModal.vue'
import type { Account } from '@/types'

const api = vi.hoisted(() => ({ generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), applyOAuthCredentials: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: api.showError }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

const flow = defineComponent({
  props: ['showRefreshTokenOption'],
  emits: ['generate-url'],
  setup(_, { expose }) {
    expose({ authCode: ref('fixture-code'), oauthState: ref('fixture-state'), inputMethod: ref('manual') })
    return {}
  },
  template: '<button data-testid="generate" @click="$emit(\'generate-url\')">Generate</button>'
})

function mountModal(pending: boolean) {
  const account = { id: 42, name: 'Fixture', platform: 'openai', type: 'oauth', proxy_id: 8,
    credentials: {}, extra: { openai_auto_reauth_pending: pending } } as Account
  return mount(ReAuthAccountModal, { props: { show: true, account }, global: { stubs: {
    BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, OAuthAuthorizationFlow: flow, Icon: true
  } } })
}

beforeEach(() => {
  vi.clearAllMocks()
  api.generateAuthUrl.mockResolvedValue({ auth_url: 'https://auth.openai.com/oauth/authorize?state=fixture-state', session_id: 'server-session' })
  api.exchangeCode.mockResolvedValue({ access_token: 'fixture-token', expires_at: 1234 })
  api.applyOAuthCredentials.mockResolvedValue({ id: 42, extra: {} })
})

describe('verified manual recovery of automatic authorization', () => {
  it('submits an unconsumed OAuth session and hides arbitrary token recovery for pending accounts', async () => {
    const wrapper = mountModal(true)
    expect(wrapper.getComponent(flow).props('showRefreshTokenOption')).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.autoReauth.manualHint')
    await wrapper.get('[data-testid="generate"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.oauth.completeAuth')!.trigger('click')
    await flushPromises()
    expect(api.exchangeCode).not.toHaveBeenCalled()
    expect(api.applyOAuthCredentials).toHaveBeenCalledWith(42, {
      type: 'oauth', credentials: {}, openai_oauth_session: { session_id: 'server-session', code: 'fixture-code', state: 'fixture-state' }
    })
    expect(wrapper.emitted('reauthorized')).toEqual([[{ id: 42, extra: {} }]])
    wrapper.unmount()
  })

  it('keeps the existing exchange flow for accounts without a pending automatic recovery', async () => {
    const wrapper = mountModal(false)
    expect(wrapper.getComponent(flow).props('showRefreshTokenOption')).toBe(true)
    await wrapper.get('[data-testid="generate"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.oauth.completeAuth')!.trigger('click')
    await flushPromises()
    expect(api.exchangeCode).toHaveBeenCalledOnce()
    expect(api.applyOAuthCredentials).toHaveBeenCalledWith(42, {
      type: 'oauth', credentials: { access_token: 'fixture-token', expires_at: 1234 }, extra: undefined
    })
    wrapper.unmount()
  })

  it('keeps the dialog open and shows a safe message when verified recovery fails', async () => {
    api.applyOAuthCredentials.mockRejectedValue(new Error('upstream details with sensitive data'))
    const wrapper = mountModal(true)
    await wrapper.get('[data-testid="generate"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.oauth.completeAuth')!.trigger('click')
    await flushPromises()
    expect(api.showError).toHaveBeenCalledWith('admin.accounts.autoReauth.manualFailed')
    expect(wrapper.emitted('reauthorized')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })
})
