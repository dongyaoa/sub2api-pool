import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { pelicanMonitorAPI } from '@/api/pelicanMonitor'
beforeEach(() => { client.get.mockReset(); client.get.mockResolvedValue({ data: {} }) })
describe('read-only user pelican API', () => {
  it('uses only scoped user endpoints and exposes no mutations', async () => {
    const signal = new AbortController().signal
    await pelicanMonitorAPI.config(signal); await pelicanMonitorAPI.list(signal); await pelicanMonitorAPI.detail(9, signal)
    expect(client.get.mock.calls).toEqual([['/pelican-monitor/config', { signal }], ['/pelican-monitor', { signal }], ['/pelican-monitor/runs/9', { signal }]])
    expect(Object.keys(pelicanMonitorAPI).sort()).toEqual(['config', 'detail', 'list'])
  })
})
