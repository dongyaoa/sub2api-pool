<template>
  <BaseDialog :show="true" :title="t(artwork ? 'intelligenceMonitor.deleteArtworkTitle' : 'intelligenceMonitor.deleteOAuthTitle')" width="narrow" motion="fade" :close-on-escape="!busy" @close="!busy && emit('close')">
    <div class="flex items-start gap-3">
      <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-rose-50 text-rose-500 dark:bg-rose-500/10"><Icon name="trash" size="md" /></div>
      <p class="pt-1 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t(artwork ? 'intelligenceMonitor.deleteArtworkHint' : 'intelligenceMonitor.deleteOAuthHint', { name }) }}</p>
    </div>
    <p v-if="error" role="alert" class="mt-3 rounded-lg bg-rose-50 px-3 py-2 text-xs text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-sm bg-rose-600 text-white hover:bg-rose-700 disabled:opacity-50" :disabled="busy" data-testid="confirm-permanent-delete" @click="emit('confirm')"><Icon :name="busy ? 'refresh' : 'trash'" size="sm" class="mr-1.5" :class="busy && 'animate-spin'" />{{ t('intelligenceMonitor.permanentDelete') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
defineProps<{ name?: string; artwork?: boolean; busy: boolean; error?: string }>()
const emit = defineEmits<{ close: []; confirm: [] }>()
const { t } = useI18n()
</script>
