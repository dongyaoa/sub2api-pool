import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'

type DurationRun = Pick<IntelligenceRun, 'duration_ms' | 'started_at' | 'finished_at'>

// Prefer the server's measured execution duration. Legacy records can fall
// back to execution timestamps; queued time must never count as generation.
export function intelligenceDurationMs(run: DurationRun | null | undefined): number | null {
  if (!run) return null
  if (typeof run.duration_ms === 'number' && Number.isFinite(run.duration_ms) && run.duration_ms >= 0) return run.duration_ms
  if (!run.started_at || !run.finished_at) return null
  const elapsed = Date.parse(run.finished_at) - Date.parse(run.started_at)
  return Number.isFinite(elapsed) && elapsed >= 0 ? elapsed : null
}

type DurationTranslator = (key: string, values: Record<string, number | string>) => string

export function intelligenceDurationLabel(run: DurationRun | null | undefined, t: DurationTranslator): string | null {
  const milliseconds = intelligenceDurationMs(run)
  if (milliseconds === null) return null
  const roundedSeconds = Number((milliseconds / 1000).toFixed(1))
  if (roundedSeconds < 60) return t('intelligenceMonitor.seconds', { count: roundedSeconds })
  const seconds = Math.floor(roundedSeconds)
  return t('intelligenceMonitor.durationMinutesSeconds', { minutes: Math.floor(seconds / 60), seconds: seconds % 60 })
}
