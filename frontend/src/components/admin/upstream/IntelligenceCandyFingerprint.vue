<template>
  <section class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700" data-testid="candy-fingerprint">
    <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 bg-gray-50/70 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/30">
      <h4 class="text-xs font-semibold text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.candy.fingerprint.title') }}</h4>
      <span class="rounded-md px-2 py-1 text-[11px] font-semibold" :class="badgeClass" data-testid="fingerprint-verdict">{{ t('intelligenceMonitor.candy.fingerprint.statuses.' + result) }}</span>
    </div>
    <div class="space-y-3 p-4">
      <template v-if="fingerprint">
        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-xs sm:grid-cols-4">
          <div><dt class="text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.model') }}</dt><dd class="mt-1 break-words font-medium text-gray-800 dark:text-gray-100">{{ fingerprint.model }}</dd></div>
          <div><dt class="text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.nearest') }}</dt><dd class="mt-1 break-words font-semibold text-violet-600 dark:text-violet-300">{{ fingerprint.attribution?.nearest || '—' }}</dd></div>
          <div><dt class="text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.progress') }}</dt><dd class="mt-1 font-mono font-semibold tabular-nums text-gray-800 dark:text-gray-100">{{ fingerprint.done }}<span class="font-normal text-gray-400"> / {{ fingerprint.total }}</span></dd></div>
          <div><dt class="text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.valid') }}</dt><dd class="mt-1 font-mono tabular-nums text-gray-800 dark:text-gray-100">{{ fingerprint.valid }}<span v-if="fingerprint.errors" class="ml-2 text-[10px] text-rose-500">{{ t('intelligenceMonitor.candy.fingerprint.errors') }} {{ fingerprint.errors }}</span></dd></div>
        </dl>
        <p class="text-[11px] leading-5 text-gray-500">{{ t('intelligenceMonitor.candy.fingerprint.sampling') }}</p>
        <p v-if="fingerprint.attribution?.message" class="text-xs leading-5 text-gray-600 dark:text-gray-300">{{ fingerprint.attribution.message }}</p>
        <p v-if="fingerprint.error" class="whitespace-pre-wrap break-words text-xs text-rose-600 dark:text-rose-400">{{ fingerprint.error }}</p>
        <div v-if="comparisons.length" class="overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
          <table class="w-full text-left text-[11px]" data-testid="fingerprint-comparisons">
            <caption class="sr-only">{{ t('intelligenceMonitor.candy.fingerprint.comparison') }}</caption>
            <thead class="bg-gray-50 text-gray-500 dark:bg-dark-900/40"><tr><th scope="col" class="px-3 py-2 font-medium">{{ t('intelligenceMonitor.candy.fingerprint.candidate') }}</th><th scope="col" class="whitespace-nowrap px-3 py-2 text-right font-medium">{{ t('intelligenceMonitor.candy.fingerprint.distance') }}</th><th scope="col" class="whitespace-nowrap px-3 py-2 text-right font-medium">{{ t('intelligenceMonitor.candy.fingerprint.pValue') }}</th><th scope="col" class="px-3 py-2 font-medium">{{ t('intelligenceMonitor.candy.fingerprint.result') }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="row in comparisons" :key="row.model" :class="row.model === fingerprint.attribution?.nearest && 'bg-violet-50/60 dark:bg-violet-500/5'"><th scope="row" class="break-words px-3 py-2 font-medium text-gray-700 dark:text-gray-200">{{ row.model }}<span v-if="row.model === fingerprint.model" class="ml-1.5 whitespace-nowrap rounded bg-gray-100 px-1 py-0.5 text-[9px] text-gray-500 dark:bg-dark-700 dark:text-gray-300">{{ t('intelligenceMonitor.candy.fingerprint.model') }}</span></th><td class="px-3 py-2 text-right font-mono tabular-nums text-gray-700 dark:text-gray-200">{{ fingerprintMetric(row.mean_jsd) }}</td><td class="px-3 py-2 text-right font-mono tabular-nums text-gray-700 dark:text-gray-200">{{ fingerprintMetric(row.p_value) }}</td><td class="px-3 py-2 text-gray-500 dark:text-gray-400">{{ comparisonVerdict(row.verdict) }}</td></tr></tbody>
          </table>
        </div>
        <p v-else class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-400 dark:bg-dark-900/40">{{ t('intelligenceMonitor.candy.fingerprint.noComparisons') }}</p>
        <p v-if="comparisons.length" class="text-[10px] leading-5 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.metricHint') }}</p>
        <ul v-if="fingerprint.attribution?.warnings?.length" class="space-y-1 text-[11px] leading-5 text-amber-600 dark:text-amber-400"><li v-for="warning in fingerprint.attribution.warnings" :key="warning">{{ warning }}</li></ul>
      </template>
      <p v-else class="text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.candy.fingerprint.historicalHint') }}</p>
      <p class="text-[10px] leading-5 text-gray-400">{{ t('intelligenceMonitor.candy.fingerprint.disclaimer') }}</p>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IntelligenceFingerprint } from '@/api/admin/intelligenceMonitor'
import { fingerprintMetric, fingerprintResult } from './intelligenceCandy'
const props = defineProps<{ fingerprint?: IntelligenceFingerprint | null }>()
const { t } = useI18n()
const result = computed(() => fingerprintResult(props.fingerprint))
const comparisons = computed(() => [...(props.fingerprint?.attribution?.comparisons || [])].sort((a, b) => (a.mean_jsd ?? Infinity) - (b.mean_jsd ?? Infinity)))
const badgeClass = computed(() => {
  if (result.value === 'consistent' && props.fingerprint?.passed === true) return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300'
  if (['failed', 'timeout', 'substitution', 'different'].includes(result.value)) return 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-300'
  return 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300'
})
function comparisonVerdict(value: string) {
  const key = `intelligenceMonitor.candy.fingerprint.comparisonVerdicts.${value}`
  return ['match', 'uncertain', 'mismatch', 'insufficient'].includes(value) ? t(key) : value || '—'
}
</script>
