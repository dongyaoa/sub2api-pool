<template>
  <BaseDialog :show="show && !!item" :title="t('upstreamCenter.storage.removeTitle')" width="narrow" :show-close-button="!busy" :close-on-escape="!busy" @close="!busy && emit('close')">
    <p class="break-words text-sm font-semibold text-gray-900 dark:text-gray-100">{{ item?.name }}</p>
    <div v-if="archiveAllowed" class="mt-4 grid grid-cols-2 gap-2" role="group" :aria-label="t('upstreamCenter.storage.removeMode')">
      <button v-for="option in modes" :key="option" type="button" :disabled="busy" :data-delete-mode="option" :aria-pressed="mode === option" class="rounded-xl border px-3 py-3 text-left text-sm transition-colors disabled:opacity-50" :class="mode === option ? option === 'purge' ? 'border-rose-300 bg-rose-50 text-rose-700 dark:border-rose-700 dark:bg-rose-500/10 dark:text-rose-300' : 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-700 dark:bg-primary-500/10 dark:text-primary-300' : 'border-gray-200 text-gray-500 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-300 dark:hover:bg-dark-700'" @click="mode = option">
        <span class="block font-medium">{{ t(`upstreamCenter.storage.${option}`) }}</span>
        <span class="mt-1 block text-[11px] leading-5 opacity-80">{{ t(`upstreamCenter.storage.${option}Short`) }}</span>
      </button>
    </div>
    <p class="mt-4 text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t(mode === 'archive' ? 'upstreamCenter.storage.archiveHint' : item?.kind === 'intelligence' ? 'upstreamCenter.storage.purgeIntelligenceHint' : 'upstreamCenter.storage.purgeUpstreamHint') }}</p>
    <div v-if="mode === 'purge'" class="mt-4 rounded-xl border border-rose-100 bg-rose-50/70 p-3 dark:border-rose-900 dark:bg-rose-500/10">
      <label :for="inputID" class="mb-2 block text-xs font-medium leading-5 text-rose-700 dark:text-rose-300">{{ t('upstreamCenter.storage.confirmName', { name: item?.name || '' }) }}</label>
      <input :id="inputID" v-model="confirmation" :disabled="busy" autocomplete="off" spellcheck="false" class="input text-sm" :placeholder="item?.name" data-testid="purge-confirm-name" @keydown.enter.prevent="confirm" />
    </div>
    <p v-if="error" role="alert" class="mt-4 rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button type="button" class="btn disabled:opacity-50" :class="mode === 'purge' ? 'bg-rose-600 text-white hover:bg-rose-700' : 'btn-primary'" :disabled="!canConfirm" data-testid="confirm-removal" @click="confirm"><Icon v-if="busy" name="refresh" size="sm" class="mr-1.5 animate-spin" />{{ t(mode === 'purge' ? 'upstreamCenter.storage.purge' : 'upstreamCenter.storage.archive') }}</button>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UpstreamArchiveKind } from '@/api/admin/upstreamCenter'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
const props = withDefaults(defineProps<{ show: boolean; item: { kind: UpstreamArchiveKind; id: number; name: string } | null; busy?: boolean; error?: string; archiveAllowed?: boolean }>(), { busy: false, error: '', archiveAllowed: true })
const emit = defineEmits<{ close: []; confirm: [mode: 'archive' | 'purge'] }>()
const { t } = useI18n()
const modes = ['archive', 'purge'] as const
const mode = ref<'archive' | 'purge'>('archive'), confirmation = ref('')
const inputID = computed(() => `upstream-purge-name-${props.item?.kind || 'item'}-${props.item?.id || 0}`)
const canConfirm = computed(() => props.show && !!props.item && !props.busy && (mode.value === 'archive' || confirmation.value === props.item.name))
watch([() => props.show, () => props.item?.kind, () => props.item?.id, () => props.item?.name, () => props.archiveAllowed], () => { mode.value = props.archiveAllowed ? 'archive' : 'purge'; confirmation.value = '' }, { immediate: true })
watch(mode, () => { confirmation.value = '' })
function confirm() { if (canConfirm.value) emit('confirm', mode.value) }
</script>
