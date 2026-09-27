import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { intelligenceMonitorAPI } from '@/api/admin/intelligenceMonitor'
beforeEach(() => { vi.resetAllMocks(); client.post.mockResolvedValue({ data: { id: 12, test_kind: 'candy' } }); client.get.mockResolvedValue({ data: { items: [] } }) })

describe('candy API contract', () => {
  it('uses a separate explicit manual-run endpoint', async () => {
    await expect(intelligenceMonitorAPI.runCandy(4)).resolves.toEqual({ id: 12, test_kind: 'candy' })
    expect(client.post).toHaveBeenCalledWith('/admin/intelligence-monitors/plans/4/candy/run')
  })
  it('preserves the existing pelican history request and adds a candy filter only when requested', async () => {
    const signal = new AbortController().signal
    await intelligenceMonitorAPI.runs(4, 1, signal)
    await intelligenceMonitorAPI.runs(4, 2, signal, 'candy')
    expect(client.get).toHaveBeenNthCalledWith(1, '/admin/intelligence-monitors/runs', { params: { plan_id: 4, page: 1, page_size: 12 }, signal })
    expect(client.get).toHaveBeenNthCalledWith(2, '/admin/intelligence-monitors/runs', { params: { plan_id: 4, page: 2, page_size: 12, test_kind: 'candy' }, signal })
  })
  it('sends explicit opt-in or opt-out without running a generation', async () => {
    client.put.mockResolvedValue({ data: { id: 4, candy_enabled: false } })
    await intelligenceMonitorAPI.update(4, { candy_enabled: true })
    await intelligenceMonitorAPI.update(4, { candy_enabled: false })
    expect(client.put).toHaveBeenNthCalledWith(1, '/admin/intelligence-monitors/plans/4', { candy_enabled: true })
    expect(client.put).toHaveBeenNthCalledWith(2, '/admin/intelligence-monitors/plans/4', { candy_enabled: false })
    expect(client.post).not.toHaveBeenCalled()
  })
})
