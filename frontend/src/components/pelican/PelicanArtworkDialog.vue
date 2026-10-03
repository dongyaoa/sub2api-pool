<template>
  <BaseDialog :show="true" :title="groupName" width="extra-wide" motion="fade" @close="emit('close')">
    <div class="pelican-artwork-dialog flex min-h-0 flex-col overflow-hidden rounded-xl border border-gray-100 dark:border-dark-700">
      <PelicanArtworkPreview :run="run" :large="true" class="!aspect-auto !min-h-0 min-w-0 flex-1" />
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-gray-100 bg-white px-4 py-3 text-xs dark:border-dark-700 dark:bg-dark-800"><time class="text-gray-500 dark:text-gray-400" :datetime="run.started_at || run.created_at">{{ pelicanDate(run.started_at || run.created_at, locale) }}</time><div class="flex min-w-0 max-w-full flex-wrap items-center gap-x-4 gap-y-2"><PelicanModelInfo :model="run.model" :effort="run.reasoning_effort" compact /><span class="font-medium tabular-nums text-gray-700 dark:text-gray-200">{{ t('pelicanMonitor.duration') }} {{ pelicanDuration(run) }}</span></div></div>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PelicanRun } from '@/api/pelicanMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PelicanArtworkPreview from './PelicanArtworkPreview.vue'
import PelicanModelInfo from './PelicanModelInfo.vue'
import { pelicanDate, pelicanDuration } from './pelicanFormat'
defineProps<{ run: PelicanRun; groupName: string }>()
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n()
</script>
<style scoped>
.pelican-artwork-dialog { height:min(680px,calc(90vh - 112px)); height:min(680px,calc(90dvh - 112px)); }
</style>
