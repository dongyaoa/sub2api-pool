<template>
  <article :class="compact ? 'min-w-0 py-4' : 'card min-w-0 p-5'">
    <header class="flex items-start gap-2.5">
      <div v-if="!compact" class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400"><Icon name="chart" size="md" /></div>
      <div class="min-w-0 flex-1">
        <div class="flex items-start justify-between gap-3">
          <button type="button" class="min-w-0 truncate text-left font-semibold leading-5 text-gray-900 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-100 dark:hover:text-primary-400" :class="compact ? 'text-sm' : 'text-base'" :title="target.name" @click="emit('details', target, selectedModel)">{{ target.name }}</button>
          <UpstreamStatusBadge class="shrink-0" :status="targetStatus(target, selectedModel)" />
        </div>
        <div v-if="!compact" class="mt-1 flex min-w-0 items-center gap-1.5">
          <p class="min-w-0 truncate text-[11px] leading-4 text-gray-400 dark:text-dark-400" :title="website || t('upstreamCenter.websiteUnavailable')" data-testid="upstream-website-label">{{ websiteHost || '—' }}</p>
          <a v-if="website" :href="website" target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer" class="inline-flex h-5 shrink-0 items-center gap-1 rounded px-1 text-[10px] font-medium leading-4 text-primary-600 transition-colors hover:bg-primary-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-400 dark:hover:bg-primary-500/10" :title="t('upstreamCenter.visitWebsite')" :aria-label="t('upstreamCenter.visitWebsite')" data-testid="upstream-website"><span>{{ t('upstreamCenter.visitWebsite') }}</span><Icon name="externalLink" size="xs" class="shrink-0" /></a>
        </div>
      </div>
    </header>

    <div class="mt-3 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
      <div class="flex min-w-0 items-center gap-1.5 text-[11px] leading-4 text-gray-400 dark:text-dark-400">
        <label :for="target.models.length > 1 ? `target-model-${target.id}` : undefined" class="shrink-0 text-[10px]">{{ t('upstreamCenter.model') }}</label>
        <select v-if="target.models.length > 1" :id="`target-model-${target.id}`" v-model="selectedModel" class="min-w-0 flex-1 truncate rounded border-0 bg-transparent py-0.5 pl-0 pr-5 text-[11px] leading-4 text-gray-500 outline-none transition-colors hover:text-gray-700 focus:ring-2 focus:ring-primary-500 dark:text-dark-400 dark:hover:text-gray-200" :title="selectedModel"><option v-for="model in target.models" :key="model" :value="model" class="bg-white text-gray-700 dark:bg-dark-800 dark:text-gray-200">{{ model }} · {{ t(`upstreamCenter.status.${targetStatus(target, model)}`) }}</option></select>
        <span v-else class="min-w-0 truncate" :title="selectedModel">{{ selectedModel }}</span>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <span v-if="target.models.length > 1 && modelIssues" class="inline-flex items-center gap-1 text-[11px] font-medium leading-4 tabular-nums text-amber-600 dark:text-amber-400" :title="t('upstreamCenter.modelIssues', { count: modelIssues })" :aria-label="t('upstreamCenter.modelIssues', { count: modelIssues })"><Icon name="exclamationTriangle" size="xs" /><span aria-hidden="true">{{ modelIssues }}</span></span>
        <UpstreamRateBadge class="shrink-0" :billing="target.balance?.billing" data-testid="upstream-rate" />
      </div>
    </div>

    <dl class="mt-3 grid grid-cols-2 gap-4">
      <div class="min-w-0">
        <dt class="text-[10px] leading-4 text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.availability7d') }}</dt>
        <dd class="mt-0.5"><strong class="text-base font-semibold leading-5 tabular-nums text-gray-400" :style="{ color: availabilityColor(statistics?.availability_7d) }" data-testid="upstream-availability">{{ availability(statistics?.availability_7d) }}</strong></dd>
      </div>
      <div class="min-w-0 border-l border-gray-100 pl-4 dark:border-dark-700">
        <dt class="text-[10px] leading-4 text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.latestLatency') }}</dt>
        <dd class="mt-0.5"><strong class="text-base font-semibold leading-5 tabular-nums" :class="latencyColor(statistics?.latest_latency_ms, target.degraded_threshold_ms, target.timeout_seconds, statistics?.status)" data-testid="upstream-latency">{{ latency(statistics?.latest_latency_ms) }}</strong></dd>
      </div>
    </dl>
    <UpstreamHistoryBar class="mt-3" :records="statistics?.timeline || []" :last-checked-at="statistics?.last_checked_at" :legend="!compact" @select="record => emit('details', target, selectedModel, record)" />
    <div v-if="!compact" class="mt-2 flex min-w-0 items-center justify-between gap-2 text-[10px] leading-4 text-gray-400 dark:text-dark-400">
      <span class="shrink-0">{{ t('upstreamCenter.nextCheck') }}</span>
      <time v-if="target.enabled" class="min-w-0 truncate text-right tabular-nums" :datetime="target.next_check_at || undefined" :title="dateTime(target.next_check_at)" data-testid="upstream-next-check">{{ dateTime(target.next_check_at) }}</time>
      <span v-else data-testid="upstream-next-check">{{ t('upstreamCenter.status.paused') }}</span>
    </div>

    <footer :class="compact ? 'mt-3' : 'mt-3 border-t border-gray-100 pt-2 dark:border-dark-700'" class="flex flex-wrap items-center justify-between gap-1.5">
      <div class="flex min-w-0 items-center gap-2">
        <button type="button" class="inline-flex h-8 items-center gap-1.5 rounded-md text-[11px] font-medium text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-50 dark:text-primary-400" :disabled="busy || running" @click="emit('run', target)"><Icon :name="running ? 'refresh' : 'play'" size="xs" :class="running && 'animate-spin'" />{{ t(running ? 'upstreamCenter.running' : 'upstreamCenter.run') }}</button>
        <button v-if="!compact" type="button" class="inline-flex h-8 items-center rounded-md px-1 text-[11px] text-gray-500 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-400 dark:hover:text-primary-400" @click="emit('details', target, selectedModel)">{{ t('upstreamCenter.details') }}</button>
      </div>
      <div class="ml-auto flex shrink-0 items-center gap-0.5">
        <button type="button" class="action" :disabled="busy" :title="t(target.enabled ? 'upstreamCenter.pause' : 'upstreamCenter.resume')" :aria-label="t(target.enabled ? 'upstreamCenter.pause' : 'upstreamCenter.resume')" @click="emit('toggle', target)"><svg v-if="target.enabled" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path stroke-linecap="round" d="M8 5v14M16 5v14" /></svg><Icon v-else name="play" size="sm" /></button>
        <button type="button" class="action" :disabled="busy" :title="t('upstreamCenter.edit')" :aria-label="t('upstreamCenter.edit')" @click="emit('edit', target)"><Icon name="edit" size="sm" /></button>
        <button type="button" class="action hover:!text-rose-500" :disabled="busy" :title="t('upstreamCenter.remove')" :aria-label="t('upstreamCenter.remove')" @click="emit('delete', target)"><Icon name="trash" size="sm" /></button>
      </div>
    </footer>
  </article>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UpstreamHistoryRecord, UpstreamTarget } from '@/api/admin/upstreamCenter'
import Icon from '@/components/icons/Icon.vue'
import UpstreamHistoryBar from './UpstreamHistoryBar.vue'
import UpstreamRateBadge from './UpstreamRateBadge.vue'
import UpstreamStatusBadge from './UpstreamStatusBadge.vue'
import { availability, availabilityColor, dateTime, latency, latencyColor, targetStatus } from './format'
import { safeWebsite } from './safeWebsite'
const props = withDefaults(defineProps<{ target: UpstreamTarget; compact?: boolean; busy?: boolean; running?: boolean }>(), { compact: false, busy: false, running: false })
const emit = defineEmits<{ details: [target: UpstreamTarget, model: string, record?: UpstreamHistoryRecord]; run: [target: UpstreamTarget]; toggle: [target: UpstreamTarget]; edit: [target: UpstreamTarget]; delete: [target: UpstreamTarget] }>()
const { t } = useI18n()
const website = computed(() => safeWebsite(props.target.endpoint))
const websiteHost = computed(() => website.value ? new URL(website.value).host : '')
const selectedModel = ref(props.target.models?.[0] || '')
watch(() => props.target.models, models => { if (!models.includes(selectedModel.value)) selectedModel.value = models[0] || '' })
const statistics = computed(() => props.target.statistics?.find(item => item.model === selectedModel.value))
const modelIssues = computed(() => props.target.models.filter(model => ['degraded', 'failed', 'error', 'stale'].includes(targetStatus(props.target, model))).length)
</script>
<style scoped>
.action { @apply flex h-8 w-8 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-40 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400; }
</style>
