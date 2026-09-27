<template>
  <article class="local-monitor-card" data-testid="local-plan-card">
    <aside class="local-identity">
      <div class="flex min-w-0 items-center gap-2.5">
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-300"><Icon name="grid" size="sm" /></span>
        <div class="min-w-0 flex-1">
          <h3 class="line-clamp-2 break-words text-sm font-semibold leading-5 text-gray-900 dark:text-gray-100" :title="groupName">{{ groupName }}</h3>
          <p v-if="plan.name !== groupName" class="mt-0.5 truncate text-[11px] text-gray-400" :title="plan.name">{{ plan.name }}</p>
        </div>
        <span class="shrink-0 rounded-md bg-emerald-50 px-2 py-1 text-xs font-semibold tabular-nums text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300" :title="t('intelligenceMonitor.local.groupRate')" data-testid="local-group-rate">{{ rate }}</span>
      </div>

      <div class="my-3.5 min-w-0 rounded-lg bg-gray-50 px-2.5 py-2 dark:bg-dark-900/40">
        <div class="flex min-w-0 items-center justify-between gap-2 text-[10px]">
          <span class="inline-flex shrink-0 items-center gap-1 text-gray-500 dark:text-dark-300"><Icon name="key" size="xs" />{{ t(plan.local_api_key_managed !== false ? 'intelligenceMonitor.local.managedBadge' : 'intelligenceMonitor.local.ownBadge') }}</span>
          <span class="truncate font-mono text-gray-400">{{ plan.api_key_masked || '••••' }}</span>
        </div>
        <p v-if="plan.local_api_key_name" class="mt-1 truncate text-[11px] font-medium text-gray-600 dark:text-gray-300" :title="plan.local_api_key_name">{{ plan.local_api_key_name }}</p>
      </div>

      <div class="space-y-2 text-[11px]">
        <div class="flex items-center justify-between gap-2">
          <span class="inline-flex items-center gap-1.5 text-gray-400"><Icon name="clock" size="xs" />{{ t('intelligenceMonitor.local.pelican') }}</span>
          <span class="font-medium" :class="plan.enabled ? 'text-gray-700 dark:text-gray-200' : 'text-gray-400'">{{ intervalLabel }}</span>
        </div>
        <div v-if="plan.enabled || pelicanActive" class="flex min-h-4 items-center justify-between gap-2">
          <span class="text-gray-400">{{ t('intelligenceMonitor.nextCheck') }}</span>
          <span v-if="pelicanActive" class="inline-flex items-center gap-1.5 text-violet-600 dark:text-violet-300"><span class="h-1.5 w-1.5 rounded-full bg-current motion-safe:animate-pulse" />{{ t(`intelligenceMonitor.status.${plan.latest_run?.status}`) }}</span>
          <time v-else-if="remaining !== null && remaining > 0" :datetime="plan.next_run_at || undefined" :title="dateTime(plan.next_run_at)" class="font-mono text-xs font-semibold tabular-nums text-emerald-600 dark:text-emerald-300" data-testid="local-pelican-countdown">{{ countdown }}</time>
          <span v-else class="text-gray-500 dark:text-dark-300">{{ t('intelligenceMonitor.waitingSchedule') }}</span>
        </div>
      </div>
      <p v-if="plan.local_group_status && plan.local_group_status !== 'active'" class="mt-2 text-[10px] text-amber-600 dark:text-amber-300">{{ t('intelligenceMonitor.sourceMissing') }}</p>
      <p v-if="plan.notes" class="mt-2 truncate text-[10px] text-gray-400" :title="plan.notes">{{ plan.notes }}</p>

      <div class="mt-auto flex items-center justify-between gap-2 pt-3.5">
        <button type="button" class="local-run" :disabled="busy || pelicanActive" data-testid="local-pelican-run" @click="emit('run')"><Icon :name="pelicanActive ? 'clock' : 'play'" size="xs" />{{ t('intelligenceMonitor.run') }}</button>
        <div class="flex items-center gap-0.5">
          <button type="button" class="local-action" :disabled="busy" :aria-label="t(plan.enabled ? 'intelligenceMonitor.pause' : 'intelligenceMonitor.resume')" :title="t(plan.enabled ? 'intelligenceMonitor.pause' : 'intelligenceMonitor.resume')" @click="emit('toggle')"><Icon :name="plan.enabled ? 'clock' : 'play'" size="sm" /></button>
          <button type="button" class="local-action" :disabled="busy || active" :aria-label="t('intelligenceMonitor.edit')" :title="t('intelligenceMonitor.edit')" @click="emit('edit')"><Icon name="edit" size="sm" /></button>
          <button type="button" class="local-action hover:!text-rose-500" :disabled="busy || active" :aria-label="t('intelligenceMonitor.archive')" :title="t('intelligenceMonitor.archive')" @click="emit('archive')"><Icon name="trash" size="sm" /></button>
        </div>
      </div>
    </aside>

    <section class="min-w-0 p-4">
      <IntelligenceCandyBar v-if="plan.candy_enabled" :plan="plan" :busy="busy" @run="emit('candyRun')" @select="emit('candySelect', $event)" />
      <div class="mb-2.5 flex min-h-6 flex-wrap items-center justify-between gap-2">
        <div class="flex items-center gap-2"><span class="text-xs font-semibold text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.recentWorks') }}</span><span class="text-[10px] text-gray-400">{{ t('intelligenceMonitor.newestFirst') }}</span></div>
        <button type="button" class="inline-flex items-center gap-1 text-[11px] text-gray-500 transition-colors hover:text-primary-600 dark:text-dark-300 dark:hover:text-primary-300" @click="emit('history')">{{ t('intelligenceMonitor.history') }}<span class="ml-0.5 tabular-nums text-gray-400">{{ completed.length }}/20</span><Icon name="chevronRight" size="xs" /></button>
      </div>
      <div v-if="works.length" ref="worksScroller" class="local-artwork-strip" :aria-label="t('intelligenceMonitor.recentWorks')" data-testid="local-artwork-strip">
        <div v-for="(work, index) in works" :key="work.id" class="local-artwork" :class="index === 0 && 'local-artwork-latest'" :data-run-id="work.id">
          <IntelligenceArtifactPreview :run="work" class="local-artwork-preview !min-h-0 !flex-none !aspect-auto" @open="emit('history', work.id)" />
          <button type="button" class="local-artwork-caption" @click="emit('history', work.id)">
            <div class="flex items-center justify-between gap-2">
              <span class="inline-flex min-w-0 items-center gap-1.5 text-[10px]" :class="statusClass(work)"><span class="h-1.5 w-1.5 shrink-0 rounded-full bg-current" /><span class="truncate">{{ index === 0 && !isIntelligenceRunActive(work) ? t('intelligenceMonitor.latest') : t(`intelligenceMonitor.status.${work.status}`) }}</span></span>
              <time class="shrink-0 text-[10px] tabular-nums text-gray-500 dark:text-dark-300" :datetime="work.started_at || work.created_at" :title="dateTime(work.started_at || work.created_at)">{{ compactTime(work.started_at || work.created_at) }}</time>
            </div>
            <div class="mt-1.5 flex min-w-0 items-center justify-between gap-2 text-[9px]" data-testid="artwork-metadata">
              <span class="min-w-0 truncate text-gray-400" :title="`${work.model} · ${work.reasoning_effort}`">{{ work.model }} · {{ work.reasoning_effort }}</span>
              <span v-if="work.status === 'succeeded' && intelligenceDurationLabel(work, t)" class="shrink-0 whitespace-nowrap font-medium tabular-nums text-gray-600 dark:text-dark-300" :title="`${t('intelligenceMonitor.duration')} · ${intelligenceDurationLabel(work, t)}`" data-testid="artwork-duration">{{ intelligenceDurationLabel(work, t) }}</span>
            </div>
          </button>
        </div>
      </div>
      <div v-else class="local-artwork-empty" data-testid="local-artwork-empty"><Icon name="lightbulb" size="md" class="text-gray-300 dark:text-dark-500" /><span>{{ t('intelligenceMonitor.waiting') }}</span></div>
    </section>
  </article>
</template>

<script setup lang="ts">
import { computed, inject, nextTick, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import IntelligenceCandyBar from './IntelligenceCandyBar.vue'
import { isIntelligencePlanActive, isIntelligenceRunActive } from './intelligenceCandy'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'
import { intelligenceDurationLabel } from './intelligenceDuration'
import { useIntelligenceCountdown } from './useIntelligenceCountdown'
import { dateTime } from './format'

const props = withDefaults(defineProps<{ plan: IntelligencePlan; busy: boolean; visible?: boolean; overview?: UpstreamOverview | null }>(), { visible: true })
const emit = defineEmits<{ run: []; candyRun: []; candySelect: [run: IntelligenceRun]; edit: []; toggle: []; archive: []; history: [id?: number] }>()
const { t, locale } = useI18n()
const parentActive = inject(intelligencePanelActiveKey, ref(true))
const visible = computed(() => parentActive.value && props.visible)
provide(intelligencePanelActiveKey, visible)
const active = computed(() => isIntelligencePlanActive(props.plan))
const pelicanActive = computed(() => isIntelligenceRunActive(props.plan.latest_run))
const groupName = computed(() => props.plan.local_group_name || props.plan.source_name || props.plan.name)
const rate = computed(() => typeof props.plan.local_group_rate_multiplier === 'number' && Number.isFinite(props.plan.local_group_rate_multiplier) ? `${props.plan.local_group_rate_multiplier}×` : '—')
const intervalLabel = computed(() => !props.plan.enabled ? t('intelligenceMonitor.manual') : props.plan.interval_seconds % 60 === 0 ? t('intelligenceMonitor.minutes', { count: props.plan.interval_seconds / 60 }) : t('intelligenceMonitor.seconds', { count: props.plan.interval_seconds }))
const { remaining, label: countdown } = useIntelligenceCountdown(() => props.plan.next_run_at, () => visible.value && props.plan.enabled && !pelicanActive.value)
const timeFormatter = computed(() => new Intl.DateTimeFormat(locale?.value || 'zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }))
function compactTime(value: string) { const date = new Date(value); return Number.isFinite(date.getTime()) ? timeFormatter.value.format(date) : '—' }
const completed = computed(() => {
  const records = [...(props.plan.recent_runs || [])]
  const latest = props.plan.latest_run
  if (latest && !isIntelligenceRunActive(latest) && latest.test_kind !== 'candy') {
    const index = records.findIndex(run => run.id === latest.id)
    if (index >= 0) records[index] = latest
    else records.unshift(latest)
  }
  const seen = new Set<number>()
  return records.filter(run => {
    if (run.test_kind === 'candy' || isIntelligenceRunActive(run) || seen.has(run.id) || (pelicanActive.value && run.id === latest?.id)) return false
    seen.add(run.id)
    return true
  }).slice(0, 20)
})
const works = computed(() => pelicanActive.value && props.plan.latest_run ? [props.plan.latest_run, ...completed.value] : completed.value)
const worksScroller = ref<HTMLElement | null>(null)
watch(() => props.plan.latest_run?.id, async () => {
  if (!pelicanActive.value) return
  await nextTick()
  if (worksScroller.value) worksScroller.value.scrollLeft = 0
})
function statusClass(run: IntelligenceRun) {
  return run.status === 'succeeded' ? 'text-emerald-600 dark:text-emerald-400' : run.status === 'failed' ? 'text-rose-500 dark:text-rose-400' : 'text-violet-600 dark:text-violet-300'
}
</script>

<style scoped>
.local-monitor-card { @apply grid min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800; box-shadow: 0 1px 2px rgb(15 23 42 / 0.025); }
.local-identity { @apply flex min-w-0 flex-col border-b border-gray-100 p-4 dark:border-dark-700; }
.local-action { @apply flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-gray-200; }
.local-run { @apply inline-flex items-center gap-1.5 rounded-lg border border-violet-100 bg-violet-50 px-2.5 py-1.5 text-[11px] font-semibold text-violet-600 transition-colors hover:border-violet-200 hover:bg-violet-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-violet-500 disabled:opacity-40 dark:border-violet-500/20 dark:bg-violet-500/10 dark:text-violet-300 dark:hover:bg-violet-500/20; }
.local-artwork-strip { @apply flex min-w-0 snap-x gap-2.5 overflow-x-auto pb-1; scrollbar-width: thin; scrollbar-color: #d1d5db transparent; }
.local-artwork { @apply w-[184px] shrink-0 snap-start overflow-hidden rounded-lg border border-gray-100 dark:border-dark-700; }
.local-artwork-latest { @apply border-emerald-200 dark:border-emerald-500/30; }
.local-artwork-preview { height: 116px; }
.local-artwork-caption { @apply block w-full border-t border-gray-100 px-2.5 py-2 text-left transition-colors hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 dark:border-dark-700 dark:hover:bg-dark-700/50; }
.local-artwork-empty { @apply flex h-[176px] items-center justify-center gap-2.5 rounded-lg border border-dashed border-gray-200 bg-gray-50/40 px-4 text-xs text-gray-400 dark:border-dark-700 dark:bg-dark-900/20; }
@media (min-width: 1024px) {
  .local-monitor-card { grid-template-columns: 232px minmax(0, 1fr); }
  .local-identity { @apply border-b-0 border-r; }
}
@media (min-width: 1280px) {
  .local-monitor-card { grid-template-columns: 260px minmax(0, 1fr); }
  .local-artwork { width: 200px; }
  .local-artwork-preview { height: 124px; }
}
:global(.dark) .local-artwork-strip { scrollbar-color: #475569 transparent; }
</style>
