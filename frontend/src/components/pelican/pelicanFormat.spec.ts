import { describe, expect, it } from 'vitest'
import type { PelicanMonitorGroup, PelicanRun } from '@/api/pelicanMonitor'
import { pelicanCountdown, pelicanDuration, pelicanInterval, pelicanWorks } from './pelicanFormat'
const run = (id: number, status: PelicanRun['status'] = 'succeeded') => ({ id, status }) as PelicanRun
describe('user pelican presentation', () => {
  it('retains twenty completed runs and shows the active run separately', () => {
    const recent = Array.from({ length: 25 }, (_, index) => run(30 - index))
    const works = pelicanWorks({ latest_run: run(31, 'running'), recent_runs: [run(31, 'pending'), ...recent] } as PelicanMonitorGroup)
    expect(works).toHaveLength(21)
    expect(works[0]?.status).toBe('running')
    expect(works.at(-1)?.id).toBe(11)
    expect(new Set(works.map(item => item.id)).size).toBe(21)
  })
  it('updates a completed latest record without keeping stale running metadata', () => {
    expect(pelicanWorks({ latest_run: run(31), recent_runs: [run(31, 'running'), run(30)] } as PelicanMonitorGroup)).toEqual([run(31), run(30)])
  })
  it('formats custom intervals, server-aligned countdowns and durations', () => {
    expect(pelicanInterval(31)).toEqual({ seconds: 31 })
    expect(pelicanInterval(300)).toEqual({ minutes: 5 })
    expect(pelicanCountdown('2026-09-30T12:05:00Z', Date.parse('2026-09-30T12:00:01Z'))).toBe('04:59')
    expect(pelicanCountdown('invalid', Date.now())).toBe(null)
    expect(pelicanDuration({ ...run(1), duration_ms: 125000 })).toBe('2m 5s')
  })
})
