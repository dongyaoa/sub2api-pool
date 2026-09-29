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
            <p class="break-words">{{ t('intelligenceMonitor.candy.answer') }}: {{ run.answer || '—' }}</p>
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
import { candyHistory, candyResult, candyResultTone, isIntelligenceRunActive } from './intelligenceCandy'
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
