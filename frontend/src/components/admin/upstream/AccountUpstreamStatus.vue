<template>
  <section class="account-status" :aria-label="t('intelligenceMonitor.apiKeyAccount.statusTitle')">
    <header class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div class="flex min-w-0 items-center gap-2">
        <Icon name="chart" size="sm" class="shrink-0 text-primary-500 dark:text-primary-400" />
        <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.apiKeyAccount.statusTitle') }}</h3>
      </div>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <span class="inline-flex items-center gap-1.5 text-[10px] text-gray-400 dark:text-dark-400" data-testid="account-status-schedule">
          <span class="h-1.5 w-1.5 rounded-full" :class="target.enabled ? 'bg-emerald-500' : 'bg-gray-300 dark:bg-dark-500'" aria-hidden="true"></span>
          {{ t(target.enabled ? 'intelligenceMonitor.scheduled' : 'upstreamCenter.status.paused') }}
          <span aria-hidden="true">·</span>
          {{ t('upstreamCenter.everySeconds', { seconds: target.interval_seconds }) }}
        </span>
        <button type="button" class="configure-button" :disabled="busy" data-testid="account-status-configure" @click="emit('configure')">
          <Icon name="edit" size="xs" />{{ t('intelligenceMonitor.apiKeyAccount.configureStatus') }}
        </button>
      </div>
    </header>

    <div class="mt-3 grid min-w-0 gap-x-5 gap-y-3 lg:grid-cols-[minmax(180px,1fr)_minmax(0,3fr)] lg:items-center">
      <div class="min-w-0">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <span class="max-w-full truncate text-sm font-semibold text-gray-900 dark:text-gray-100" :title="target.name">{{ target.name }}</span>
          <UpstreamStatusBadge :status="targetStatus(target, selectedModel)" />
          <UpstreamRateBadge :billing="target.balance?.billing" />
        </div>
        <div class="mt-1.5 flex min-w-0 items-center gap-2 text-[11px] leading-5 text-gray-400 dark:text-dark-400">
          <span v-if="supplierName" class="max-w-[40%] truncate" :title="supplierName">{{ supplierName }}</span>
          <span v-if="supplierName" class="text-gray-200 dark:text-dark-600" aria-hidden="true">/</span>
          <label class="sr-only" :for="`account-status-model-${target.id}`">{{ t('upstreamCenter.model') }}</label>
          <Select v-if="target.models.length > 1" :id="`account-status-model-${target.id}`" v-model="selectedModel" class="status-model min-w-0 max-w-xs flex-1" :options="modelOptions" :searchable="false" :aria-label="t('upstreamCenter.model')" />
          <span v-else class="min-w-0 truncate" :title="selectedModel">{{ selectedModel || '—' }}</span>
        </div>
      </div>
      <dl class="account-status-metrics">
        <div>
          <dt>{{ t('upstreamCenter.wallet.todayUsed') }}</dt>
          <dd :title="`${t('upstreamCenter.wallet.usageHint')} ${money(target.balance?.today_used, target.balance?.currency)}`" data-testid="account-status-upstream-spend">{{ amount(target.balance?.today_used) }}</dd>
        </div>
        <div>
          <dt>{{ t('upstreamCenter.finance.todayRevenue') }}</dt>
          <dd :title="`${t('upstreamCenter.finance.bindingHint')} ${money(target.finance?.revenue, target.finance?.currency)}`" data-testid="account-status-user-spend">{{ amount(target.finance?.revenue) }}</dd>
        </div>
        <div>
          <dt>{{ t('upstreamCenter.finance.todayProfit') }}</dt>
          <dd :class="target.finance?.profit == null ? 'text-amber-600 dark:text-amber-400' : target.finance.profit < 0 ? 'text-rose-600 dark:text-rose-400' : 'text-primary-700 dark:text-primary-400'" :title="target.finance?.profit == null ? t('upstreamCenter.finance.pending') : `${t('upstreamCenter.finance.note')} ${money(target.finance.profit, target.finance.currency)}`" data-testid="account-status-profit">{{ amount(target.finance?.profit) }}</dd>
        </div>
        <div>
          <dt>{{ t('upstreamCenter.availability7d') }}</dt>
          <dd class="text-gray-400" :style="{ color: availabilityColor(statistics?.availability_7d) }" data-testid="account-status-availability">{{ availability(statistics?.availability_7d) }}</dd>
        </div>
        <div>
          <dt>{{ t('upstreamCenter.latestLatency') }}</dt>
          <dd :class="latencyColor(statistics?.latest_latency_ms, target.degraded_threshold_ms, target.timeout_seconds, statistics?.status)" data-testid="account-status-latency">{{ latency(statistics?.latest_latency_ms) }}</dd>
        </div>
      </dl>
    </div>

    <UpstreamHistoryBar class="mt-3" :records="statistics?.timeline || []" :last-checked-at="statistics?.last_checked_at" />
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UpstreamTarget } from '@/api/admin/upstreamCenter'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamHistoryBar from './UpstreamHistoryBar.vue'
import UpstreamRateBadge from './UpstreamRateBadge.vue'
import UpstreamStatusBadge from './UpstreamStatusBadge.vue'
import { amount, money, availability, availabilityColor, latency, latencyColor, targetStatus } from './format'

const props = withDefaults(defineProps<{ target: UpstreamTarget; supplierName?: string; busy?: boolean }>(), { supplierName: '', busy: false })
const emit = defineEmits<{ configure: [] }>()
const { t } = useI18n()
const selectedModel = ref(props.target.models[0] || '')
watch(() => [props.target.id, props.target.models] as const, ([id, models], [previousId]) => {
  if (id !== previousId || !models.includes(selectedModel.value)) selectedModel.value = models[0] || ''
})
const modelOptions = computed(() => props.target.models.map(model => ({ value: model, label: model })))
const statistics = computed(() => props.target.statistics?.find(item => item.model === selectedModel.value))
</script>

<style scoped>
.account-status { @apply min-w-0 rounded-xl border border-gray-200/80 bg-white p-4 dark:border-dark-700 dark:bg-dark-800; }
.account-status-metrics { @apply grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 text-gray-800 dark:text-gray-100 sm:grid-cols-5 sm:gap-x-0 sm:divide-x sm:divide-gray-100 dark:sm:divide-dark-700; }
.account-status-metrics > div { @apply min-w-0 sm:px-3; }
.account-status-metrics > div:first-child { @apply sm:pl-0; }
.account-status-metrics > div:last-child { @apply sm:pr-0; }
.account-status-metrics dt { @apply text-[10px] leading-4 text-gray-400 dark:text-dark-400; }
.account-status-metrics dd { @apply mt-0.5 break-words text-lg font-semibold leading-7 tabular-nums tracking-tight xl:text-xl; }
.configure-button { @apply inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium text-primary-600 transition-colors hover:bg-primary-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-40 dark:text-primary-400 dark:hover:bg-primary-500/10; }
.status-model :deep(.select-trigger) { @apply min-w-0 gap-2 rounded border-0 bg-transparent p-0 text-[11px] leading-5 text-gray-500 hover:text-gray-700 dark:bg-transparent dark:text-dark-400 dark:hover:text-gray-200; }
.status-model :deep(.select-icon svg) { @apply h-3 w-3; }
</style>
