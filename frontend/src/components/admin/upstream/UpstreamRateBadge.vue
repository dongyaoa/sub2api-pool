<template>
  <span class="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] tabular-nums" :class="known ? outdated ? 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-500/10 dark:text-amber-400' : 'border-gray-200 bg-gray-50 text-gray-600 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300' : 'border-gray-100 text-gray-400 dark:border-dark-700 dark:text-dark-400'" :title="detail">
    {{ t('upstreamCenter.billing.rate') }} <strong class="font-medium">{{ known ? `${billing!.effective_rate_multiplier}×` : '—' }}</strong><Icon v-if="outdated" name="clock" size="xs" />
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UpstreamBillingSnapshot } from '@/api/admin/upstreamCenter'
import { dateTime } from './format'
const props = defineProps<{ billing?: UpstreamBillingSnapshot | null }>()
const { t } = useI18n()
const known = computed(() => props.billing?.effective_rate_multiplier != null)
const outdated = computed(() => !!props.billing && (props.billing.stale || props.billing.status === 'error'))
const detail = computed(() => {
  if (!props.billing || !known.value) return t('upstreamCenter.billing.unavailable')
  const value = (number: number | null) => number == null ? '—' : `${number}×`
  return `${t('upstreamCenter.billing.effective')}: ${value(props.billing.effective_rate_multiplier)}\n${t('upstreamCenter.billing.group')}: ${value(props.billing.group_rate_multiplier)}\n${t('upstreamCenter.billing.user')}: ${value(props.billing.user_rate_multiplier)}\n${t('upstreamCenter.billing.base')}: ${value(props.billing.resolved_rate_multiplier)}\n${t('upstreamCenter.wallet.syncedAt', { time: dateTime(props.billing.synced_at) })}${outdated.value ? `\n${t('upstreamCenter.billing.stale')}` : ''}`
})
</script>
