import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IntelligencePublicDisplayDialog from './IntelligencePublicDisplayDialog.vue'
import type { IntelligencePlan, IntelligencePublicDisplaySettings, IntelligenceSource } from '@/api/admin/intelligenceMonitor'

const api = vi.hoisted(() => ({ publicDisplay: vi.fn(), updatePublicDisplay: vi.fn(), plans: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: api.showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const settings = (): IntelligencePublicDisplaySettings => ({ enabled: false, title: 'Public title', description: 'Public description', notice: 'Public notice', plan_ids: [2] })
const plan = (id: number, source_type: IntelligenceSource = 'local_group'): IntelligencePlan => ({
  id, source_type, name: `PRIVATE PLAN ${id}`, supplier_note: 'PRIVATE SUPPLIER', group_note: 'PRIVATE GROUP NOTE', rate_note: 'PRIVATE RATE', notes: 'PRIVATE NOTES',
  api_mode: 'responses', enabled: false, interval_seconds: 300, timeout_seconds: 600,
  model: 'gpt-6-astra', reasoning_effort: 'high', prompt: '', api_key_masked: 'PRIVATE KEY', source_name: 'PRIVATE SOURCE', rate_snapshot: null,
  created_by: 1, created_at: '', updated_at: '', last_run_at: null, next_run_at: null, latest_run: null,
  group_id: id, local_group_name: `Visible group ${id}`, local_group_status: 'active', local_group_rate_multiplier: id === 2 ? 0 : 1.5,
})
let wrapper: VueWrapper | undefined
function render(show = true) {
  wrapper = mount(IntelligencePublicDisplayDialog, { props: { show }, global: { stubs: { BaseDialog: dialog, Icon: true } } })
  return wrapper
}
const saveButton = '[data-testid="save-public-display"]'
const checkbox = (id: number) => `[data-public-plan="${id}"] input`
const input = (name: string) => `#intelligence-public-${name}`
beforeEach(() => {
  vi.clearAllMocks()
  api.publicDisplay.mockResolvedValue(settings())
  api.plans.mockResolvedValue({ items: [plan(1), plan(2), plan(3, 'external'), plan(4, 'openai_oauth'), plan(5, 'upstream')] })
  api.updatePublicDisplay.mockImplementation(async value => value)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('intelligence public display settings', () => {
  it('only loads on opening, preserves saved copy and selections and never copies private plan information', async () => {
    const view = render(false)
    expect(api.publicDisplay).not.toHaveBeenCalled()
    expect(api.plans).not.toHaveBeenCalled()
    await view.setProps({ show: true }); await flushPromises()
    expect(api.publicDisplay).toHaveBeenCalledTimes(1)
    expect(view.findAll('[data-public-plan]')).toHaveLength(2)
    expect(view.get('[data-public-plan="2"]').classes()).toContain('bg-primary-50/80')
    expect(view.get('[data-public-plan="2"]').text()).toContain('0×')
    expect((view.get(checkbox(2)).element as HTMLInputElement).checked).toBe(true)
    expect(view.text()).not.toContain('PRIVATE')
    expect((view.get(input('title')).element as HTMLInputElement).value).toBe('Public title')
    expect((view.get(input('description')).element as HTMLTextAreaElement).value).toBe('Public description')
    expect((view.get(input('notice')).element as HTMLTextAreaElement).value).toBe('Public notice')
    await view.get(saveButton).trigger('click'); await flushPromises()
    expect(api.updatePublicDisplay).toHaveBeenCalledWith(settings())
    expect(view.emitted('saved')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('saves explicit copy and local selections independently of paused or running tests', async () => {
    api.plans.mockResolvedValueOnce({ items: [plan(1), { ...plan(2), enabled: true, latest_run: { id: 8, status: 'running' } }] })
    const view = render(); await flushPromises()
    await view.get(input('enabled')).trigger('click')
    await view.get(input('title')).setValue('New public title')
    await view.get(input('description')).setValue('New public description')
    await view.get(input('notice')).setValue('New public notice')
    await view.get(checkbox(1)).setValue(true)
    await view.get(saveButton).trigger('click'); await flushPromises()
    expect(api.updatePublicDisplay).toHaveBeenCalledWith({ enabled: true, title: 'New public title', description: 'New public description', notice: 'New public notice', plan_ids: [2, 1] })
  })

  it.each(['settings', 'plans'])('fails closed when %s loading fails and allows retry without submitting defaults', async failure => {
    if (failure === 'settings') api.publicDisplay.mockRejectedValueOnce(new Error('Settings unavailable'))
    else api.plans.mockRejectedValueOnce(new Error('Plans unavailable'))
    const view = render()
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    await flushPromises()
    expect(view.find(input('title')).exists()).toBe(false)
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    await view.get(saveButton).trigger('click')
    expect(api.updatePublicDisplay).not.toHaveBeenCalled()
    await view.get('[data-testid="retry-public-display"]').trigger('click'); await flushPromises()
    expect((view.get(input('title')).element as HTMLInputElement).value).toBe(settings().title)
    expect(view.get(saveButton).attributes('disabled')).toBeUndefined()
  })

  it('keeps unavailable saved IDs visible and blocks saving until explicitly removed', async () => {
    api.publicDisplay.mockResolvedValueOnce({ ...settings(), plan_ids: [2, 3, 99] })
    const view = render(); await flushPromises()
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    expect(view.get('[data-testid="public-display-remove-unavailable"]').exists()).toBe(true)
    await view.get('[data-testid="public-display-remove-unavailable"]').trigger('click')
    await view.get(saveButton).trigger('click'); await flushPromises()
    expect(api.updatePublicDisplay).toHaveBeenCalledWith(settings())
  })

  it('excludes inactive or deleted groups but keeps paused plans for active groups', async () => {
    api.plans.mockResolvedValueOnce({ items: [plan(1), { ...plan(2), local_group_status: 'disabled' }, { ...plan(3), local_group_status: '' }] })
    const view = render(); await flushPromises()
    expect(view.findAll('[data-public-plan]')).toHaveLength(1)
    expect(view.get('[data-public-plan="1"]').exists()).toBe(true)
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    expect(view.get('[data-testid="public-display-remove-unavailable"]').exists()).toBe(true)
  })

  it.each([[0], [-1], [1.5], [2, 2], ['2']])('rejects malformed saved plan IDs %j', async (...ids) => {
    api.publicDisplay.mockResolvedValueOnce({ ...settings(), plan_ids: ids })
    const view = render(); await flushPromises()
    expect(view.find(input('title')).exists()).toBe(false)
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    expect(api.updatePublicDisplay).not.toHaveBeenCalled()
  })

  it('supports clearing and selecting local plans without filling empty public copy', async () => {
    api.publicDisplay.mockResolvedValueOnce({ enabled: false, title: '', description: '', notice: '', plan_ids: [] })
    const view = render(); await flushPromises()
    await view.get('[data-testid="public-display-select-all"]').trigger('click')
    expect((view.get(checkbox(1)).element as HTMLInputElement).checked).toBe(true)
    expect((view.get(checkbox(2)).element as HTMLInputElement).checked).toBe(true)
    await view.get('[data-testid="public-display-clear"]').trigger('click')
    await view.get(saveButton).trigger('click'); await flushPromises()
    expect(api.updatePublicDisplay).toHaveBeenCalledWith({ enabled: false, title: '', description: '', notice: '', plan_ids: [] })
    expect(view.text()).not.toContain('PRIVATE')
  })

  it('enforces copy lengths and the 200-plan cap', async () => {
    api.plans.mockResolvedValueOnce({ items: Array.from({ length: 201 }, (_, i) => plan(i + 1)) })
    api.publicDisplay.mockResolvedValueOnce({ ...settings(), plan_ids: Array.from({ length: 200 }, (_, i) => i + 1) })
    const view = render(); await flushPromises()
    expect(view.get(checkbox(201)).attributes('disabled')).toBeDefined()
    expect(view.get('[data-testid="public-display-select-all"]').attributes('disabled')).toBeDefined()
    for (const [field, max] of [['title', 60], ['description', 240], ['notice', 1000]] as const) {
      await view.get(input(field)).setValue('x'.repeat(max + 1))
      expect(view.get(saveButton).attributes('disabled')).toBeDefined()
      expect(view.get(input(field)).attributes('aria-invalid')).toBe('true')
      await view.get(input(field)).setValue('x'.repeat(max))
    }
    expect(view.get(saveButton).attributes('disabled')).toBeUndefined()
  })

  it('preserves edits on save errors and guards against repeated saves and closing', async () => {
    api.updatePublicDisplay.mockRejectedValueOnce(new Error('Unable to write'))
    const view = render(); await flushPromises()
    await view.get(input('title')).setValue('Keep this title')
    await view.get(saveButton).trigger('click'); await flushPromises()
    expect(view.text()).toContain('Unable to write')
    expect(view.emitted('close')).toBeUndefined()
    expect((view.get(input('title')).element as HTMLInputElement).value).toBe('Keep this title')
    let finishSave!: (value: IntelligencePublicDisplaySettings) => void
    api.updatePublicDisplay.mockReturnValueOnce(new Promise(resolve => { finishSave = resolve }))
    await view.get(saveButton).trigger('click')
    expect(view.get(saveButton).attributes('disabled')).toBeDefined()
    expect(view.get(input('title')).attributes('disabled')).toBeDefined()
    view.getComponent(dialog).vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toBeUndefined()
    finishSave(settings()); await flushPromises()
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('ignores stale settings after closing and reopening', async () => {
    let finishOld!: (value: IntelligencePublicDisplaySettings) => void
    api.publicDisplay.mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
    const view = render()
    const signal = api.publicDisplay.mock.calls[0][0] as AbortSignal
    await view.setProps({ show: false })
    api.publicDisplay.mockResolvedValueOnce({ ...settings(), title: 'Current title' })
    await view.setProps({ show: true }); await flushPromises()
    finishOld(settings()); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect((view.get(input('title')).element as HTMLInputElement).value).toBe('Current title')
  })
})
