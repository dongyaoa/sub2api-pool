import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import IntelligenceExecutionSource from './IntelligenceExecutionSource.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const run = (snapshot: Record<string, unknown> | null) => ({ source_type: 'local_group', source_name: 'Current group is not actual account', source_snapshot: snapshot }) as IntelligenceRun
const render = (snapshot: Record<string, unknown> | null) => mount(IntelligenceExecutionSource, { props: { run: run(snapshot) }, global: { stubs: { Icon: true } } })
describe('actual execution provenance', () => {
  it('does not infer the executing account from a group name or legacy snapshot', () => {
    const view = render({ account_id: 99, name: 'Old current account' })
    expect(view.text()).toContain('intelligenceMonitor.execution.notRecorded')
    expect(view.text()).not.toContain('Current group')
    expect(view.text()).not.toContain('Old current account')
    view.unmount()
  })
  it('shows actual account ID, unique upstream mapping and configured origin without turning it into a dynamic URL', () => {
    const view = render({ execution_account_id: 12, execution_account_name: 'Actual key account', execution_account_type: 'apikey', execution_account_platform: 'openai', execution_account_base_origin: 'https://relay.example', execution_attempt_count: 2, execution_source_status: 'completed', execution_binding_status: 'matched', execution_supplier_name: 'Relay', execution_upstream_target_name: 'Premium' })
    expect(view.text()).toContain('Actual key account · #12')
    expect(view.text()).toContain('openai · apikey')
    expect(view.text()).toContain('Relay')
    expect(view.text()).toContain('Premium')
    expect(view.text()).toContain('intelligenceMonitor.execution.origin')
    expect(view.find('a').exists()).toBe(false)
    view.unmount()
  })
  it.each(['ambiguous', 'unbound', 'unavailable', 'unknown'])('shows %s mapping and labels failures as attempted accounts', status => {
    const view = render({ execution_account_id: 13, execution_account_name: 'Attempted account', execution_source_status: 'attempted', execution_binding_status: status })
    expect(view.text()).toContain('intelligenceMonitor.execution.attemptedAccount')
    expect(view.text()).toContain(`intelligenceMonitor.execution.${status}`)
    view.unmount()
  })
  it('does not add local routing metadata to external requests', () => {
    const view = mount(IntelligenceExecutionSource, { props: { run: { ...run(null), source_type: 'external' } }, global: { stubs: { Icon: true } } })
    expect(view.find('[data-testid="execution-source"]').exists()).toBe(false)
    view.unmount()
  })
})
