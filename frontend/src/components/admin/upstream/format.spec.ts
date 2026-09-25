import { afterEach, describe, expect, it, vi } from 'vitest'
import type { UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
import { amount, availability, availabilityColor, compactTokens, latencyColor, money, overallTargetStatus, recentHistory, targetStatus } from './format'

function record(id: number, checked_at: string, status: UpstreamHistoryRecord['status'] = 'operational'): UpstreamHistoryRecord {
  return { id, checked_at, status, target_id: 1, model: 'test-model', latency_ms: 800, ping_latency_ms: null, http_status: 200, message: '', cost: null, cost_source: 'unknown' }
}
function target(): UpstreamTarget {
  return { enabled: true, models: ['test-model'], interval_seconds: 300, timeout_seconds: 45, statistics: [{ model: 'test-model', status: 'operational', last_checked_at: '2026-09-23T09:59:00Z', timeline: [] }] } as unknown as UpstreamTarget
}

describe('upstream monitoring display invariants', () => {
  afterEach(() => vi.useRealTimers())
  it('keeps missing samples neutral and displays the most recent records chronologically without mutating the API response', () => {
    const records = [record(2, '2026-09-23T10:05:00Z', 'degraded'), record(1, '2026-09-23T10:00:00Z', 'error')]
    const bars = recentHistory(records)
    expect(bars).toHaveLength(60)
    expect(bars.slice(0, 58).every(value => value === null)).toBe(true)
    expect(bars.slice(-2).map(value => value?.status)).toEqual(['error', 'degraded'])
    expect(records.map(value => value.id)).toEqual([2, 1])
    expect(recentHistory().every(value => value === null)).toBe(true)
  })
  it('retains only the latest 60 records, including failures', () => {
    const records = Array.from({ length: 65 }, (_, index) => record(index, new Date(Date.UTC(2026, 8, 23, 10, index)).toISOString(), index === 64 ? 'error' : 'operational'))
    const bars = recentHistory(records)
    expect(bars[0]?.id).toBe(5)
    expect(bars[59]?.status).toBe('error')
  })
  it('does not describe paused, never checked, or expired data as currently healthy', () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-23T10:00:00Z'))
    const item = target()
    expect(targetStatus(item)).toBe('operational')
    item.enabled = false
    expect(targetStatus(item)).toBe('paused')
    item.enabled = true
    expect(targetStatus(item, 'unseen-model')).toBe('unknown')
    vi.setSystemTime(new Date('2026-09-23T10:20:00Z'))
    expect(targetStatus(item)).toBe('stale')
  })
  it('does not turn unknown balances or unmeasured availability into zero', () => {
    expect(money(null)).toBe('—')
    expect(money(undefined)).toBe('—')
    expect(availability(null)).toBe('—')
    expect(availability(0)).toBe('0.00%')
    expect(money(0)).not.toBe('—')
    expect(money(1234567, 'QUOTA')).toBe('1,234,567 QUOTA')
    expect(money(0, 'QUOTA')).toBe('0 QUOTA')
  })
  it('does not hide another model’s failure behind a healthy primary model', () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-23T10:00:00Z'))
    const item = target()
    item.models.push('second-model')
    item.statistics.push({ ...item.statistics[0]!, model: 'second-model', status: 'failed' })
    expect(targetStatus(item, 'test-model')).toBe('operational')
    expect(overallTargetStatus(item)).toBe('failed')
  })
  it('colors metrics using their value and keeps missing measurements neutral', () => {
    expect(availabilityColor(100)).toBe('hsl(120 72% 42%)')
    expect(availabilityColor(0)).toBe('hsl(0 72% 42%)')
    expect(availabilityColor(null)).toBeUndefined()
    expect(latencyColor(5999, 6000, 45)).toContain('emerald')
    expect(latencyColor(6000, 6000, 45)).toContain('amber')
    expect(latencyColor(45000, 6000, 45)).toContain('rose')
    expect(latencyColor(null, 6000, 45)).toContain('gray')
    expect(latencyColor(158, 6000, 45, 'error')).toContain('rose')
    expect(compactTokens(null)).toBe('—')
    expect(compactTokens(0)).toBe('0.00M')
    expect(compactTokens(1250000)).toBe('1.25M')
  })
  it('converts token totals to M and B without hiding small nonzero usage', () => {
    expect(compactTokens(1)).toBe('0.000001M')
    expect(compactTokens(1000000000)).toBe('1.00B')
    expect(compactTokens(1250000000)).toBe('1.25B')
    expect(compactTokens(999000000)).toBe('999.00M')
    expect(compactTokens(Number.NaN)).toBe('—')
  })
  it('formats numeric charges without currency prefixes and preserves unknown and small costs', () => {
    expect(amount(12.5)).toBe('12.50')
    expect(amount(-1.25)).toBe('-1.25')
    expect(amount(0.000123)).toBe('0.000123')
    expect(amount(null)).toBe('—')
  })
})
