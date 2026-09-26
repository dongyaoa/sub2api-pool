import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamDeleteDialog from './UpstreamDeleteDialog.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const item = { kind: 'target' as const, id: 1, name: 'Primary group' }
function render(archiveAllowed = true) { return mount(UpstreamDeleteDialog, { props: { show: true, item, archiveAllowed }, global: { stubs: { BaseDialog: dialog, Icon: true } } }) }
describe('upstream removal confirmation', () => {
  it('defaults to archive and requires the exact name for permanent deletion', async () => {
    const view = render()
    expect(view.get('[data-delete-mode="archive"]').attributes('aria-pressed')).toBe('true')
    await view.get('[data-testid="confirm-removal"]').trigger('click')
    expect(view.emitted('confirm')).toEqual([['archive']])
    await view.get('[data-delete-mode="purge"]').trigger('click')
    expect(view.get('[data-testid="confirm-removal"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="purge-confirm-name"]').setValue('primary group')
    await view.get('[data-testid="purge-confirm-name"]').trigger('keydown', { key: 'Enter' })
    expect(view.emitted('confirm')).toHaveLength(1)
    await view.get('[data-testid="purge-confirm-name"]').setValue(item.name)
    await view.get('[data-testid="confirm-removal"]').trigger('click')
    expect(view.emitted('confirm')).toEqual([['archive'], ['purge']])
    view.unmount()
  })
  it('clears confirmation across item and dialog changes and blocks submission while busy', async () => {
    const view = render(false)
    expect(view.find('[data-delete-mode]').exists()).toBe(false)
    await view.get('[data-testid="purge-confirm-name"]').setValue(item.name)
    await view.setProps({ busy: true })
    await view.get('[data-testid="purge-confirm-name"]').trigger('keydown', { key: 'Enter' })
    expect(view.emitted('confirm')).toBeUndefined()
    await view.setProps({ busy: false, item: { ...item, id: 2, name: 'Second group' } })
    expect((view.get('[data-testid="purge-confirm-name"]').element as HTMLInputElement).value).toBe('')
    await view.get('[data-testid="purge-confirm-name"]').setValue('Second group')
    await view.setProps({ show: false })
    await view.setProps({ show: true })
    expect((view.get('[data-testid="purge-confirm-name"]').element as HTMLInputElement).value).toBe('')
    view.unmount()
  })
})
