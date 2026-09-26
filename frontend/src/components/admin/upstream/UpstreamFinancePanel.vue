<template>
  <div class="space-y-5">
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="page = 1; load()">
      <div v-if="supplier && !target" class="min-w-0 flex-1 basis-40"><label for="finance-target" class="input-label">{{ t('upstreamCenter.finance.group') }}</label><Select id="finance-target" v-model="targetId" :options="targetOptions" :searchable="false" :aria-label="t('upstreamCenter.finance.group')" /></div>
      <div class="min-w-[140px] flex-1"><label for="finance-from" class="input-label">{{ t('upstreamCenter.history.start') }}</label><input id="finance-from" v-model="fromDate" type="date" class="input" :max="toDate || undefined" /></div><div class="min-w-[140px] flex-1"><label for="finance-to" class="input-label">{{ t('upstreamCenter.history.end') }}</label><input id="finance-to" v-model="toDate" type="date" class="input" :min="fromDate || undefined" /></div>
      <button type="submit" class="btn btn-primary" :disabled="loading">{{ t('upstreamCenter.finance.refresh') }}</button><button type="button" class="btn btn-secondary" :disabled="loading" @click="fromDate = ''; toDate = ''; page = 1; load()">{{ t('upstreamCenter.finance.today') }}</button>
    </form>
    <p v-if="error" role="alert" class="rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
    <template v-if="data">
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-3">
        <div v-for="metric in metrics" :key="metric.key" class="rounded-xl border border-gray-100 bg-gray-50/70 p-3.5 dark:border-dark-700 dark:bg-dark-900/40"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t(`upstreamCenter.finance.${metric.key}`) }}</p><p class="mt-2 text-lg font-semibold tabular-nums" :class="metric.key === 'profit' ? data.summary.profit == null ? 'text-amber-600 dark:text-amber-400' : data.summary.profit < 0 ? 'text-rose-600 dark:text-rose-400' : 'text-primary-700 dark:text-primary-300' : 'text-gray-900 dark:text-gray-100'">{{ metric.value == null && metric.key === 'profit' ? t('upstreamCenter.finance.pending') : money(metric.value, data.summary.currency) }}</p></div>
      </div>
      <div class="rounded-xl bg-primary-50/60 p-3.5 text-xs leading-5 text-primary-800 dark:bg-primary-500/10 dark:text-primary-200"><p>{{ t('upstreamCenter.finance.note') }}</p><p v-if="data.summary.unpriced_monitor_count" class="mt-1 text-amber-700 dark:text-amber-400">{{ t('upstreamCenter.finance.unpriced', { count: data.summary.unpriced_monitor_count }) }}</p><p class="mt-1 opacity-80">{{ t('upstreamCenter.finance.bindingHint') }}</p></div>
      <div class="flex flex-wrap justify-between gap-2 text-[11px] text-gray-500 dark:text-dark-400"><span>{{ t('upstreamCenter.finance.accountingDate', { from: dateTime(data.summary.from), to: dateTime(data.summary.to) }) }}</span><span>{{ t(`upstreamCenter.finance.${data.summary.cost_source}`) }} · {{ t('upstreamCenter.finance.currency', { currency: data.summary.currency }) }}</span></div>
      <div class="overflow-x-auto rounded-xl border border-gray-100 dark:border-dark-700" :class="loading && 'opacity-50'" :aria-busy="loading">
        <table class="w-full whitespace-nowrap text-left text-xs"><thead class="bg-gray-50 text-gray-500 dark:bg-dark-900/60 dark:text-dark-400"><tr><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.time') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.group') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.model') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.account') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.siteGroup') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.finance.revenue') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.finance.businessCost') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('upstreamCenter.finance.businessProfit') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.billing') }}</th><th class="px-3 py-3 font-medium">{{ t('upstreamCenter.finance.request') }}</th></tr></thead><tbody class="divide-y divide-gray-100 text-gray-700 dark:divide-dark-700 dark:text-gray-200"><tr v-for="row in data.items" :key="row.id" class="hover:bg-gray-50/60 dark:hover:bg-dark-700/30"><td class="px-3 py-3 tabular-nums">{{ dateTime(row.created_at) }}</td><td class="px-3 py-3">{{ row.target_name }}</td><td class="px-3 py-3 font-mono text-[11px]">{{ row.model }}</td><td class="px-3 py-3">{{ row.account_id ? `#${row.account_id}` : '—' }}</td><td class="px-3 py-3">{{ row.group_id ? `#${row.group_id}` : '—' }}</td><td class="px-3 py-3 text-right tabular-nums">{{ money(row.revenue, data.summary.currency) }}</td><td class="px-3 py-3 text-right tabular-nums">{{ money(row.business_cost, data.summary.currency) }}</td><td class="px-3 py-3 text-right tabular-nums" :class="row.profit < 0 ? 'text-rose-600 dark:text-rose-400' : 'text-primary-700 dark:text-primary-400'">{{ money(row.profit, data.summary.currency) }}</td><td class="px-3 py-3">{{ t(row.billing_type === 1 ? 'upstreamCenter.finance.subscriptionBilling' : 'upstreamCenter.finance.balanceBilling') }}</td><td class="max-w-[190px] truncate px-3 py-3 font-mono text-[11px]" :title="row.request_id">{{ row.request_id || '—' }}</td></tr><tr v-if="!data.items.length"><td colspan="10" class="px-4 py-12 text-center text-gray-400">{{ t('upstreamCenter.finance.noRows') }}</td></tr></tbody></table>
      </div>
      <Pagination v-if="data.total > 0" :page="page" :total="data.total" :page-size="50" :show-page-size-selector="false" @update:page="page = $event; load()" />
      <p class="text-[11px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.finance.diffHint') }}</p>
    </template>
    <div v-else-if="loading" class="flex h-40 items-center justify-center"><Icon name="refresh" class="animate-spin text-primary-500" /></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { upstreamCenterAPI, type UpstreamFinancePage, type UpstreamSupplier, type UpstreamTarget } from '@/api/admin/upstreamCenter'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import { dateTime, money } from './format'
const props = defineProps<{ supplier: UpstreamSupplier | null; target: UpstreamTarget | null }>()
const { t } = useI18n()
const data = ref<UpstreamFinancePage | null>(null), error = ref(''), loading = ref(false)
const page = ref(1), fromDate = ref(''), toDate = ref(''), targetId = ref('')
const targetOptions = computed(() => [
  { value: '', label: t('upstreamCenter.financeDetails') },
  ...(props.supplier?.targets || []).map(target => ({ value: String(target.id), label: target.name }))
])
const metrics = computed(() => data.value ? [
  { key: 'revenue', value: data.value.summary.revenue }, { key: 'businessCost', value: data.value.summary.business_cost }, { key: 'monitorCost', value: data.value.summary.monitor_cost },
  { key: 'profit', value: data.value.summary.profit }, { key: 'remoteUsedRange', value: data.value.summary.remote_used }, { key: 'reconciliation', value: data.value.summary.reconciliation_delta },
] : [])
let controller: AbortController | undefined
async function load() {
  controller?.abort(); const current = new AbortController(); controller = current
  loading.value = true; error.value = ''
  const to = toDate.value ? new Date(`${toDate.value}T00:00:00`) : null
  if (to) to.setDate(to.getDate() + 1)
  try {
    const result = await upstreamCenterAPI.finance({ supplier_id: props.supplier?.id, target_id: props.target?.id || (targetId.value ? Number(targetId.value) : undefined), from: fromDate.value ? new Date(`${fromDate.value}T00:00:00`).toISOString() : undefined, to: to?.toISOString(), page: page.value, page_size: 50 }, current.signal)
    if (!current.signal.aborted) data.value = result
  } catch (err) { if (!current.signal.aborted) error.value = extractApiErrorMessage(err, t('upstreamCenter.loadFailed'), { UPSTREAM_FINANCE_ARCHIVED_RANGE: t('upstreamCenter.storage.archivedFinanceRange') }) }
  finally { if (!current.signal.aborted) loading.value = false }
}
watch([() => props.supplier?.id, () => props.target?.id], () => { data.value = null; page.value = 1; targetId.value = ''; fromDate.value = ''; toDate.value = ''; void load() }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
