import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get, post } }))
import { checkUpdates, getUpdateStatus, getVersion, performUpdate, systemAPI } from '@/api/admin/system'

beforeEach(() => { get.mockReset(); post.mockReset() })
describe('Pool container update API', () => {
  it('force-checks the backend release registry with a bounded timeout', async () => {
    get.mockResolvedValue({ data: { current_version: '0.2.7-pool.4' } })
    await checkUpdates(true)
    expect(get).toHaveBeenCalledWith('/admin/system/check-updates', { params: { force: 'true' }, timeout: 30000 })
  })
  it('submits a pinned target quickly and retains compatibility exports', async () => {
    const target = { version: '0.2.7-pool.5', digest: 'sha256:' + 'a'.repeat(64) }
    post.mockResolvedValue({ data: { message: 'queued', need_restart: false, job: { id: 'job-1' } } })
    const result = await performUpdate(target)
    expect(post).toHaveBeenCalledWith('/admin/system/update', target, {
      headers: { 'Idempotency-Key': expect.stringMatching(/^pool-image-update-[A-Za-z0-9-]+$/) },
      timeout: 65000
    })
    expect(result.job?.id).toBe('job-1')
    expect(systemAPI.rollback).toBeTypeOf('function')
    expect(systemAPI.restartService).toBeTypeOf('function')
  })
  it('uses a fresh idempotency key for each explicit attempt without retrying rejected requests', async () => {
    const target = { version: '0.2.7-pool.5', digest: 'sha256:' + 'a'.repeat(64) }
    post.mockRejectedValueOnce({ status: 0 }).mockResolvedValueOnce({ data: { message: 'queued', need_restart: false } })
    await expect(performUpdate(target)).rejects.toEqual({ status: 0 })
    expect(post).toHaveBeenCalledTimes(1)
    const firstKey = post.mock.calls[0][2].headers['Idempotency-Key']
    await performUpdate(target)
    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[1][2].headers['Idempotency-Key']).not.toBe(firstKey)
  })
  it('reads persistent job status and the live binary revision', async () => {
    get.mockResolvedValueOnce({ data: { available: true, job: { state: 'checking' } } })
      .mockResolvedValueOnce({ data: { version: '0.2.7-pool.5', revision: 'b'.repeat(40) } })
    expect((await getUpdateStatus()).job?.state).toBe('checking')
    expect((await getVersion()).revision).toBe('b'.repeat(40))
    expect(get).toHaveBeenNthCalledWith(1, '/admin/system/update-status', { timeout: 10000 })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/system/version', { timeout: 10000 })
  })
})
