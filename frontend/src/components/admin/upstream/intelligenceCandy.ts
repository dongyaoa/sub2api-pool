import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'

export const isIntelligenceRunActive = (run?: IntelligenceRun | null) => !!run && (run.status === 'pending' || run.status === 'running')
export const isIntelligencePlanActive = (plan: IntelligencePlan) => isIntelligenceRunActive(plan.latest_run) || isIntelligenceRunActive(plan.candy_latest_run)
export function candyResult(run: IntelligenceRun) {
  if (isIntelligenceRunActive(run)) return run.status === 'pending' ? 'pending' : 'running'
  if (run.status === 'failed') return 'failed'
  return run.correct === true ? 'correct' : run.correct === false ? 'incorrect' : 'unknown'
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
