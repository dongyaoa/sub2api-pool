<template>
  <BaseDialog :show="show" :title="t('intelligenceMonitor.concurrency.title')" motion="fade" :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <p class="text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t('intelligenceMonitor.concurrency.scope') }}</p>
    <div v-if="loading" class="flex items-center justify-center gap-2 py-16 text-sm text-gray-400" role="status">
      <Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}
    </div>
    <div v-else-if="!settings && loadError" class="mt-4 rounded-xl bg-rose-50 p-4 dark:bg-rose-500/10" role="alert">
      <p class="text-sm text-rose-600 dark:text-rose-400">{{ loadError }}</p>
      <button type="button" class="btn btn-secondary btn-sm mt-3" @click="load()">{{ t('upstreamCenter.retry') }}</button>
    </div>
    <template v-else-if="settings">
      <div class="mt-4 grid gap-3 sm:grid-cols-2">
        <section v-for="field in fields" :key="field.key" class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <label :for="`intelligence-concurrency-${field.key}`" class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t(`intelligenceMonitor.concurrency.${field.label}`) }}</label>
          <div class="mt-3 flex items-center gap-2">
            <input :id="`intelligence-concurrency-${field.key}`" v-model="form[field.key]" type="number" min="1" :max="field.max" step="1" inputmode="numeric" class="input !w-24 !text-lg !font-semibold tabular-nums" :disabled="saving" :aria-invalid="!validValue(form[field.key], field.max)" :aria-describedby="`intelligence-concurrency-range-${field.key}`" />
            <span :id="`intelligence-concurrency-range-${field.key}`" class="text-[11px] text-gray-400 dark:text-dark-400">{{ t('intelligenceMonitor.concurrency.range', { max: field.max }) }}</span>
          </div>
          <div class="mt-3 grid grid-cols-4 gap-1.5" role="group" :aria-label="t(`intelligenceMonitor.concurrency.${field.label}`)">
            <button v-for="value in field.presets" :key="value" type="button" class="rounded-lg border px-1 py-1.5 text-xs font-medium tabular-nums transition-colors disabled:opacity-50" :class="Number(form[field.key]) === value ? 'border-primary-200 bg-primary-50 text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/15 dark:text-primary-300' : 'border-gray-200 text-gray-500 hover:border-primary-300 hover:text-primary-600 dark:border-dark-600 dark:text-dark-300'" :aria-pressed="Number(form[field.key]) === value" :disabled="saving" :data-testid="`concurrency-${field.label}-${value}`" @click="form[field.key] = value">{{ value }}</button>
          </div>
          <div class="mt-4 flex items-center justify-between gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
            <span class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.concurrency.running') }} <strong class="ml-1 text-base font-semibold tabular-nums text-primary-600 dark:text-primary-400" :data-testid="`${field.label}-running`">{{ settings[field.running] }}</strong></span>
            <span class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.concurrency.pending') }} <strong class="ml-1 text-base font-semibold tabular-nums" :class="settings[field.pending] > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-gray-500 dark:text-dark-300'" :data-testid="`${field.label}-pending`">{{ settings[field.pending] }}</strong></span>
          </div>
        </section>
      </div>
      <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-[11px] text-gray-400 dark:text-dark-400">
        <span>{{ t(`intelligenceMonitor.concurrency.sources.${settings.source}`) }}</span>
        <button type="button" class="flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-50 dark:text-dark-300 dark:hover:bg-dark-700" :disabled="refreshing || saving" data-testid="refresh-concurrency" @click="load(true)"><Icon name="refresh" size="sm" :class="refreshing && 'animate-spin'" />{{ t('intelligenceMonitor.concurrency.refresh') }}</button>
      </div>
      <div class="mt-3 space-y-1 rounded-xl bg-gray-50 px-3 py-2.5 text-[11px] leading-5 text-gray-500 dark:bg-dark-900/60 dark:text-dark-300">
        <p>{{ t('intelligenceMonitor.concurrency.immediate') }}</p>
        <p>{{ t('intelligenceMonitor.concurrency.runningHint') }}</p>
      </div>
      <p v-if="!valid" role="alert" class="mt-3 text-xs text-rose-600 dark:text-rose-400">{{ t('intelligenceMonitor.concurrency.invalid') }}</p>
      <p v-if="loadError || saveError" role="alert" class="mt-3 text-xs text-rose-600 dark:text-rose-400">{{ loadError || saveError }}</p>
    </template>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-primary" :disabled="!settings || loading || refreshing || saving || !valid" data-testid="save-concurrency" @click="save"><Icon v-if="saving" name="refresh" size="sm" class="mr-1.5 animate-spin" />{{ t('intelligenceMonitor.concurrency.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, type IntelligenceConcurrency } from '@/api/admin/intelligenceMonitor'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const app = useAppStore()
const settings = ref<IntelligenceConcurrency | null>(null)
const form = reactive<{ max_concurrency: number | string; candy_max_concurrency: number | string }>({ max_concurrency: 8, candy_max_concurrency: 4 })
const loading = ref(false), refreshing = ref(false), saving = ref(false)
const loadError = ref(''), saveError = ref('')
const fields = [
  { key: 'max_concurrency', label: 'pelican', max: 256, presets: [16, 32, 64, 128], running: 'pelican_running', pending: 'pelican_pending' },
  { key: 'candy_max_concurrency', label: 'candy', max: 128, presets: [8, 16, 32, 64], running: 'candy_running', pending: 'candy_pending' },
] as const
const validValue = (value: number | string, max: number) => Number.isInteger(Number(value)) && Number(value) >= 1 && Number(value) <= max
const valid = computed(() => fields.every(field => validValue(form[field.key], field.max)))
let session = 0
let controller: AbortController | undefined
let pollTimer: ReturnType<typeof setTimeout> | undefined
let backgroundLoading = false
function stopPolling() { clearTimeout(pollTimer); pollTimer = undefined }
function schedulePolling() {
  stopPolling()
  if (props.show && !document.hidden && !saving.value && settings.value) {
    pollTimer = setTimeout(() => void load(true, true), 2000)
  }
}
function close() { if (!saving.value) { stopPolling(); emit('close') } }
async function load(statusOnly = false, background = false) {
  if (!props.show || saving.value || refreshing.value || loading.value || (background && backgroundLoading)) return
  stopPolling()
  const generation = ++session
  controller?.abort(); controller = new AbortController()
  backgroundLoading = background
  if (!background) {
    if (statusOnly) refreshing.value = true
    else loading.value = true
    loadError.value = ''
  }
  try {
    const result = await intelligenceMonitorAPI.concurrency(controller.signal)
    if (generation !== session) return
    settings.value = result
    loadError.value = ''
    if (!statusOnly) Object.assign(form, { max_concurrency: result.max_concurrency, candy_max_concurrency: result.candy_max_concurrency })
  } catch (error) {
    if (generation === session) loadError.value = extractApiErrorMessage(error, t('intelligenceMonitor.concurrency.loadFailed'))
  } finally {
    if (generation === session) { loading.value = false; refreshing.value = false; backgroundLoading = false; controller = undefined; schedulePolling() }
  }
}
async function save() {
  if (!settings.value || !valid.value || loading.value || refreshing.value || saving.value) return
  stopPolling()
  const generation = ++session
  controller?.abort(); controller = undefined; backgroundLoading = false
  saving.value = true; saveError.value = ''; loadError.value = ''
  try {
    await intelligenceMonitorAPI.updateConcurrency({ max_concurrency: Number(form.max_concurrency), candy_max_concurrency: Number(form.candy_max_concurrency) })
    if (generation !== session) return
    app.showSuccess(t('intelligenceMonitor.concurrency.saved'))
    emit('close')
  } catch (error) {
    if (generation === session) saveError.value = extractApiErrorMessage(error, t('intelligenceMonitor.concurrency.saveFailed'))
  } finally { if (generation === session) { saving.value = false; schedulePolling() } }
}
function reset() {
  stopPolling(); backgroundLoading = false
  session++; controller?.abort(); controller = undefined
  loading.value = false; refreshing.value = false; saving.value = false
  settings.value = null; loadError.value = ''; saveError.value = ''
}
function resumePolling() {
  stopPolling()
  if (props.show && !document.hidden && settings.value) void load(true, true)
}
watch(() => props.show, value => { reset(); if (value) void load() }, { immediate: true })
onMounted(() => { document.addEventListener('visibilitychange', resumePolling); window.addEventListener('online', resumePolling) })
onBeforeUnmount(() => { reset(); document.removeEventListener('visibilitychange', resumePolling); window.removeEventListener('online', resumePolling) })
</script>
