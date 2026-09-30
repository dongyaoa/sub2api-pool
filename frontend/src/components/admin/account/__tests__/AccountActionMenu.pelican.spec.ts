import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import type { Account } from '@/types'
import AccountActionMenu from '../AccountActionMenu.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
const account = (fields: Partial<Account> = {}) => ({ id: 42, name: 'OAuth account', platform: 'openai', type: 'oauth', parent_account_id: null, status: 'active', schedulable: true, ...fields }) as Account
function render(value: Account) {
  wrapper = mount(AccountActionMenu, { props: { show: true, account: value, anchorRect: new DOMRect(100, 100, 24, 24) }, global: { stubs: { Teleport: true, Icon: true } } })
  return wrapper
}
describe('account pelican monitor menu entry', () => {
  it.each([
    {},
    { status: 'inactive', schedulable: false },
    { status: 'error', schedulable: false },
    { rate_limit_reset_at: '2099-01-01T00:00:00Z', overload_until: '2099-01-01T00:00:00Z' },
    { temp_unschedulable_until: '2099-01-01T00:00:00Z' },
  ] as Partial<Account>[])('allows real OAuth history regardless of scheduling state %o', async fields => {
    const value = account(fields), view = render(value)
    const button = view.get('[data-testid="account-pelican-monitor"]')
    expect(button.text()).toBe('intelligenceMonitor.accountMonitor.entry')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    expect(view.emitted('pelican-monitor')).toEqual([[value]])
    expect(view.emitted('close')).toEqual([[]])
  })
  it.each([
    { platform: 'anthropic' }, { platform: 'grok' }, { platform: 'antigravity' },
    { type: 'apikey' }, { type: 'setup-token' },
    { parent_account_id: 9 }, { parent_account_id: 0 },
    { extra: { synthetic_ui_test: true } },
  ] as Partial<Account>[])('does not offer monitoring for unsupported accounts %o', fields => {
    expect(render(account(fields)).find('[data-testid="account-pelican-monitor"]').exists()).toBe(false)
  })
})
