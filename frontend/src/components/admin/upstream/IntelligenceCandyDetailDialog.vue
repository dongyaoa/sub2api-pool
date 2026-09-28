<template>
  <BaseDialog :show="true" :title="t('intelligenceMonitor.candy.detail')" width="wide" motion="fade" @close="close">
    <div class="candy-detail flex min-h-0 flex-col gap-4" data-testid="candy-detail">
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-2">
        <div><p class="text-sm font-semibold" :class="tone === 'success' ? 'text-emerald-600 dark:text-emerald-400' : tone === 'error' ? 'text-rose-600 dark:text-rose-400' : tone === 'warning' ? 'text-amber-600 dark:text-amber-400' : 'text-gray-500'" data-testid="candy-combined-verdict">{{ t('intelligenceMonitor.candy.' + result) }}</p><p class="mt-1 text-xs text-gray-500">{{ dateTime(display.started_at || display.created_at) }}</p></div>
        <button type="button" class="btn btn-secondary btn-sm" :aria-busy="loading" @click="load"><Icon name="refresh" size="sm" class="mr-1.5" :class="loading && 'animate-spin'" />{{ t('intelligenceMonitor.refresh') }}</button>
      </div>
      <div class="min-h-0 flex-1 space-y-4 overflow-y-auto pr-1" data-testid="candy-detail-scroll">
      <dl class="grid grid-cols-2 gap-3 rounded-xl bg-gray-50 p-3 text-xs dark:bg-dark-900/40 sm:grid-cols-4">
        <div><dt class="text-gray-400">{{ t('intelligenceMonitor.model') }}</dt><dd class="mt-1 break-words font-medium text-gray-700 dark:text-gray-200">{{ display.model }} · {{ display.reasoning_effort }}</dd></div>
        <div><dt class="text-gray-400">{{ t('intelligenceMonitor.candy.answer') }}</dt><dd class="mt-1 break-words font-semibold">{{ display.answer || '—' }}<span class="ml-2 text-[10px] font-medium" :class="display.correct === true ? 'text-emerald-600 dark:text-emerald-400' : display.correct === false ? 'text-rose-600 dark:text-rose-400' : 'text-gray-400'">{{ t('intelligenceMonitor.candy.' + candyAnswerResult(display)) }}</span></dd></div>
        <div><dt class="text-gray-400">{{ t('intelligenceMonitor.totalDuration') }}</dt><dd class="mt-1 tabular-nums">{{ intelligenceDurationLabel(display, t) || '—' }}</dd></div>
        <div><dt class="text-gray-400">{{ t('intelligenceMonitor.http') }}</dt><dd class="mt-1 tabular-nums">{{ display.http_status ?? '—' }}</dd></div>
      </dl>
      <p v-if="display.error" class="max-h-20 shrink-0 overflow-auto whitespace-pre-wrap break-words text-xs text-rose-600 dark:text-rose-400">{{ display.error }}</p>
      <IntelligenceExecutionSource :run="display" class="!mt-0 max-h-40 shrink-0 overflow-y-auto" />
      <p v-if="error" role="alert" class="shrink-0 text-xs text-rose-600">{{ error }}</p>
      <IntelligenceCandyFingerprint :fingerprint="display.fingerprint" />
      <section class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700">
        <h4 class="shrink-0 border-b border-gray-100 px-4 py-2 text-[11px] font-medium text-gray-500 dark:border-dark-700">{{ t('intelligenceMonitor.candy.response') }}</h4>
        <div v-if="loading && !detail" class="flex min-h-32 items-center justify-center text-xs text-gray-400" role="status">{{ t('common.loading') }}</div>
        <pre v-else class="min-h-32 whitespace-pre-wrap break-words p-4 font-mono text-xs leading-6 text-gray-700 dark:text-gray-200" data-testid="candy-response">{{ detail?.raw_text || t('intelligenceMonitor.candy.noResponse') }}</pre>
      </section>
      <p class="shrink-0 text-[10px] leading-5 text-gray-400">{{ t('intelligenceMonitor.candy.scoringHint') }}</p>
      </div>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import { extractApiErrorMessage } from '@/utils/apiError'
import { candyAnswerResult, candyResult, candyResultTone } from './intelligenceCandy'
import { intelligenceDurationLabel } from './intelligenceDuration'
import { dateTime } from './format'
import IntelligenceExecutionSource from './IntelligenceExecutionSource.vue'
import IntelligenceCandyFingerprint from './IntelligenceCandyFingerprint.vue'
const props = defineProps<{ run: IntelligenceRun }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const detail = ref<IntelligenceRun | null>(null), loading = ref(false), error = ref('')
const display = computed(() => detail.value || props.run)
const result = computed(() => candyResult(display.value))
const tone = computed(() => candyResultTone(display.value))
let controller: AbortController | undefined, closed = false
function close() { closed = true; controller?.abort(); emit('close') }
async function load() {
  if (closed) return
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  const id = props.run.id
  try {
    const response = await intelligenceMonitorAPI.detail(id, current.signal)
    if (closed || current.signal.aborted || controller !== current) return
    if (response.id !== id || response.plan_id !== props.run.plan_id || response.test_kind !== 'candy') throw new Error(t('intelligenceMonitor.loadFailed'))
    detail.value = response
  } catch (cause) {
    if (!closed && !current.signal.aborted && controller === current) error.value = extractApiErrorMessage(cause, t('intelligenceMonitor.loadFailed'))
  } finally { if (controller === current && !closed) loading.value = false }
}
watch([() => props.run.id, () => props.run.status, () => props.run.finished_at, () => props.run.correct, () => props.run.answer, () => {
  const fp = props.run.fingerprint
  return [fp?.status, fp?.passed, fp?.done, fp?.valid, fp?.errors, fp?.attribution?.status, fp?.attribution?.nearest, fp?.attribution?.reference_p_value].join('|')
}], (value, previous) => { if (value[0] !== previous?.[0]) detail.value = null; void load() }, { immediate: true })
onBeforeUnmount(() => { closed = true; controller?.abort() })
</script>
<style scoped>
.candy-detail { height: min(560px, calc(90vh - 112px)); height: min(560px, calc(90dvh - 112px)); }
</style>
