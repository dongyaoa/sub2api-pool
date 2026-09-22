import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OPENAI_REAUTH_POLL_INTERVAL, useOpenAIAutoReauth } from '../useOpenAIAutoReauth'
import type { OpenAIAutoReauthOverview } from '@/api/admin/openaiAutoReauth'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/admin/openaiAutoReauth', () => ({ openaiAutoReauthAPI: { list } }))
const result: OpenAIAutoReauthOverview = { worker_configured: true, encryption_key_configured: true, accounts: [] }
let scope = effectScope()
beforeEach(() => { vi.useFakeTimers(); list.mockReset(); list.mockResolvedValue(result); scope = effectScope() })
afterEach(() => { scope.stop(); vi.useRealTimers() })

describe('useOpenAIAutoReauth', () => {
  it('polls only while open, aborts on close, and stops on unmount', async () => {
    const active = ref(false)
    scope.run(() => useOpenAIAutoReauth(active))
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL * 2)
    expect(list).not.toHaveBeenCalled()
    active.value = true
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL)
    expect(list).toHaveBeenCalledTimes(2)
    const signal = list.mock.calls[1][0] as AbortSignal
    active.value = false
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL * 3)
    expect(list).toHaveBeenCalledTimes(2)
    active.value = true
    await flushPromises()
    scope.stop()
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL * 3)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('ignores stale in-flight responses across closing and reopening', async () => {
    let finish!: (value: OpenAIAutoReauthOverview) => void
    list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const active = ref(true)
    const state = scope.run(() => useOpenAIAutoReauth(active))!
    active.value = false
    active.value = true
    await flushPromises()
    finish({ ...result, worker_configured: false })
    await flushPromises()
    expect(state.overview.value?.worker_configured).toBe(true)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL)
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('retries transient failures without overlapping requests', async () => {
    let fail!: (reason: Error) => void
    list.mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject }))
    const state = scope.run(() => useOpenAIAutoReauth(ref(true)))!
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL * 3)
    expect(list).toHaveBeenCalledTimes(1)
    fail(new Error('connection failed'))
    await flushPromises()
    expect(state.loadFailed.value).toBe(true)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL)
    expect(state.loadFailed.value).toBe(false)
    expect(state.overview.value).toEqual(result)
  })

  it.each([
    [{ status: 404 }, 'endpoint_unavailable'],
    [{ code: 'OPENAI_REAUTH_ENDPOINT_UNAVAILABLE' }, 'endpoint_unavailable'],
    [{ status: 401 }, 'unauthorized'],
    [{ status: 403 }, 'forbidden'],
    [{ status: 503, message: 'auto_reauth_schema_unavailable' }, 'schema_unavailable']
  ])('stops automatic retries for permanent failure %j and permits explicit retry', async (error, expected) => {
    list.mockRejectedValueOnce(error)
    const state = scope.run(() => useOpenAIAutoReauth(ref(true)))!
    await flushPromises()
    expect(state.loadError.value).toBe(expected)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL * 5)
    expect(list).toHaveBeenCalledTimes(1)
    await state.refresh()
    expect(state.loadFailed.value).toBe(false)
  })

  it.each([
    [{ status: 503 }, 'server_error'],
    [{ status: 503, message: 'auto_reauth_status_unavailable' }, 'status_unavailable']
  ])('retries server errors %j with a safe reason', async (error, expected) => {
    list.mockRejectedValueOnce(error)
    const state = scope.run(() => useOpenAIAutoReauth(ref(true)))!
    await flushPromises()
    expect(state.loadError.value).toBe(expected)
    await vi.advanceTimersByTimeAsync(OPENAI_REAUTH_POLL_INTERVAL)
    expect(state.loadFailed.value).toBe(false)
  })
})
