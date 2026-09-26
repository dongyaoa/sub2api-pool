import { defineComponent, ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useMonitorRefresh } from '../useMonitorRefresh'

let view: VueWrapper | undefined
const request = vi.fn(), apply = vi.fn(), onError = vi.fn()
function deferred() {
  let resolve!: (value: number) => void
  const promise = new Promise<number>(r => { resolve = r })
  return { promise, resolve }
}
function render() {
  const active = ref(true), paused = ref(false), interval = ref(5000)
  let polling!: ReturnType<typeof useMonitorRefresh<number>>
  view = mount(defineComponent({
    setup() { polling = useMonitorRefresh({ request, apply, onError, active: () => active.value, paused: () => paused.value, intervalMs: () => interval.value }); return {} },
    template: '<div />',
  }))
  return { polling, active, paused, interval }
}
beforeEach(() => {
  vi.useFakeTimers(); vi.resetAllMocks()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  request.mockResolvedValue(1)
})
afterEach(() => { view?.unmount(); view = undefined; vi.restoreAllMocks(); vi.useRealTimers() })

describe('monitor refresh lifecycle', () => {
  it('waits for a slow background read and lets manual refresh supersede it without stale overwrite', async () => {
    const old = deferred(); request.mockReturnValueOnce(old.promise)
    const { polling } = render()
    const oldSignal = request.mock.calls[0][0] as AbortSignal
    await vi.advanceTimersByTimeAsync(20000)
    expect(request).toHaveBeenCalledTimes(1)
    expect(oldSignal.aborted).toBe(false)
    request.mockResolvedValue(2)
    await polling.refresh()
    expect(oldSignal.aborted).toBe(true)
    expect(apply).toHaveBeenLastCalledWith(2)
    old.resolve(1); await flushPromises()
    expect(apply).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(request).toHaveBeenCalledTimes(3)
  })

  it('stops while hidden or on an inactive tab and refreshes immediately on return', async () => {
    const hidden = vi.spyOn(document, 'hidden', 'get')
    const { active } = render(); await flushPromises()
    active.value = false; await flushPromises()
    await vi.advanceTimersByTimeAsync(15000)
    expect(request).toHaveBeenCalledTimes(1)
    active.value = true; await flushPromises()
    expect(request).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(true); document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(15000)
    expect(request).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(false); document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(request).toHaveBeenCalledTimes(3)
  })

  it('backs off failures, supports manual retry while paused, and changes the polling cadence', async () => {
    request.mockRejectedValueOnce(new Error('network'))
    const { polling, paused, interval } = render(); await flushPromises()
    expect(onError).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(request).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(request).toHaveBeenCalledTimes(2)
    paused.value = true
    await vi.advanceTimersByTimeAsync(10000)
    expect(request).toHaveBeenCalledTimes(2)
    await polling.refresh()
    expect(request).toHaveBeenCalledTimes(3)
    paused.value = false; interval.value = 2000; await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(request).toHaveBeenCalledTimes(4)
  })

  it('aborts on unmount and ignores the canceled result', async () => {
    const pending = deferred(); request.mockReturnValueOnce(pending.promise)
    render(); const signal = request.mock.calls[0][0] as AbortSignal
    view!.unmount(); view = undefined
    expect(signal.aborted).toBe(true)
    pending.resolve(5); await flushPromises()
    expect(apply).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
