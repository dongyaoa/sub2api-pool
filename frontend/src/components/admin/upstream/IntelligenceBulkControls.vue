<template>
  <div class="inline-flex shrink-0 items-center gap-0.5 rounded-lg border border-gray-200 bg-white p-0.5 dark:border-dark-600 dark:bg-dark-800" :title="t('intelligenceMonitor.bulk.hint')" :aria-busy="!!busy">
    <button type="button" class="bulk-control hover:bg-amber-50 hover:text-amber-700 dark:hover:bg-amber-500/10 dark:hover:text-amber-300" :disabled="!ready || !active || !!busy || !hasEnabled" data-testid="stop-all-monitoring" @click="setEnabled(false)"><Icon :name="busy === 'stop' ? 'refresh' : 'clock'" size="sm" :class="busy === 'stop' && 'animate-spin'" />{{ t('intelligenceMonitor.bulk.stop') }}</button>
    <span class="h-4 w-px bg-gray-100 dark:bg-dark-600" aria-hidden="true" />
    <button type="button" class="bulk-control hover:bg-emerald-50 hover:text-emerald-700 dark:hover:bg-emerald-500/10 dark:hover:text-emerald-300" :disabled="!ready || !active || !!busy || !hasDisabled" data-testid="start-all-monitoring" @click="setEnabled(true)"><Icon :name="busy === 'start' ? 'refresh' : 'play'" size="sm" :class="busy === 'start' && 'animate-spin'" />{{ t('intelligenceMonitor.bulk.start') }}</button>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, type IntelligencePlan } from '@/api/admin/intelligenceMonitor'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ plans: IntelligencePlan[]; ready: boolean; active: boolean }>()
const emit = defineEmits<{ changed: [enabled: boolean] }>()
const { t } = useI18n()
const app = useAppStore()
const busy = ref<false | 'stop' | 'start'>(false)
const hasEnabled = computed(() => props.plans.some(plan => plan.enabled))
const hasDisabled = computed(() => props.plans.some(plan => !plan.enabled))
let disposed = false
async function setEnabled(enabled: boolean) {
  if (!props.ready || !props.active || disposed || busy.value || !(enabled ? hasDisabled.value : hasEnabled.value)) return
  busy.value = enabled ? 'start' : 'stop'
  try {
    await intelligenceMonitorAPI.setAllEnabled(enabled)
    if (disposed || !props.active) return
    emit('changed', enabled)
    app.showSuccess(t(enabled ? 'intelligenceMonitor.bulk.started' : 'intelligenceMonitor.bulk.stopped'))
  } catch (err) {
    if (!disposed && props.active) app.showError(extractApiErrorMessage(err, t('intelligenceMonitor.actionFailed')))
  } finally { if (!disposed) busy.value = false }
}
onBeforeUnmount(() => { disposed = true })
</script>
<style scoped>
.bulk-control { @apply inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium text-gray-500 transition-colors disabled:cursor-not-allowed disabled:opacity-40 dark:text-dark-300; }
</style>
