import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'

export const isIntelligenceRunActive = (run?: IntelligenceRun | null) => !!run && (run.status === 'pending' || run.status === 'running')
export const isIntelligencePlanActive = (plan: IntelligencePlan) => isIntelligenceRunActive(plan.latest_run) || isIntelligenceRunActive(plan.candy_latest_run)
// One poll per visible panel, independent of its card count. Speed up while
// either test is running or is about to become due; keep idle reads lightweight.
export function intelligenceRefreshInterval(plans: IntelligencePlan[], now = Date.now()) {
  const imminent = (value?: string | null) => !!value && Date.parse(value) <= now + 5000
  return plans.some(plan => isIntelligencePlanActive(plan) || (plan.enabled && (
    imminent(plan.next_run_at) || (plan.candy_enabled && imminent(plan.candy_next_run_at))
  ))) ? 1000 : 5000
}
export function candyResult(run: IntelligenceRun) {
  if (isIntelligenceRunActive(run)) return run.status === 'pending' ? 'pending' : 'running'
  if (run.status === 'failed' || (run.http_status != null && run.http_status >= 400)) return 'failed'
  if (run.correct === false) return 'incorrect'
  return run.status === 'succeeded' && run.correct === true ? 'correct' : 'unknown'
}
export function candyAnswerResult(run: IntelligenceRun) {
  return run.correct === true ? 'correct' : run.correct === false ? 'incorrect' : 'unknown'
}
export function candyResultTone(run: IntelligenceRun) {
  const result = candyResult(run)
  if (result === 'correct') return 'success'
  if (['incorrect', 'failed'].includes(result)) return 'error'
  return ['pending', 'running'].includes(result) ? 'neutral' : 'warning'
}
export function candyHistory(plan: IntelligencePlan): IntelligenceRun[] {
  const latest = plan.candy_latest_run
  const recent = [...(plan.candy_recent_runs || [])]
  if (latest && !isIntelligenceRunActive(latest)) {
    const index = recent.findIndex(run => run.id === latest.id)
    if (index >= 0) recent[index] = latest
    else recent.unshift(latest)
  }
  const seen = new Set<number>()
  const completed = recent.filter(run => {
    if (run.test_kind === 'pelican' || isIntelligenceRunActive(run) || seen.has(run.id) || (isIntelligenceRunActive(latest) && run.id === latest?.id)) return false
    seen.add(run.id)
    return true
  }).slice(0, 60)
  return isIntelligenceRunActive(latest) ? [latest!, ...completed] : completed
}
