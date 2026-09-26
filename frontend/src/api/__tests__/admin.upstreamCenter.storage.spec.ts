import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { upstreamCenterAPI } from '@/api/admin/upstreamCenter'
beforeEach(() => { vi.clearAllMocks() })
describe('upstream storage API contract', () => {
  it('uses read-only endpoints for policy and archives, forwarding cancellation', async () => {
    const signal = new AbortController().signal
    const policy = { enabled: true, history_retention_days: 30, snapshot_retention_days: 7, last_cleanup_at: null, last_result: null }
    client.get.mockResolvedValueOnce({ data: policy }).mockResolvedValueOnce({ data: { items: [], total: 0 } })
    await expect(upstreamCenterAPI.storage(signal)).resolves.toEqual(policy)
    await expect(upstreamCenterAPI.archives(signal)).resolves.toEqual({ items: [], total: 0 })
    expect(client.get).toHaveBeenNthCalledWith(1, '/admin/upstream-center/storage', { signal })
    expect(client.get).toHaveBeenNthCalledWith(2, '/admin/upstream-center/storage/archives', { signal })
  })
  it('sends separate explicit policy, cleanup and name-confirmed purge actions', async () => {
    const input = { enabled: false, history_retention_days: 60, snapshot_retention_days: 30 }
    client.put.mockResolvedValue({ data: input }); client.post.mockResolvedValue({ data: { has_more: false } })
    await upstreamCenterAPI.updateStorage(input); await upstreamCenterAPI.cleanupStorage(); await upstreamCenterAPI.purge({ kind: 'supplier', id: 4, confirm_name: 'Example' })
    expect(client.put).toHaveBeenCalledWith('/admin/upstream-center/storage', input)
    expect(client.post).toHaveBeenNthCalledWith(1, '/admin/upstream-center/storage/cleanup', undefined, { timeout: 60000 })
    expect(client.post).toHaveBeenNthCalledWith(2, '/admin/upstream-center/storage/purge', { kind: 'supplier', id: 4, confirm_name: 'Example' }, { timeout: 60000 })
  })
})
