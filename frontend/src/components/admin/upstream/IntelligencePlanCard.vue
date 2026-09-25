<template>
  <article class="grid min-w-0 overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm lg:grid-cols-[256px_minmax(0,1fr)] dark:border-dark-700 dark:bg-dark-800">
    <aside class="flex min-w-0 flex-col border-b border-gray-100 p-4 lg:border-b-0 lg:border-r dark:border-dark-700">
      <div class="flex items-start gap-2.5">
        <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400"><Icon :name="oauth ? 'shield' : 'server'" size="sm" /></div>
        <div class="min-w-0 flex-1"><h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white" :title="plan.name">{{ plan.name }}</h3><p class="mt-1 truncate text-[11px] text-gray-500 dark:text-dark-400" :title="siteName">{{ siteName }}</p></div>
        <span v-if="oauth" class="rounded-md border border-violet-200 bg-violet-50 px-1.5 py-0.5 text-[10px] font-semibold text-violet-600 dark:border-violet-500/20 dark:bg-violet-500/10 dark:text-violet-300">OAuth</span>
      </div>
      <p class="mt-2 truncate font-mono text-[10px] text-gray-400 dark:text-dark-400" :title="endpoint">{{ endpoint || '—' }}</p>
      <div class="mt-3 flex items-end justify-between gap-3">
        <div class="min-w-0"><p class="text-[10px] text-gray-400">{{ t(oauth ? 'intelligenceMonitor.oauth.account' : 'intelligenceMonitor.group') }}</p><p class="mt-1 truncate text-xs font-medium text-gray-700 dark:text-gray-200" :title="groupName">{{ groupName }}</p></div>
        <div v-if="!oauth" class="shrink-0 text-right"><p class="text-[10px] text-gray-400">{{ t('intelligenceMonitor.rate') }}</p><p class="mt-0.5 text-lg font-semibold tabular-nums" :class="rate?.stale ? 'text-amber-500' : 'text-primary-600 dark:text-primary-400'">{{ intelligenceRateLabel(rate) || '—' }}</p></div>
      </div>
      <p v-if="plan.rate_note || plan.notes" class="mt-2 line-clamp-1 text-[10px] text-gray-400" :title="[plan.rate_note, plan.notes].filter(Boolean).join(' · ')">{{ [plan.rate_note, plan.notes].filter(Boolean).join(' · ') }}</p>
      <div class="mt-3 flex items-center justify-between gap-2 text-[10px]"><span class="inline-flex items-center gap-1" :class="plan.enabled ? 'text-primary-600 dark:text-primary-400' : 'text-gray-400'"><Icon name="clock" size="xs" />{{ plan.enabled ? intervalLabel : t('intelligenceMonitor.manual') }}</span><span class="rounded-md px-1.5 py-0.5 font-medium" :class="statusClass(plan.latest_run?.status)">{{ t(`intelligenceMonitor.status.${plan.latest_run?.status || 'idle'}`) }}</span></div>
      <div v-if="plan.enabled" class="mt-2 flex min-h-5 items-center justify-between gap-2 text-[10px]" data-testid="intelligence-schedule">
        <span class="shrink-0 text-gray-400 dark:text-dark-400">{{ t('intelligenceMonitor.nextCheck') }}</span>
        <span v-if="active" class="text-right text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.afterCurrentRun') }}</span>
        <time v-else-if="remainingSeconds !== null && remainingSeconds > 0" :datetime="plan.next_run_at || undefined" :title="dateTime(plan.next_run_at)" class="font-mono text-xs font-semibold tabular-nums text-primary-600 dark:text-primary-400" data-testid="intelligence-countdown">{{ countdownLabel }}</time>
        <span v-else class="text-amber-600 dark:text-amber-400" data-testid="intelligence-waiting-schedule">{{ t('intelligenceMonitor.waitingSchedule') }}</span>
      </div>
      <div class="mt-3 flex items-center justify-between gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
        <button type="button" class="inline-flex items-center gap-1 text-xs font-semibold text-primary-600 disabled:opacity-40 dark:text-primary-400" :disabled="busy || active" @click="emit('run')"><Icon :name="active ? 'clock' : 'play'" size="xs" />{{ t(active ? `intelligenceMonitor.status.${plan.latest_run?.status}` : 'intelligenceMonitor.run') }}</button>
        <div class="flex items-center gap-0.5"><button class="action" :disabled="busy" :title="t(plan.enabled ? 'intelligenceMonitor.pause' : 'intelligenceMonitor.resume')" :aria-label="t(plan.enabled ? 'intelligenceMonitor.pause' : 'intelligenceMonitor.resume')" @click="emit('toggle')"><Icon :name="plan.enabled ? 'clock' : 'play'" size="sm" /></button><button class="action" :disabled="busy || active" :title="t('intelligenceMonitor.edit')" :aria-label="t('intelligenceMonitor.edit')" @click="emit('edit')"><Icon name="edit" size="sm" /></button><button class="action hover:!text-rose-500" :disabled="busy || active" :title="t('intelligenceMonitor.archive')" :aria-label="t('intelligenceMonitor.archive')" @click="emit('archive')"><Icon name="trash" size="sm" /></button></div>
      </div>
    </aside>
    <section class="flex min-w-0 flex-col px-4 py-3">
      <div class="mb-3 flex shrink-0 flex-wrap items-center justify-between gap-2"><div class="flex items-center gap-2"><span class="text-xs font-medium text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.recentWorks') }}</span><span class="text-[10px] text-gray-400">{{ t('intelligenceMonitor.newestFirst') }}</span></div><button type="button" class="text-[11px] text-primary-600 dark:text-primary-400" @click="emit('history')">{{ t('intelligenceMonitor.history') }}<span class="ml-1 text-gray-400">{{ completedWorks.length }}/20</span></button></div>
      <div v-if="works.length" class="flex min-h-0 flex-1 snap-x items-stretch gap-3 overflow-x-auto" :aria-label="t('intelligenceMonitor.recentWorks')">
        <div v-for="(work, index) in works" :key="work.id" class="flex w-[176px] shrink-0 snap-start flex-col overflow-hidden rounded-xl border" :class="index === 0 ? 'border-primary-200 dark:border-primary-600/40' : 'border-gray-100 dark:border-dark-700'">
          <IntelligenceArtifactPreview :run="work" @open="emit('history', work.id)" />
          <button type="button" class="w-full shrink-0 p-2.5 text-left hover:bg-gray-50 dark:hover:bg-dark-700/50" @click="emit('history', work.id)">
            <div class="flex items-center justify-between gap-1"><span class="text-[10px] font-semibold" :class="index === 0 ? 'text-primary-600 dark:text-primary-400' : 'text-gray-400'">{{ isActive(work) ? t(`intelligenceMonitor.status.${work.status}`) : index === 0 ? t('intelligenceMonitor.latest') : `#${work.id}` }}</span><span class="h-1.5 w-1.5 rounded-full" :class="work.status === 'succeeded' ? 'bg-emerald-500' : work.status === 'failed' ? 'bg-rose-500' : 'bg-amber-400'"></span></div>
            <p class="mt-1 text-[10px] tabular-nums text-gray-600 dark:text-dark-300" :title="dateTime(work.started_at || work.created_at)">{{ dateTime(work.started_at || work.created_at) }}</p>
            <div class="mt-1 flex min-w-0 items-center justify-between gap-2 text-[9px]" data-testid="artwork-metadata">
              <span class="min-w-0 truncate text-gray-400" :title="`${work.model} · ${work.reasoning_effort}`">{{ work.model }} · {{ work.reasoning_effort }}</span>
              <span v-if="work.status === 'succeeded' && intelligenceDurationLabel(work, t)" class="ml-auto shrink-0 whitespace-nowrap font-medium tabular-nums text-gray-600 dark:text-dark-300" :title="`${t('intelligenceMonitor.duration')} · ${intelligenceDurationLabel(work, t)}`" data-testid="artwork-duration">{{ intelligenceDurationLabel(work, t) }}</span>
            </div>
          </button>
        </div>
      </div>
      <div v-else class="flex min-h-[196px] flex-1 items-center justify-center rounded-xl border border-dashed border-gray-200 bg-gray-50/50 text-xs text-gray-400 dark:border-dark-700 dark:bg-dark-900/30"><Icon name="lightbulb" size="sm" class="mr-2" />{{ t('intelligenceMonitor.waiting') }}</div>
    </section>
  </article>
</template>
<script setup lang="ts">
import { computed, inject, onBeforeUnmount, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntelligencePlan, IntelligenceRate, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import { intelligenceRateLabel } from './intelligencePreview'
import { intelligenceDurationLabel } from './intelligenceDuration'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'
import { dateTime } from './format'
const props = withDefaults(defineProps<{ plan: IntelligencePlan; overview: UpstreamOverview | null; busy: boolean; visible?: boolean }>(), { visible: true })
const emit = defineEmits<{ run: []; toggle: []; edit: []; archive: []; history: [runID?: number] }>()
const { t } = useI18n()
const panelActive = inject(intelligencePanelActiveKey, ref(true))
const cardActive = computed(() => panelActive.value && props.visible)
// Filtered cards retain their artwork DOM; hidden cards must pause playback.
provide(intelligencePanelActiveKey, cardActive)
const oauth = computed(() => props.plan.source_type === 'openai_oauth')
const supplier = computed(() => props.overview?.suppliers.find(item => item.targets.some(target => target.id === props.plan.upstream_target_id)))
const target = computed(() => supplier.value?.targets.find(item => item.id === props.plan.upstream_target_id))
const siteName = computed(() => oauth.value ? 'OpenAI' : supplier.value?.name || props.plan.supplier_note || props.plan.source_name || t(`intelligenceMonitor.source.${props.plan.source_type}`))
const endpoint = computed(() => oauth.value ? 'https://chatgpt.com' : target.value?.endpoint || props.plan.endpoint || props.plan.latest_run?.source_endpoint || '')
const groupName = computed(() => target.value?.name || props.plan.group_note || props.plan.source_name || props.plan.name)
const rate = computed<IntelligenceRate>(() => target.value?.balance?.billing as unknown as IntelligenceRate || props.plan.rate_snapshot)
const isActive = (run: IntelligenceRun) => run.status === 'running' || run.status === 'pending'
const active = computed(() => !!props.plan.latest_run && isActive(props.plan.latest_run))
const now = ref(Date.now())
const nextRunTime = computed(() => {
  const timestamp = props.plan.next_run_at ? Date.parse(props.plan.next_run_at) : NaN
  return Number.isFinite(timestamp) ? timestamp : null
})
const remainingSeconds = computed(() => nextRunTime.value === null ? null : Math.max(0, Math.ceil((nextRunTime.value - now.value) / 1000)))
const countdownLabel = computed(() => {
  const seconds = remainingSeconds.value ?? 0
  return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60].map(value => String(value).padStart(2, '0')).join(':')
})
let countdownTimer: ReturnType<typeof setInterval> | undefined
function stopCountdown() {
  clearInterval(countdownTimer)
  countdownTimer = undefined
}
// Active runs only have a provisional next_run_at; completion sets the actual deadline.
watch([() => props.plan.enabled, active, nextRunTime, cardActive], () => {
  stopCountdown()
  now.value = Date.now()
  if (!cardActive.value || !props.plan.enabled || active.value || !remainingSeconds.value) return
  countdownTimer = setInterval(() => {
    now.value = Date.now()
    if (!remainingSeconds.value) stopCountdown()
  }, 1000)
}, { immediate: true })
onBeforeUnmount(stopCountdown)
const completedWorks = computed(() => {
  const recent = props.plan.recent_runs || []
  const latest = props.plan.latest_run
  const candidates = latest && !isActive(latest) && !recent.some(run => run.id === latest.id) ? [latest, ...recent] : recent
  const seen = new Set<number>()
  return candidates.filter(run => {
    if (isActive(run) || seen.has(run.id) || (active.value && run.id === latest?.id)) return false
    seen.add(run.id)
    return true
  }).slice(0, 20)
})
const works = computed(() => active.value && props.plan.latest_run ? [props.plan.latest_run, ...completedWorks.value] : completedWorks.value)
const intervalLabel = computed(() => {
  const seconds = props.plan.interval_seconds
  if (seconds % 3600 === 0) return t('intelligenceMonitor.hours', { count: seconds / 3600 })
  if (seconds % 60 === 0) return t('intelligenceMonitor.minutes', { count: seconds / 60 })
  return t('intelligenceMonitor.seconds', { count: seconds })
})
function statusClass(status?: string) {
  if (status === 'succeeded') return 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400'
  if (status === 'failed') return 'bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400'
  if (status === 'running' || status === 'pending') return 'bg-amber-50 text-amber-600 dark:bg-amber-500/10'
  return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'
}
</script>
<style scoped>
.action { @apply flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:opacity-40 dark:text-dark-400 dark:hover:bg-dark-700; }
</style>
