<template>
  <div class="min-w-0">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[10px] text-gray-400 dark:text-dark-400">
      <span>{{ t('upstreamCenter.history.recent') }}</span>
      <span class="tabular-nums" :title="dateTime(lastChecked)" data-testid="upstream-last-updated">{{ updatedLabel }}</span>
    </div>
    <div class="history-strip" :aria-label="t('upstreamCenter.history.recent')">
      <template v-for="(record, index) in bars" :key="record?.id ?? `empty-${index}`">
        <HelpTooltip v-if="record" class="!ml-0 min-w-0 !items-stretch" width-class="w-72 max-w-[calc(100vw-2rem)]">
          <template #trigger>
            <button type="button" class="history-bar cursor-help transition-transform hover:scale-y-110 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :class="barClass(record)" :data-status="record.status" :aria-label="recordLabel(record)" @click="emit('select', record)"></button>
          </template>
          <div class="space-y-1">
            <div class="flex items-start justify-between gap-3"><span class="font-medium text-white">{{ dateTime(record.checked_at) }}</span><button type="button" class="rounded p-0.5 text-gray-300 hover:bg-white/10 hover:text-white" :aria-label="t('upstreamCenter.history.copyDetails')" @click.stop="copyDetails(record)"><Icon name="copy" size="sm" /></button></div>
            <div class="font-semibold" :class="record.status === 'error' || record.status === 'failed' ? 'text-rose-400' : record.status === 'degraded' ? 'text-amber-300' : 'text-emerald-400'">{{ t(`upstreamCenter.status.${record.status}`) }}<span v-if="record.http_status"> · HTTP {{ record.http_status }}</span></div>
            <div class="break-words">{{ t('upstreamCenter.model') }}: {{ record.model }}</div>
            <div>{{ t('upstreamCenter.responseLatency') }}: {{ latency(record.latency_ms) }}</div>
            <div v-if="record.message" class="max-h-48 overflow-auto whitespace-pre-wrap break-words" :class="record.status === 'error' || record.status === 'failed' ? 'text-rose-200' : 'text-gray-200'">{{ record.message }}</div>
          </div>
        </HelpTooltip>
        <span v-else class="history-bar history-bar--empty" data-status="unknown" aria-hidden="true"></span>
      </template>
    </div>
    <div v-if="legend" class="mt-2 flex flex-wrap justify-end gap-3 text-[10px] text-gray-500 dark:text-dark-400">
      <span v-for="state in ['operational', 'degraded', 'error']" :key="state" class="inline-flex items-center gap-1.5"><i class="h-2.5 w-1 rounded-full" :class="colors[state]"></i>{{ t(`upstreamCenter.status.${state}`) }}</span>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import type { UpstreamHistoryRecord } from '@/api/admin/upstreamCenter'
import { dateTime, latency, recentHistory } from './format'
const props = withDefaults(defineProps<{ records?: UpstreamHistoryRecord[]; lastCheckedAt?: string | null; legend?: boolean }>(), { records: () => [], lastCheckedAt: null, legend: false })
const emit = defineEmits<{ select: [record: UpstreamHistoryRecord] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const bars = computed(() => recentHistory(props.records))
const lastChecked = computed(() => props.lastCheckedAt || bars.value[bars.value.length - 1]?.checked_at)
const now = ref(Date.now())
const updatedLabel = computed(() => {
  const timestamp = lastChecked.value ? Date.parse(lastChecked.value) : NaN
  return Number.isFinite(timestamp) ? t('upstreamCenter.history.updatedAgo', { seconds: Math.max(0, Math.floor((now.value - timestamp) / 1000)).toLocaleString() }) : t('upstreamCenter.history.waiting')
})
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => { now.value = Date.now() }, 1000) })
onBeforeUnmount(() => clearInterval(timer))
const colors: Record<string, string> = { operational: 'history-bar--success', degraded: 'history-bar--degraded', error: 'history-bar--failure', failed: 'history-bar--failure' }
function barClass(record: UpstreamHistoryRecord) { return colors[record.status] || 'history-bar--empty' }
function recordLabel(record: UpstreamHistoryRecord) { return `${dateTime(record.checked_at)} · ${record.model} · ${t(`upstreamCenter.status.${record.status}`)} · ${latency(record.latency_ms)}${record.http_status ? ` · HTTP ${record.http_status}` : ''}${record.message ? ` · ${record.message}` : ''}` }
function copyDetails(record: UpstreamHistoryRecord) {
  void copyToClipboard([dateTime(record.checked_at), `${t('upstreamCenter.model')}: ${record.model}`, t(`upstreamCenter.status.${record.status}`), `${t('upstreamCenter.responseLatency')}: ${latency(record.latency_ms)}`, record.http_status ? `HTTP ${record.http_status}` : '', record.message].filter(Boolean).join('\n'))
}
</script>
<style scoped>
.history-strip { display: grid; grid-template-columns: repeat(60, minmax(0, 1fr)); gap: clamp(2px, 0.2vw, 3px); align-items: center; width: 100%; height: 24px; }
.history-bar { display: block; width: 100%; min-width: 0; height: 22px; border-radius: 3px; }
.history-bar--empty { background: linear-gradient(180deg, #e8eaf0, #e3e6ed); }
.history-bar--success { background: linear-gradient(180deg, #27d96c, #19c55d); }
.history-bar--degraded { background: linear-gradient(180deg, #fbc94c, #f2b82b); }
.history-bar--failure { background: linear-gradient(180deg, #fb595e, #ef3d45); }
.dark .history-bar--empty { background: linear-gradient(180deg, #465367, #3b475a); }
@media (prefers-reduced-motion: reduce) { .history-bar { transition: none; } }
</style>
