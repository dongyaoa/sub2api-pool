import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { intelligenceMonitorAPI } from '@/api/admin/intelligenceMonitor'

beforeEach(() => { vi.resetAllMocks(); client.get.mockResolvedValue({ data: { items: [] } }) })

describe('account monitor list contract', () => {
  it('scopes the shared endpoint by account ID and forwards cancellation without fetching all plans', async () => {
    const signal = new AbortController().signal
    await expect(intelligenceMonitorAPI.plansForOAuthAccount(42, signal)).resolves.toEqual({ items: [] })
    expect(client.get).toHaveBeenCalledTimes(1)
    expect(client.get).toHaveBeenCalledWith('/admin/intelligence-monitors/plans', { signal, params: { account_id: 42 } })
  })
})
