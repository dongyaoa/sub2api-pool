<template>
  <span class="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium" :class="statusClasses[status] || statusClasses.unknown">
    <span class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true"></span>{{ t(`upstreamCenter.status.${status in statusClasses ? status : 'unknown'}`) }}
  </span>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { statusClasses } from './format'
defineProps<{ status: string }>()
const { t } = useI18n()
</script>
