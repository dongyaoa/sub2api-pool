import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IntelligenceConcurrencyDialog from './IntelligenceConcurrencyDialog.vue'
import type { IntelligenceConcurrency } from '@/api/admin/intelligenceMonitor'

const api = vi.hoisted(() => ({ concurrency: vi.fn(), updateConcurrency: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin/intelligenceMonitor', () => ({ intelligenceMonitorAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: api.showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const settings = (): IntelligenceConcurrency => ({ max_concurrency: 8, candy_max_concurrency: 4, source: 'deployment', pelican_running: 6, pelican_pending: 12, candy_running: 2, candy_pending: 0 })
const pelican = '#intelligence-concurrency-max_concurrency'
const candy = '#intelligence-concurrency-candy_max_concurrency'
let wrapper: VueWrapper | undefined
function render(show = true) {
  wrapper = mount(IntelligenceConcurrencyDialog, { props: { show }, global: { stubs: { BaseDialog: dialog, Icon: true } } })
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  api.concurrency.mockResolvedValue(settings())
  api.updateConcurrency.mockImplementation(async input => ({ ...settings(), ...input, source: 'database' }))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers() })

describe('intelligence concurrency settings', () => {
  it('loads when opened and saves each independent limit, then closes with a success message', async () => {
    const view = render(false)
    expect(api.concurrency).not.toHaveBeenCalled()
    await view.setProps({ show: true }); await flushPromises()
    expect((view.get(pelican).element as HTMLInputElement).value).toBe('8')
    expect(view.get('[data-testid="pelican-running"]').text()).toBe('6')
    expect(view.get('[data-testid="pelican-pending"]').text()).toBe('12')
    await view.get('[data-testid="concurrency-pelican-16"]').trigger('click')
    await view.get('[data-testid="concurrency-candy-8"]').trigger('click')
    await view.get('[data-testid="save-concurrency"]').trigger('click'); await flushPromises()
    expect(api.updateConcurrency).toHaveBeenCalledWith({ max_concurrency: 16, candy_max_concurrency: 8 })
    expect(api.showSuccess).toHaveBeenCalledWith('intelligenceMonitor.concurrency.saved')
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('rejects blank, fractional and out-of-range values without writing them', async () => {
    const view = render(); await flushPromises()
    for (const [selector, value] of [[pelican, ''], [pelican, '1.5'], [pelican, '0'], [pelican, '257'], [candy, '129']]) {
      await view.get(pelican).setValue(8)
      await view.get(candy).setValue(4)
      await view.get(selector).setValue(value)
      expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeDefined()
      expect(view.get(selector).attributes('aria-invalid')).toBe('true')
    }
    expect(api.updateConcurrency).not.toHaveBeenCalled()
  })

  it('accepts and saves the highest supported limits', async () => {
    const view = render(); await flushPromises()
    expect(view.get(pelican).attributes('max')).toBe('256')
    expect(view.get(candy).attributes('max')).toBe('128')
    await view.get(pelican).setValue(256)
    await view.get(candy).setValue(128)
    expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeUndefined()
    await view.get('[data-testid="save-concurrency"]').trigger('click'); await flushPromises()
    expect(api.updateConcurrency).toHaveBeenCalledWith({ max_concurrency: 256, candy_max_concurrency: 128 })
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('refreshes running and queued counts without overwriting unsaved input', async () => {
    const view = render(); await flushPromises()
    await view.get(pelican).setValue(24)
    api.concurrency.mockResolvedValue({ ...settings(), max_concurrency: 16, pelican_running: 8, pelican_pending: 3 })
    await view.get('[data-testid="refresh-concurrency"]').trigger('click'); await flushPromises()
    expect((view.get(pelican).element as HTMLInputElement).value).toBe('24')
    expect(view.get('[data-testid="pelican-running"]').text()).toBe('8')
    expect(view.get('[data-testid="pelican-pending"]').text()).toBe('3')
  })

  it('automatically refreshes counts while visible, preserving edits, and pauses when closed or hidden', async () => {
    const hidden = vi.spyOn(document, 'hidden', 'get')
    const view = render(); await flushPromises()
    await view.get(candy).setValue(64)
    api.concurrency.mockResolvedValue({ ...settings(), candy_running: 12, candy_pending: 0 })
    await vi.advanceTimersByTimeAsync(2000)
    expect(api.concurrency).toHaveBeenCalledTimes(2)
    expect(view.get('[data-testid="candy-running"]').text()).toBe('12')
    expect((view.get(candy).element as HTMLInputElement).value).toBe('64')
    hidden.mockReturnValue(true); document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.concurrency).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(false); document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(api.concurrency).toHaveBeenCalledTimes(3)
    await view.setProps({ show: false }); await vi.advanceTimersByTimeAsync(10000)
    expect(api.concurrency).toHaveBeenCalledTimes(3)
  })

  it('allows saving during a slow status poll and ignores the superseded response', async () => {
    const view = render(); await flushPromises()
    let finishPoll!: (value: IntelligenceConcurrency) => void
    api.concurrency.mockReturnValueOnce(new Promise(resolve => { finishPoll = resolve }))
    await vi.advanceTimersByTimeAsync(2000)
    const signal = api.concurrency.mock.calls[1][0] as AbortSignal
    await view.get(candy).setValue(64)
    expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeUndefined()
    await view.get('[data-testid="save-concurrency"]').trigger('click'); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(api.updateConcurrency).toHaveBeenCalledWith({ max_concurrency: 8, candy_max_concurrency: 64 })
    finishPoll({ ...settings(), candy_running: 99 }); await flushPromises()
    expect(view.get('[data-testid="candy-running"]').text()).toBe('2')
    expect(view.emitted('close')).toHaveLength(1)
  })

  it('blocks saving during loading, supports retry and keeps server validation errors visible', async () => {
    api.concurrency.mockRejectedValueOnce({ message: 'Load unavailable' })
    const view = render()
    expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeDefined()
    await flushPromises()
    expect(view.text()).toContain('Load unavailable')
    await view.get('[role="alert"] button').trigger('click'); await flushPromises()
    api.updateConcurrency.mockRejectedValueOnce({ message: 'Write unavailable' })
    await view.get('[data-testid="save-concurrency"]').trigger('click'); await flushPromises()
    expect(view.text()).toContain('Write unavailable')
    expect(view.emitted('close')).toBeUndefined()
    expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeUndefined()
  })

  it('ignores stale requests after closing and reopening', async () => {
    let resolveOld!: (value: IntelligenceConcurrency) => void
    api.concurrency.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const view = render()
    const oldSignal = api.concurrency.mock.calls[0][0] as AbortSignal
    await view.setProps({ show: false })
    api.concurrency.mockResolvedValueOnce({ ...settings(), max_concurrency: 20 })
    await view.setProps({ show: true }); await flushPromises()
    resolveOld(settings()); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect((view.get(pelican).element as HTMLInputElement).value).toBe('20')
  })

  it('prevents a second save and closing while a write is in progress', async () => {
    let finishSave!: (value: IntelligenceConcurrency) => void
    api.updateConcurrency.mockReturnValueOnce(new Promise(resolve => { finishSave = resolve }))
    const view = render(); await flushPromises()
    await view.get('[data-testid="save-concurrency"]').trigger('click')
    expect(view.get('[data-testid="save-concurrency"]').attributes('disabled')).toBeDefined()
    expect(view.get(pelican).attributes('disabled')).toBeDefined()
    view.getComponent(dialog).vm.$emit('close'); await flushPromises()
    expect(view.emitted('close')).toBeUndefined()
    finishSave(settings()); await flushPromises()
    expect(api.updateConcurrency).toHaveBeenCalledTimes(1)
    expect(view.emitted('close')).toHaveLength(1)
  })
})
