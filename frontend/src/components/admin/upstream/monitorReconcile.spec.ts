import { describe, expect, it } from 'vitest'
import { reconcileMonitorData } from './monitorReconcile'

describe('monitor polling structural sharing', () => {
  it('retains unchanged cards on reorder and updates a changed run without mutating previous data', () => {
    const first = { id: 1, latest_run: { id: 10, status: 'running' }, recent_runs: [{ id: 9, status: 'succeeded' }] }
    const second = { id: 2, latest_run: null, recent_runs: [] }
    const previous = [first, second]
    expect(reconcileMonitorData(previous, structuredClone(previous))).toBe(previous)
    const next = reconcileMonitorData(previous, [structuredClone(second), { ...structuredClone(first), latest_run: { id: 10, status: 'succeeded' } }])
    expect(next[0]).toBe(second)
    expect(next[1]).not.toBe(first)
    expect(next[1].recent_runs).toBe(first.recent_runs)
    expect(first.latest_run.status).toBe('running')
    expect(next[1].latest_run?.status).toBe('succeeded')
  })
})
