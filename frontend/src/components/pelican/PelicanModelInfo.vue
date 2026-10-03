<template>
  <span class="model-info" :class="compact ? 'model-info-compact' : 'model-info-detail'" :title="description" :aria-label="description">
    <span class="model-summary">
      <span class="model-name">{{ pelicanModelLabel(model) }}</span>
      <span class="model-reasoning">
        <span v-if="compact" class="model-separator" aria-hidden="true">·</span>
        <span class="model-effort">{{ effortLabel }}</span>
        <span v-if="!compact" class="model-effort-label">{{ t('pelicanMonitor.reasoningShort') }}</span>
      </span>
    </span>
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { pelicanModelLabel } from '@/utils/pelicanModels'

const props = defineProps<{ model?: string; effort?: string; compact?: boolean }>()
const { t } = useI18n()
const effortLabel = computed(() => props.effort ? props.effort.charAt(0).toUpperCase() + props.effort.slice(1) : '—')
const description = computed(() => `${t('pelicanMonitor.model')}: ${pelicanModelLabel(props.model)} · ${t('pelicanMonitor.reasoning')}: ${effortLabel.value}`)
</script>
<style scoped>
.model-info { min-width:0; max-width:100%; }
.model-summary { display:flex; min-width:0; align-items:center; justify-content:space-between; gap:8px; }
.model-name { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.model-reasoning { display:inline-flex; flex:none; align-items:center; gap:3px; white-space:nowrap; }
.model-info-detail { @apply bg-primary-50/70 dark:bg-primary-500/[0.06]; display:block; padding:10px; border-radius:10px; }
.model-info-detail .model-name { @apply text-gray-800 dark:text-gray-100; font-size:12px; font-weight:600; line-height:20px; letter-spacing:-.02em; }
.model-info-detail .model-reasoning { @apply bg-primary-100/70 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300; padding:2px 6px; border-radius:5px; font-size:9px; line-height:14px; }
.model-info-detail .model-effort { font-weight:600; }
.model-info-detail .model-effort-label { font-weight:400; }
.model-info-compact { display:inline-flex; line-height:16px; }
.model-info-compact .model-summary { gap:5px; }
.model-info-compact .model-name { @apply text-gray-500 dark:text-gray-400; font-size:10px; font-weight:500; }
.model-info-compact .model-reasoning { gap:5px; }
.model-separator { @apply text-gray-300 dark:text-dark-600; font-size:10px; }
.model-info-compact .model-effort { @apply text-gray-400 dark:text-gray-400; font-size:9px; font-weight:400; }
</style>
