import type { UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
export { hslForPct as availabilityColor } from '@/composables/useChannelMonitorFormat'

export function money(value: number | null | undefined, currency = 'USD'): string {
  if (value == null || !Number.isFinite(value)) return '—'
  if (currency === 'QUOTA') return `${value.toLocaleString(undefined, { maximumFractionDigits: 6 })} QUOTA`
  try { return new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'USD', minimumFractionDigits: 2, maximumFractionDigits: Math.abs(value) > 0 && Math.abs(value) < 0.01 ? 6 : 2 }).format(value) }
  catch { return `${value.toFixed(2)} ${currency}` }
}
export function amount(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: Math.abs(value) > 0 && Math.abs(value) < 0.01 ? 6 : 2 })
}
export function latency(value: number | null | undefined): string { return value == null || !Number.isFinite(value) ? '—' : `${Math.round(value).toLocaleString()} ms` }
export function availability(value: number | null | undefined): string { return value == null || !Number.isFinite(value) ? '—' : `${value.toFixed(2)}%` }
export function compactTokens(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  const billions = Math.abs(value) >= 1_000_000_000
  const scaled = value / (billions ? 1_000_000_000 : 1_000_000)
  const digits = value !== 0 && Math.abs(scaled) < 0.01 ? 6 : 2
  return `${scaled.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: digits })}${billions ? 'B' : 'M'}`
}
export function latencyColor(value: number | null | undefined, threshold: number, timeoutSeconds: number, status?: string): string {
  if (value == null || !Number.isFinite(value)) return 'text-gray-400 dark:text-dark-400'
  if (status === 'error' || status === 'failed') return 'text-rose-600 dark:text-rose-400'
  if (value >= timeoutSeconds * 1000) return 'text-rose-600 dark:text-rose-400'
  if (value >= threshold) return 'text-amber-600 dark:text-amber-400'
  return 'text-emerald-600 dark:text-emerald-400'
}
export function dateTime(value: string | null | undefined): string { return value ? new Date(value).toLocaleString() : '—' }
export function shortTime(value: string | null | undefined): string { return value ? new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—' }
export function domain(value: string): string { try { return new URL(value).host } catch { return value } }
export function recentHistory(records: UpstreamHistoryRecord[] = []): Array<UpstreamHistoryRecord | null> {
  const sorted = [...records].sort((a, b) => Date.parse(a.checked_at) - Date.parse(b.checked_at) || a.id - b.id).slice(-60)
  return [...Array<null>(Math.max(0, 60 - sorted.length)).fill(null), ...sorted]
}
export function targetStatus(target: UpstreamTarget, model?: string): string {
  if (!target.enabled) return 'paused'
  const stats = target.statistics?.find(item => item.model === (model || target.models?.[0]))
  if (!stats?.last_checked_at) return 'unknown'
  if (Date.now() - Date.parse(stats.last_checked_at) > (target.interval_seconds * 2 + target.timeout_seconds) * 1000) return 'stale'
  return stats.status || 'unknown'
}
export function overallTargetStatus(target: UpstreamTarget): string {
  if (!target.enabled) return 'paused'
  const statuses = (target.models || []).map(model => targetStatus(target, model))
  return ['error', 'failed', 'degraded', 'stale', 'unknown', 'operational'].find(status => statuses.includes(status)) || 'unknown'
}
export const statusClasses: Record<string, string> = {
  operational: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400',
  degraded: 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400',
  failed: 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-400',
  error: 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-400',
  unknown: 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400',
  paused: 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400',
  stale: 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400',
}
