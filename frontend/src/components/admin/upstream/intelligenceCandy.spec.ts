import { describe, expect, it } from 'vitest'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import { candyHistory, candyResult, isIntelligencePlanActive } from './intelligenceCandy'

const run = (id: number, fields: Partial<IntelligenceRun> = {}) => ({ id, test_kind: 'candy', status: 'succeeded', correct: true, ...fields }) as IntelligenceRun
const plan = (fields: Partial<IntelligencePlan> = {}) => ({ latest_run: null, ...fields }) as IntelligencePlan

describe('candy record state', () => {
  it.each([
    [{ correct: true }, 'correct'], [{ correct: false }, 'incorrect'], [{ correct: null }, 'unknown'],
    [{ status: 'failed', correct: true }, 'failed'], [{ status: 'pending', correct: false }, 'pending'],
    [{ status: 'running', correct: true }, 'running'],
  ] as const)('uses the server verdict without treating HTTP success as a correct answer: %o', (fields, expected) => {
    expect(candyResult(run(1, fields))).toBe(expected)
  })

  it('preserves newest-first server order, deduplicates and caps completed history at sixty', () => {
    const records = Array.from({ length: 65 }, (_, index) => run(100 - index))
    const result = candyHistory(plan({ candy_recent_runs: [run(101, { test_kind: 'pelican' }), run(102, { status: 'pending' }), records[0]!, ...records] }))
    expect(result.map(item => item.id)).toEqual(records.slice(0, 60).map(item => item.id))
  })

  it('replaces stale same-id metadata and adds an active run without evicting completed records', () => {
    const recent = Array.from({ length: 60 }, (_, index) => run(60 - index))
    const active = run(61, { status: 'running', correct: null })
    expect(candyHistory(plan({ candy_latest_run: active, candy_recent_runs: recent }))).toEqual([active, ...recent])
    expect(candyHistory(plan({ candy_latest_run: active, candy_recent_runs: [run(61), ...recent] }))).toEqual([active, ...recent])
    const completed = run(61, { correct: false, answer: '20' })
    const result = candyHistory(plan({ candy_latest_run: completed, candy_recent_runs: [active, ...recent] }))
    expect(result[0]).toEqual(completed)
    expect(result).toHaveLength(60)
    expect(result.at(-1)?.id).toBe(2)
  })

  it('treats either independent test as active', () => {
    expect(isIntelligencePlanActive(plan({ latest_run: run(1, { test_kind: 'pelican', status: 'running' }) }))).toBe(true)
    expect(isIntelligencePlanActive(plan({ candy_latest_run: run(2, { status: 'pending' }) }))).toBe(true)
    expect(isIntelligencePlanActive(plan({ latest_run: run(1), candy_latest_run: run(2) }))).toBe(false)
  })
})
