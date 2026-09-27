import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('keeps parent scroll locked and focused while Escape closes only the top dialog', async () => {
    const parent = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Parent' }, slots: { default: '<button data-open-child>Open works</button>' } })
    await nextTick()
    const trigger = document.body.querySelector<HTMLButtonElement>('[data-open-child]')!
    trigger.focus()
    const child = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Works' } })
    const hidden = mount(BaseDialog, { attachTo: document.body, props: { show: false, title: 'Settings' } })
    await nextTick()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.body.querySelectorAll<HTMLElement>('.modal-overlay')[1].style.zIndex).toBe('60')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(child.emitted('close')).toHaveLength(1)
    expect(parent.emitted('close')).toBeUndefined()
    await child.setProps({ show: false })
    hidden.unmount()
    child.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.activeElement).toBe(trigger)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(parent.emitted('close')).toHaveLength(1)
    parent.unmount()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})
