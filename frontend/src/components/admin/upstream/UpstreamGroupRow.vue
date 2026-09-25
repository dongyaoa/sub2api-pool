<template>
  <section class="group-row">
    <header class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <button type="button" class="max-w-[240px] truncate text-left text-sm font-semibold text-gray-900 hover:text-primary-600 dark:text-gray-100 dark:hover:text-primary-400" :title="target.name" @click="emit('details', target, selectedModel)">{{ target.name }}</button>
        <UpstreamStatusBadge :status="targetStatus(target, selectedModel)" />
        <UpstreamRateBadge :billing="target.balance?.billing" />
        <span class="font-mono text-[10px] text-gray-400 dark:text-dark-400">{{ target.api_key_masked }}</span>
      </div>
      <div class="flex shrink-0 items-center gap-0.5">
        <button type="button" class="mr-1 inline-flex items-center gap-1 rounded-md px-2 py-1.5 text-xs font-medium text-primary-600 hover:bg-primary-50 disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-500/10" :disabled="busy || running" @click="emit('run', target)">
          <Icon :name="running ? 'refresh' : 'play'" size="xs" :class="running && 'animate-spin'" />{{ t(running ? 'upstreamCenter.running' : 'upstreamCenter.run') }}
        </button>
        <button type="button" class="action" :disabled="busy" :aria-label="t(target.enabled ? 'upstreamCenter.pause' : 'upstreamCenter.resume')" :title="t(target.enabled ? 'upstreamCenter.pause' : 'upstreamCenter.resume')" @click="emit('toggle', target)">
          <svg v-if="target.enabled" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path stroke-linecap="round" d="M8 5v14M16 5v14" /></svg><Icon v-else name="play" size="xs" />
        </button>
        <button type="button" class="action" :disabled="busy" :aria-label="t('upstreamCenter.edit')" :title="t('upstreamCenter.edit')" @click="emit('edit', target)"><Icon name="edit" size="xs" /></button>
        <button type="button" class="action hover:!text-rose-500" :disabled="busy" :aria-label="t('upstreamCenter.remove')" :title="t('upstreamCenter.remove')" @click="emit('delete', target)"><Icon name="trash" size="xs" /></button>
      </div>
    </header>

    <div class="group-body mt-2.5">
      <div class="monitor-panel">
        <div class="monitor-overview">
        <div class="monitor-model flex min-w-0 flex-wrap items-center justify-between gap-2">
          <div class="flex min-w-0 flex-1 items-center gap-2">
            <label :for="`upstream-group-model-${target.id}`" class="sr-only">{{ t('upstreamCenter.model') }}</label>
            <Select v-if="target.models.length > 1" :id="`upstream-group-model-${target.id}`" v-model="selectedModel" :options="modelOptions" :searchable="false" :aria-label="t('upstreamCenter.model')" class="model-select min-w-0 max-w-full" :title="selectedModel" />
            <span v-else class="truncate font-mono text-[11px] font-medium text-gray-600 dark:text-dark-300" :title="selectedModel">{{ selectedModel }}</span>
            <span v-if="modelIssues && target.models.length > 1" class="shrink-0 text-[10px] text-amber-600 dark:text-amber-400">{{ t('upstreamCenter.billing.modelIssuesShort', { count: modelIssues }) }}</span>
          </div>
          <button type="button" class="inline-flex shrink-0 items-center gap-1 text-[10px] text-gray-400 hover:text-primary-600 dark:hover:text-primary-400" @click="emit('details', target, selectedModel)">{{ t('upstreamCenter.details') }}<Icon name="chevronRight" size="xs" /></button>
        </div>
        <div class="monitor-measurements">
          <div class="monitor-measurement">
            <span class="text-[10px] text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.availability7d') }}</span>
            <strong class="text-xl font-semibold tabular-nums tracking-tight text-gray-400" :style="{ color: availabilityColor(statistics?.availability_7d) }" data-testid="upstream-availability">{{ availability(statistics?.availability_7d) }}</strong>
          </div>
          <div class="monitor-measurement">
            <span class="text-[10px] text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.latestLatency') }}</span>
            <strong class="text-xl font-semibold tabular-nums tracking-tight" :class="latencyColor(statistics?.latest_latency_ms, target.degraded_threshold_ms, target.timeout_seconds, statistics?.status)" data-testid="upstream-latency">{{ latency(statistics?.latest_latency_ms) }}</strong>
          </div>
        </div>
        </div>
        <UpstreamHistoryBar :records="statistics?.timeline || []" :last-checked-at="statistics?.last_checked_at" @select="record => emit('details', target, selectedModel, record)" />
      </div>

      <dl class="metric-grid" :aria-label="t('upstreamCenter.finance.title')">
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.billing.upstreamTodayShort') }}</dt>
          <dd class="metric-value" :title="`${t('upstreamCenter.wallet.usageHint')} ${money(target.balance?.today_used, target.balance?.currency)}`" data-testid="upstream-remote-spend">{{ amount(target.balance?.today_used) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.billing.revenueTodayShort') }}</dt>
          <dd class="metric-value" :title="money(target.finance?.revenue, target.finance?.currency)" data-testid="upstream-user-spend">{{ amount(target.finance?.revenue) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.billing.profitTodayShort') }}</dt>
          <dd class="metric-value" :class="target.finance?.profit == null ? '!text-amber-600 dark:!text-amber-400' : target.finance.profit < 0 ? '!text-rose-600 dark:!text-rose-400' : '!text-primary-700 dark:!text-primary-400'" :title="target.finance?.profit == null ? t('upstreamCenter.finance.pending') : `${t('upstreamCenter.finance.note')} ${money(target.finance.profit, target.finance.currency)}`" data-testid="upstream-profit">{{ amount(target.finance?.profit) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.finance.todayRequests') }}</dt>
          <dd class="secondary-value" data-testid="upstream-request-count">{{ target.finance?.request_count?.toLocaleString() ?? '—' }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.finance.todayTokens') }}</dt>
          <dd class="secondary-value" :class="target.finance?.unknown_token_requests ? '!text-amber-600 dark:!text-amber-400' : ''" :title="target.finance?.unknown_token_requests ? t('upstreamCenter.finance.tokensIncomplete', { count: target.finance.unknown_token_requests }) : t('upstreamCenter.finance.tokensHint')" data-testid="upstream-token-count">{{ compactTokens(target.finance?.total_tokens) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="metric-label">{{ t('upstreamCenter.finance.accountBilled') }}</dt>
          <dd class="secondary-value" :title="`${t('upstreamCenter.finance.accountBilledHint')} ${money(target.finance?.account_billed, target.finance?.currency)}`" data-testid="upstream-account-billed">{{ amount(target.finance?.account_billed) }}</dd>
        </div>
      </dl>
    </div>
    <p v-if="target.balance?.status === 'error'" class="mt-2 truncate text-[10px] text-amber-600 dark:text-amber-400" :title="upstreamSyncError(target.balance.error, t)">{{ t('upstreamCenter.balanceFailed') }}<span v-if="target.balance.error"> · {{ upstreamSyncError(target.balance.error, t) }}</span></p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
import UpstreamHistoryBar from './UpstreamHistoryBar.vue'
import UpstreamStatusBadge from './UpstreamStatusBadge.vue'
import UpstreamRateBadge from './UpstreamRateBadge.vue'
import { upstreamSyncError } from './newapi'
import { amount, availability, availabilityColor, compactTokens, latency, latencyColor, money, targetStatus } from './format'

const props = defineProps<{ target: UpstreamTarget; busy: boolean; running: boolean }>()
const emit = defineEmits<{ details: [target: UpstreamTarget, model: string, record?: UpstreamHistoryRecord]; run: [target: UpstreamTarget]; toggle: [target: UpstreamTarget]; edit: [target: UpstreamTarget]; delete: [target: UpstreamTarget] }>()
const { t } = useI18n()
const selectedModel = ref(props.target.models[0] || '')
watch(() => props.target.models, models => { if (!models.includes(selectedModel.value)) selectedModel.value = models[0] || '' })
const modelOptions = computed(() => props.target.models.map(model => ({ value: model, label: `${model} · ${t(`upstreamCenter.status.${targetStatus(props.target, model)}`)}` })))
const statistics = computed(() => props.target.statistics?.find(item => item.model === selectedModel.value))
const modelIssues = computed(() => props.target.models.filter(model => ['degraded', 'failed', 'error', 'stale'].includes(targetStatus(props.target, model))).length)
</script>

<style scoped>
.model-select :deep(.select-trigger) { @apply min-w-0 gap-1 rounded-md px-2 py-[3px] text-[11px] leading-4 text-gray-600 dark:text-gray-300; }
.model-select :deep(.select-icon svg) { @apply h-3 w-3; }
.group-row { container-type: inline-size; padding: 14px 18px; }
.group-body { display: grid; gap: 16px; min-width: 0; align-items: center; }
.monitor-panel { @apply min-w-0 rounded-lg bg-gray-50/80 px-3 py-2.5 dark:bg-dark-900/40; }
.monitor-overview { display: grid; gap: 8px; margin-bottom: 10px; }
.monitor-measurements { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; }
.monitor-measurement { display: flex; min-width: 0; flex-wrap: wrap; align-items: baseline; gap: 2px 8px; }
.monitor-measurement > span { display: block; }
.metric-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; align-content: center; }
.action { @apply flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:opacity-40 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400; }
.metric-label { @apply text-[10px] leading-4 text-gray-400 dark:text-dark-400; }
.metric-value { @apply mt-1 truncate text-xl font-semibold tabular-nums tracking-tight text-gray-900 dark:text-gray-100; }
.secondary-value { @apply mt-1 truncate text-base font-medium tabular-nums tracking-tight text-gray-600 dark:text-gray-300; }
@container (min-width: 540px) { .metric-grid { grid-template-columns: repeat(6, minmax(0, 1fr)); } }
@container (min-width: 660px) {
  .group-body { grid-template-columns: minmax(0, 1fr) minmax(280px, 40%); gap: 20px; }
  .metric-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); column-gap: 12px; padding-left: 16px; border-left: 1px solid; @apply border-gray-100 dark:border-dark-700; }
  .monitor-measurement { display: block; }
  .monitor-measurement strong { display: block; margin-top: 2px; }
}
@container (min-width: 1020px) {
  .group-body { grid-template-columns: minmax(0, 1fr) minmax(360px, 40%); gap: 24px; }
  .monitor-overview { grid-template-columns: minmax(110px, 1fr) minmax(0, 2fr); align-items: center; column-gap: 28px; }
  .monitor-model { display: grid; gap: 6px; justify-content: start; }
  .monitor-measurements { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 24px; }
  .metric-grid { column-gap: 20px; padding-left: 24px; }
}
@media (max-width: 639px) { .group-row { padding: 14px 12px; } }
</style>
