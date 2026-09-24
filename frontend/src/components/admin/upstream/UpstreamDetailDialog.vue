<template>
  <BaseDialog :show="show" :title="target?.name || supplier?.name || t('upstreamCenter.details')" width="extra-wide" @close="emit('close')">
    <template v-if="show">
      <div v-if="target" class="mb-5 flex flex-wrap items-center justify-between gap-3"><div class="min-w-0"><p class="break-all text-xs text-gray-500 dark:text-dark-400">{{ target.endpoint }}</p><p class="mt-1 text-[11px] text-gray-400">{{ target.api_key_masked }} · {{ t('upstreamCenter.everySeconds', { seconds: target.interval_seconds }) }}</p></div><div class="flex items-center gap-2"><UpstreamStatusBadge :status="targetStatus(target, selectedModel)" /><button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="emit('run', target)"><Icon name="play" size="xs" class="mr-1.5" />{{ t(busy ? 'upstreamCenter.running' : 'upstreamCenter.run') }}</button></div></div>
      <UpstreamWallet v-if="target?.supplier_id" class="mb-5" :wallets="target.balance ? [target.balance] : []" :target-id="target.id" :busy="busy" show-key-usage @sync="emit('sync', $event)" />
      <div v-if="target?.supplier_id" class="tabs mb-5 inline-flex"><button type="button" class="tab" :class="tab === 'history' && 'tab-active'" @click="tab = 'history'">{{ t('upstreamCenter.history.title') }}</button><button type="button" class="tab" :class="tab === 'finance' && 'tab-active'" @click="tab = 'finance'">{{ t('upstreamCenter.finance.title') }}</button></div>
      <UpstreamFinancePanel v-if="!target || tab === 'finance'" :supplier="supplier" :target="target" />
      <div v-else class="space-y-5">
        <div class="flex flex-wrap items-end justify-between gap-3"><div class="min-w-[160px] flex-1"><label for="history-model" class="input-label">{{ t('upstreamCenter.model') }}</label><select id="history-model" v-model="selectedModel" class="input"><option v-for="model in target.models" :key="model" :value="model">{{ model }}</option></select></div><div class="min-w-[120px]"><label for="history-window" class="input-label">{{ t('upstreamCenter.range') }}</label><select id="history-window" v-model="historyWindow" class="input"><option v-for="value in windows" :key="value" :value="value">{{ t(`upstreamCenter.ranges.${value}`) }}</option></select></div><button type="button" class="btn btn-secondary" :disabled="loading" @click="loadHistory"><Icon name="refresh" size="sm" :class="loading && 'animate-spin'" /><span class="ml-2">{{ t('upstreamCenter.refresh') }}</span></button></div>
        <div class="grid grid-cols-3 gap-3"><div v-for="metric in metrics" :key="metric.key" class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/60"><p class="text-[10px] text-gray-500 dark:text-dark-400">{{ t(`upstreamCenter.${metric.key}`) }} · {{ t(`upstreamCenter.ranges.${window}`) }}</p><p class="mt-2 text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ metric.value }}</p></div></div>
        <div><UpstreamHistoryBar :records="statistics?.timeline || []" legend @select="selectedRecord = $event" /><p class="mt-2 text-[11px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.latencyHint') }}</p></div>
        <div v-if="selectedRecord" class="rounded-xl border border-primary-100 bg-primary-50/60 p-4 dark:border-primary-900 dark:bg-primary-500/5"><div class="flex items-center justify-between gap-3"><p class="text-xs font-medium text-primary-700 dark:text-primary-300">{{ t('upstreamCenter.history.selected') }} · {{ dateTime(selectedRecord.checked_at) }}</p><button type="button" :aria-label="t('common.close')" class="text-gray-400" @click="selectedRecord = null"><Icon name="x" size="sm" /></button></div><div class="mt-2 flex flex-wrap items-center gap-3 text-xs text-gray-700 dark:text-gray-200"><UpstreamStatusBadge :status="selectedRecord.status" /><span class="break-all font-mono">{{ selectedRecord.model }}</span><span>{{ latency(selectedRecord.latency_ms) }}</span><span>HTTP {{ selectedRecord.http_status || '—' }}</span><span>{{ t('upstreamCenter.history.cost') }} {{ money(selectedRecord.cost) }}</span></div><p v-if="selectedRecord.message" class="mt-3 whitespace-pre-wrap break-words text-xs leading-5 text-gray-600 dark:text-dark-300">{{ selectedRecord.message }}</p></div>
        <p v-if="error" role="alert" class="rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
        <div class="overflow-x-auto rounded-xl border border-gray-100 dark:border-dark-700" :aria-busy="loading" :class="loading && 'opacity-50'"><table class="w-full whitespace-nowrap text-left text-xs"><thead class="bg-gray-50 text-gray-500 dark:bg-dark-900/60 dark:text-dark-400"><tr><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.history.time') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.model') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.history.status') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.responseLatency') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.history.http') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.history.cost') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.history.message') }}</th></tr></thead><tbody class="divide-y divide-gray-100 text-gray-700 dark:divide-dark-700 dark:text-gray-200"><tr v-for="record in records" :key="record.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/30"><td class="px-3 py-3 tabular-nums"><button type="button" class="text-primary-600 hover:underline dark:text-primary-400" @click="selectedRecord = record">{{ dateTime(record.checked_at) }}</button></td><td class="px-3 py-3 font-mono text-[11px]">{{ record.model }}</td><td class="px-3 py-3"><UpstreamStatusBadge :status="record.status" /></td><td class="px-3 py-3 text-right tabular-nums">{{ latency(record.latency_ms) }}</td><td class="px-3 py-3 text-right tabular-nums">{{ record.http_status || '—' }}</td><td class="px-3 py-3 text-right tabular-nums">{{ money(record.cost) }}<span v-if="record.cost_source === 'estimated'" class="ml-1 text-[10px] text-gray-400">~</span></td><td class="max-w-[300px] truncate px-3 py-3" :title="record.message">{{ record.message || '—' }}</td></tr><tr v-if="!records.length"><td colspan="7" class="px-3 py-12 text-center text-gray-400">{{ t('upstreamCenter.history.noRecords') }}</td></tr></tbody></table></div>
        <Pagination v-if="total > 0" :page="page" :total="total" :page-size="50" :show-page-size-selector="false" @update:page="page = $event; loadHistory()" />
      </div>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { upstreamCenterAPI, type UpstreamHistoryRecord, type UpstreamSupplier, type UpstreamTarget, type UpstreamWindow } from '@/api/admin/upstreamCenter'
import { extractApiErrorMessage } from '@/utils/apiError'
import UpstreamFinancePanel from './UpstreamFinancePanel.vue'
import UpstreamStatusBadge from './UpstreamStatusBadge.vue'
import UpstreamHistoryBar from './UpstreamHistoryBar.vue'
import UpstreamWallet from './UpstreamWallet.vue'
import { availability, dateTime, latency, money, targetStatus } from './format'
const props = defineProps<{ show: boolean; supplier: UpstreamSupplier | null; target: UpstreamTarget | null; model: string; record: UpstreamHistoryRecord | null; window: UpstreamWindow; busy: boolean }>()
const emit = defineEmits<{ close: []; run: [target: UpstreamTarget]; sync: [id: number]; 'window-change': [window: UpstreamWindow] }>()
const { t } = useI18n()
const tab = ref('history'), selectedModel = ref(''), historyWindow = ref<UpstreamWindow>('24h'), selectedRecord = ref<UpstreamHistoryRecord | null>(null)
const records = ref<UpstreamHistoryRecord[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref('')
const windows: UpstreamWindow[] = ['24h', '7d', '30d']
const statistics = computed(() => props.target?.statistics?.find(item => item.model === selectedModel.value))
const metrics = computed(() => [{ key: 'availability', value: availability(statistics.value?.availability) }, { key: 'averageLatency', value: latency(statistics.value?.avg_latency_ms) }, { key: 'p95Latency', value: latency(statistics.value?.p95_latency_ms) }])
let controller: AbortController | undefined, resetting = false
async function loadHistory() {
  controller?.abort(); loading.value = false; error.value = ''
  if (!props.show || !props.target || tab.value !== 'history') return
  const current = new AbortController(); controller = current
  loading.value = true; error.value = ''
  try {
    const hours = historyWindow.value === '24h' ? 24 : historyWindow.value === '7d' ? 168 : 720
    const result = await upstreamCenterAPI.history(props.target.id, { model: selectedModel.value, page: page.value, page_size: 50, from: new Date(Date.now() - hours * 3600000).toISOString(), to: new Date().toISOString() }, current.signal)
    if (!current.signal.aborted) { records.value = result.items || []; total.value = result.total }
  } catch (err) { if (!current.signal.aborted) error.value = extractApiErrorMessage(err, t('upstreamCenter.loadFailed')) }
  finally { if (!current.signal.aborted) loading.value = false }
}
watch([() => props.show, () => props.target?.id, () => props.supplier?.id], () => {
  controller?.abort(); loading.value = false; error.value = ''
  if (!props.show) return
  resetting = true
  tab.value = props.target ? 'history' : 'finance'; selectedModel.value = props.model || props.target?.models?.[0] || ''; historyWindow.value = props.window; selectedRecord.value = props.record; records.value = []; total.value = 0; page.value = 1
  resetting = false
  void loadHistory()
}, { immediate: true })
watch([selectedModel, historyWindow, tab], ([model, window], [oldModel, oldWindow]) => {
  if (resetting) return
  if (model !== oldModel || window !== oldWindow) selectedRecord.value = null
  records.value = []; total.value = 0; page.value = 1
  void loadHistory()
}, { flush: 'sync' })
watch(historyWindow, value => { if (props.show && value !== props.window) emit('window-change', value) })
watch(() => props.target?.last_checked_at, () => { if (props.show) void loadHistory() })
onBeforeUnmount(() => controller?.abort())
</script>
