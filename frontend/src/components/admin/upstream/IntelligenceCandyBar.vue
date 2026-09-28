<template>
  <div class="mb-3 shrink-0 rounded-lg border border-gray-100 bg-gray-50/70 px-3 py-2 dark:border-dark-700 dark:bg-dark-900/40" data-testid="candy-monitor">
    <div class="mb-1.5 flex min-w-0 items-center justify-between gap-2 text-[10px]">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1"><span class="shrink-0 font-semibold text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.candy.title') }}</span><span class="text-gray-400">{{ t('intelligenceMonitor.candy.recent') }}</span><span v-if="active" class="inline-flex shrink-0 items-center gap-1 text-violet-600 dark:text-violet-300"><Icon name="refresh" size="xs" class="animate-spin" />{{ t('intelligenceMonitor.candy.running') }}</span><template v-else-if="plan.enabled"><span class="text-gray-400">{{ t('intelligenceMonitor.minutes', { count: (plan.candy_interval_seconds || 180) / 60 }) }}</span><time v-if="remaining !== null && remaining > 0" :datetime="plan.candy_next_run_at || undefined" class="font-mono font-semibold tabular-nums text-violet-600 dark:text-violet-300" data-testid="candy-countdown">{{ countdown }}</time><span v-else class="text-gray-400">{{ t('intelligenceMonitor.waitingSchedule') }}</span></template><span v-else class="text-gray-400">{{ t('intelligenceMonitor.manual') }}</span></div>
      <button type="button" class="shrink-0 font-semibold text-violet-600 disabled:opacity-40 dark:text-violet-300" :disabled="busy || planActive" data-testid="candy-run" @click="emit('run')">{{ t('intelligenceMonitor.candy.run') }}</button>
    </div>
    <div class="candy-strip" :style="{ gridTemplateColumns: `repeat(${bars.length}, minmax(0, 1fr))` }" :aria-label="t('intelligenceMonitor.candy.recent')">
      <template v-for="(run, index) in bars" :key="run?.id ?? `empty-${index}`">
        <HelpTooltip v-if="run" lazy class="!ml-0 min-w-0 !items-stretch" width-class="w-80 max-w-[calc(100vw-2rem)]">
          <template #trigger><button type="button" class="candy-bar transition-opacity hover:opacity-75 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-violet-500" :class="barClass(run)" :data-candy-status="candyResult(run)" :aria-label="`${dateTime(run.started_at || run.created_at)} · ${t('intelligenceMonitor.candy.' + candyResult(run))}`" @click="emit('select', run)" /></template>
          <div class="max-h-[min(360px,65vh)] space-y-1.5 overflow-y-auto text-xs">
            <p class="font-semibold">{{ dateTime(run.started_at || run.created_at) }}</p>
            <p :class="toneClass(run)">{{ t('intelligenceMonitor.candy.' + candyResult(run)) }}<span v-if="run.http_status"> · HTTP {{ run.http_status }}</span></p>
            <p class="break-words">{{ run.model }} · {{ run.reasoning_effort }}</p>
            <div class="!my-2 space-y-1.5 border-y border-white/10 py-2" data-testid="candy-fingerprint-tooltip">
              <p class="flex items-start justify-between gap-3"><span class="shrink-0 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.answerVerdict') }}</span><span class="text-right" :class="run.correct === true ? 'text-emerald-300' : run.correct === false ? 'text-rose-300' : 'text-gray-300'">{{ t('intelligenceMonitor.candy.' + candyAnswerResult(run)) }} · {{ run.answer || '—' }}</span></p>
              <p class="flex items-start justify-between gap-3"><span class="shrink-0 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.verdict') }}</span><span class="text-right">{{ t('intelligenceMonitor.candy.fingerprint.statuses.' + fingerprintResult(run.fingerprint)) }}</span></p>
              <template v-if="run.fingerprint">
                <p class="flex items-start justify-between gap-3"><span class="shrink-0 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.model') }}</span><span class="break-all text-right">{{ run.fingerprint.model }} · {{ run.fingerprint.reasoning_effort }}</span></p>
                <p v-if="fingerprintDeclaredComparison(run.fingerprint)" class="flex flex-wrap justify-between gap-x-3 gap-y-1 font-mono tabular-nums"><span>JSD {{ fingerprintMetric(fingerprintDeclaredComparison(run.fingerprint)?.mean_jsd) }}</span><span>p {{ fingerprintMetric(fingerprintDeclaredComparison(run.fingerprint)?.p_value) }}</span></p>
                <p class="flex items-start justify-between gap-3"><span class="shrink-0 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.nearest') }}</span><span class="break-all text-right">{{ run.fingerprint.attribution?.nearest || '—' }}</span></p>
                <p v-if="run.fingerprint.attribution?.nearest !== run.fingerprint.model && fingerprintNearestComparison(run.fingerprint)" class="flex flex-wrap justify-between gap-x-3 gap-y-1 font-mono tabular-nums"><span>JSD {{ fingerprintMetric(fingerprintNearestComparison(run.fingerprint)?.mean_jsd) }}</span><span>p {{ fingerprintMetric(fingerprintNearestComparison(run.fingerprint)?.p_value) }}</span></p>
                <p class="flex flex-wrap justify-between gap-x-3 gap-y-1 text-gray-300"><span>{{ t('intelligenceMonitor.candy.fingerprint.progress') }} {{ run.fingerprint.done }}/{{ run.fingerprint.total }}</span><span>{{ t('intelligenceMonitor.candy.fingerprint.valid') }} {{ run.fingerprint.valid }}</span></p>
                <p v-if="run.fingerprint.error" class="break-words text-rose-200">{{ run.fingerprint.error }}</p>
              </template>
              <p class="text-[10px] leading-4 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.disclaimer') }}</p>
            </div>
            <p>{{ t('intelligenceMonitor.totalDuration') }}: {{ intelligenceDurationLabel(run, t) || '—' }}</p>
            <p v-if="run.error" class="max-h-32 overflow-auto whitespace-pre-wrap break-words text-rose-200">{{ run.error }}</p>
          </div>
        </HelpTooltip>
        <span v-else class="candy-bar candy-bar-empty" data-candy-status="empty" aria-hidden="true" />
      </template>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, inject, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IntelligencePlan, IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import { candyAnswerResult, candyHistory, candyResult, candyResultTone, fingerprintDeclaredComparison, fingerprintMetric, fingerprintNearestComparison, fingerprintResult, isIntelligenceRunActive } from './intelligenceCandy'
import { intelligenceDurationLabel } from './intelligenceDuration'
import { dateTime } from './format'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'
import { useIntelligenceCountdown } from './useIntelligenceCountdown'
const props = defineProps<{ plan: IntelligencePlan; busy: boolean }>()
const emit = defineEmits<{ run: []; select: [run: IntelligenceRun] }>()
const { t } = useI18n()
const active = computed(() => isIntelligenceRunActive(props.plan.candy_latest_run))
const planActive = computed(() => isIntelligenceRunActive(props.plan.candy_latest_run))
const panelActive = inject(intelligencePanelActiveKey, ref(true))
const { remaining, label: countdown } = useIntelligenceCountdown(() => props.plan.candy_next_run_at, () => props.plan.enabled && !active.value && panelActive.value)
const bars = computed(() => {
  const records = candyHistory(props.plan).reverse()
  return [...Array<null>(Math.max(0, 60 - records.length)).fill(null), ...records]
})
function barClass(run: IntelligenceRun) {
  const tone = candyResultTone(run)
  return tone === 'success' ? 'candy-bar-correct' : tone === 'error' ? 'candy-bar-failed' : tone === 'warning' ? 'candy-bar-warning' : 'candy-bar-empty'
}
function toneClass(run: IntelligenceRun) {
  const tone = candyResultTone(run)
  return tone === 'success' ? 'text-emerald-300' : tone === 'error' ? 'text-rose-300' : tone === 'warning' ? 'text-amber-300' : 'text-gray-300'
}
</script>
<style scoped>
.candy-strip { display: grid; width: 100%; height: 16px; gap: clamp(2px, .2vw, 3px); }
.candy-bar { display: block; width: 100%; min-width: 0; height: 16px; border-radius: 2px; }
.candy-bar-empty { background: linear-gradient(180deg, #e8eaf0, #e3e6ed); }
.candy-bar-correct { background: linear-gradient(180deg, #27d96c, #19c55d); }
.candy-bar-failed { background: linear-gradient(180deg, #fb595e, #ef3d45); }
.candy-bar-warning { background: linear-gradient(180deg, #f9c74f, #edab26); }
:global(.dark) .candy-bar-empty { background: linear-gradient(180deg, #465367, #3b475a); }
</style>
