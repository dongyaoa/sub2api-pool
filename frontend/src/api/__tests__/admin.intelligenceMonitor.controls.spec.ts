import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { intelligenceMonitorAPI } from '@/api/admin/intelligenceMonitor'

beforeEach(() => vi.resetAllMocks())

describe('monitor administration endpoints', () => {
  it('permanently deletes plans and individual artwork using their dedicated endpoints', async () => {
    await intelligenceMonitorAPI.permanentDelete(7)
    await intelligenceMonitorAPI.deleteRun(42)
    expect(client.delete.mock.calls).toEqual([
      ['/admin/intelligence-monitors/plans/7/permanent'],
      ['/admin/intelligence-monitors/runs/42'],
    ])
  })

  it('reads global scheduling state and explicitly stops or starts schedules without requesting runs', async () => {
    const signal = new AbortController().signal
    client.get.mockResolvedValue({ data: { total: 10, enabled: 5 } })
    client.put.mockResolvedValue({ data: { updated: 5, total: 10, enabled: 0 } })
    await expect(intelligenceMonitorAPI.scheduleStatus(signal)).resolves.toEqual({ total: 10, enabled: 5 })
    expect(client.get).toHaveBeenCalledWith('/admin/intelligence-monitors/plans/schedule-status', { signal })
    await expect(intelligenceMonitorAPI.setAllEnabled(false)).resolves.toEqual({ updated: 5, total: 10, enabled: 0 })
    await intelligenceMonitorAPI.setAllEnabled(true)
    expect(client.put.mock.calls).toEqual([
      ['/admin/intelligence-monitors/plans/enabled', { enabled: false }],
      ['/admin/intelligence-monitors/plans/enabled', { enabled: true }],
    ])
  })
})
