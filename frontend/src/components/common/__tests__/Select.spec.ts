import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shrinks the minimum width to fit near the right viewport edge', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('220px')
    expect(dropdown?.style.minWidth).toBe('92px')
    expect(dropdown?.style.maxWidth).toBe('92px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('clamps an offscreen-right trigger position to the viewport boundary', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('312px')
    expect(dropdown?.style.minWidth).toBe('0px')
    expect(dropdown?.style.maxWidth).toBe('0px')
  })
})

describe('Select custom option filtering', () => {
  it('combines a custom filter with search without losing the selected label', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: 'claude',
        searchable: true,
        options: [
          { value: 'gpt', label: 'GPT Standard', platform: 'openai' },
          { value: 'gpt-mini', label: 'GPT Mini', platform: 'openai' },
          { value: 'claude', label: 'Claude Standard', platform: 'anthropic' },
        ],
        filterOption: (option) => option.platform === 'openai',
      },
      slots: {
        'after-search': '<div data-test="filter-header">Platform filters</div>',
      },
    })
    unmountWrapper = () => wrapper.unmount()

    expect(wrapper.get('.select-value').text()).toBe('Claude Standard')

    await wrapper.get('button').trigger('click')
    await nextTick()

    expect(document.body.querySelector('[data-test="filter-header"]')?.textContent).toBe(
      'Platform filters'
    )

    const searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')
    expect(searchInput).not.toBeNull()
    searchInput!.value = 'mini'
    searchInput!.dispatchEvent(new Event('input'))
    await nextTick()

    const labels = Array.from(document.body.querySelectorAll('.select-option-label')).map(
      (element) => element.textContent
    )
    expect(labels).toEqual(['GPT Mini'])
  })
})

describe('Select dropdown keyboard actions', () => {
  it('closes only the dropdown when Escape is pressed inside a modal', async () => {
    const closeModal = vi.fn()
    document.addEventListener('keydown', closeModal)
    try {
      const wrapper = mount(Select, {
        attachTo: document.body,
        props: { modelValue: null, searchable: true, options: [{ value: 1, label: 'Proxy 1' }] },
      })
      unmountWrapper = () => wrapper.unmount()
      await wrapper.get('button').trigger('click')
      await nextTick()
      document.body.querySelector<HTMLInputElement>('.select-search-input')!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
      )
      await nextTick()
      expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
      expect(closeModal).not.toHaveBeenCalled()
      expect(document.activeElement).toBe(wrapper.get('button').element)
    } finally {
      document.removeEventListener('keydown', closeModal)
    }
  })

  it('tabs through custom actions before closing and restores the trigger as the tab anchor', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: { modelValue: null, searchable: true, options: [{ value: 1, label: 'Proxy 1' }] },
      slots: {
        'after-search': '<button data-test="batch-test">Test visible</button>',
        option: '<button data-test="disabled-test" disabled>Unavailable</button><button data-test="proxy-test">Test proxy</button>'
      }
    })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('button')
    await trigger.trigger('click')
    await nextTick()
    const search = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    const batch = document.body.querySelector<HTMLButtonElement>('[data-test="batch-test"]')!
    const proxy = document.body.querySelector<HTMLButtonElement>('[data-test="proxy-test"]')!
    expect(document.activeElement).toBe(search)

    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.activeElement).toBe(batch)
    expect(trigger.attributes('aria-expanded')).toBe('true')
    batch.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.activeElement).toBe(proxy)
    proxy.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.activeElement).toBe(batch)
    batch.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    proxy.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })

  it.each([false, true])('still closes a plain searchable dropdown with shiftKey=%s', async shiftKey => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: { modelValue: null, searchable: true, options: [{ value: 1, label: 'One' }] }
    })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('button')
    await trigger.trigger('click')
    await nextTick()
    document.body.querySelector<HTMLInputElement>('.select-search-input')!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true })
    )
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
    })
    unmountWrapper = () => wrapper.unmount()
    return wrapper
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        searchable: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})
