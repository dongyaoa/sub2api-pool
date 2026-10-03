<template>
  <PelicanModelInfo :model="model" :effort="effort" class="intelligence-model-info" />
</template>

<script setup lang="ts">
import PelicanModelInfo from '@/components/pelican/PelicanModelInfo.vue'

defineProps<{ model?: string; effort?: string }>()
</script>

<style scoped>
/* Keep the public presentation intact; only admin cards use this smaller size. */
.intelligence-model-info.model-info-detail { padding:5px 8px; border-radius:8px; }
.intelligence-model-info.model-info-detail :deep(.model-summary) { gap:6px; }
.intelligence-model-info.model-info-detail :deep(.model-name) { font-size:11px; line-height:18px; }
.intelligence-model-info.model-info-detail :deep(.model-reasoning) { gap:2px; padding:1px 5px; border-radius:4px; font-size:9px; line-height:14px; }
</style>
