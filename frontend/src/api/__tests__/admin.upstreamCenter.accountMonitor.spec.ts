import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { upstreamCenterAPI } from '@/api/admin/upstreamCenter'
beforeEach(() => { vi.clearAllMocks() })

describe('account upstream monitoring API', () => {
  it('reads the account projection without sending credentials or creating a target', async () => {
    const signal = new AbortController().signal
    const snapshot = { account_id: 42, account_name: 'Relay', provider: 'openai', pelican_supported: true, target: null, supplier: null }
    client.get.mockResolvedValue({ data: snapshot })
    await expect(upstreamCenterAPI.accountMonitor(42, signal)).resolves.toEqual(snapshot)
    expect(client.get).toHaveBeenCalledWith('/admin/upstream-center/accounts/42/monitor', { signal })
    expect(client.post).not.toHaveBeenCalled()
  })
  it('ensures a shared target with only the account identity and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const snapshot = { account_id: 42, target: { id: 17 } }
    client.post.mockResolvedValue({ data: snapshot })
    await expect(upstreamCenterAPI.ensureAccountMonitor(42, signal)).resolves.toEqual(snapshot)
    expect(client.post).toHaveBeenCalledWith('/admin/upstream-center/accounts/42/monitor', undefined, { signal })
  })
})
