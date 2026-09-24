import { describe, expect, it } from 'vitest'
import { intelligenceDurationLabel, intelligenceDurationMs } from './intelligenceDuration'

const timestamps = { started_at: '2026-09-24T00:05:00Z', finished_at: '2026-09-24T00:17:34Z' }
const translate = (key: string, values: Record<string, number | string>) => key.endsWith('durationMinutesSeconds')
  ? `${values.minutes} 分 ${values.seconds} 秒`
  : `${values.count} 秒`

describe('intelligence execution duration', () => {
  it('prefers the measured server duration, including zero, over execution timestamps', () => {
    expect(intelligenceDurationMs({ ...timestamps, duration_ms: 12000 })).toBe(12000)
    expect(intelligenceDurationMs({ ...timestamps, duration_ms: 0 })).toBe(0)
  })

  it('falls back to execution timestamps without including queue time', () => {
    const run = { ...timestamps, duration_ms: null, created_at: '2026-09-24T00:00:00Z' }
    expect(intelligenceDurationMs(run)).toBe(754000)
    expect(intelligenceDurationLabel(run, translate)).toBe('12 分 34 秒')
  })

  it.each([NaN, Infinity, -1])('ignores an invalid measured duration (%s)', duration_ms => {
    expect(intelligenceDurationMs({ ...timestamps, duration_ms })).toBe(754000)
  })

  it('does not invent a duration when timestamps are missing, invalid, or reversed', () => {
    expect(intelligenceDurationMs(null)).toBeNull()
    expect(intelligenceDurationMs(undefined)).toBeNull()
    expect(intelligenceDurationMs({ duration_ms: null, started_at: null, finished_at: timestamps.finished_at })).toBeNull()
    expect(intelligenceDurationMs({ ...timestamps, duration_ms: null, started_at: 'invalid' })).toBeNull()
    expect(intelligenceDurationMs({ duration_ms: null, started_at: timestamps.finished_at, finished_at: timestamps.started_at })).toBeNull()
  })

  it.each([
    [0, '0 秒'],
    [1250, '1.3 秒'],
    [59999, '1 分 0 秒'],
    [60000, '1 分 0 秒'],
    [754000, '12 分 34 秒'],
    [900000, '15 分 0 秒'],
  ])('formats %i milliseconds into a readable duration', (duration_ms, expected) => {
    expect(intelligenceDurationLabel({ ...timestamps, duration_ms }, translate)).toBe(expected)
  })
})
