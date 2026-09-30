<template>
  <BaseDialog :show="show" :title="t('intelligenceMonitor.publicDisplay.title')" width="wide" motion="fade" :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <div v-if="loading" class="flex min-h-80 items-center justify-center gap-2 text-sm text-gray-400" role="status">
      <Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}
    </div>
    <div v-else-if="!loaded" class="rounded-xl bg-rose-50 p-4 dark:bg-rose-500/10" role="alert">
      <p class="text-sm text-rose-600 dark:text-rose-400">{{ loadError }}</p>
      <button type="button" class="btn btn-secondary btn-sm mt-3" data-testid="retry-public-display" @click="load">{{ t('upstreamCenter.retry') }}</button>
    </div>
    <template v-else>
      <div class="mb-5 flex items-center justify-between gap-4 rounded-xl border px-4 py-3.5" :class="form.enabled ? 'border-primary-200 bg-primary-50/70 dark:border-primary-500/25 dark:bg-primary-500/10' : 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900/30'">
        <div>
          <label for="intelligence-public-enabled" class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('intelligenceMonitor.publicDisplay.enabled') }}</label>
          <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t('intelligenceMonitor.publicDisplay.enabledHint') }}</p>
        </div>
        <Toggle id="intelligence-public-enabled" v-model="form.enabled" :disabled="saving" :aria-label="t('intelligenceMonitor.publicDisplay.enabled')" />
      </div>
      <div class="grid gap-6 md:grid-cols-2">
        <section class="min-w-0 space-y-4">
          <div class="flex items-center gap-2 text-sm font-semibold text-gray-800 dark:text-gray-100"><Icon name="edit" size="sm" class="text-gray-400" />{{ t('intelligenceMonitor.publicDisplay.copy') }}</div>
          <div v-for="field in fields" :key="field.key">
            <div class="mb-1.5 flex items-center justify-between gap-2">
              <label :for="`intelligence-public-${field.key}`" class="text-xs font-medium text-gray-700 dark:text-gray-200">{{ t(`intelligenceMonitor.publicDisplay.${field.key === 'title' ? 'pageTitle' : field.key}`) }}</label>
              <span class="text-[10px] tabular-nums" :class="fieldValid(field.key, field.max) ? 'text-gray-400' : 'text-rose-500'">{{ length(form[field.key]) }}/{{ field.max }}</span>
            </div>
            <input v-if="field.key === 'title'" :id="`intelligence-public-${field.key}`" v-model="form[field.key]" class="input !text-sm" :maxlength="field.max" :disabled="saving" :placeholder="t('intelligenceMonitor.publicDisplay.titlePlaceholder')" :aria-invalid="!fieldValid(field.key, field.max)" />
            <textarea v-else :id="`intelligence-public-${field.key}`" v-model="form[field.key]" class="input resize-y !text-sm leading-5" :rows="field.key === 'notice' ? 4 : 2" :maxlength="field.max" :disabled="saving" :placeholder="t(`intelligenceMonitor.publicDisplay.${field.key}Placeholder`)" :aria-invalid="!fieldValid(field.key, field.max)" />
          </div>
          <p class="text-[11px] leading-5 text-gray-400 dark:text-dark-400">{{ t('intelligenceMonitor.publicDisplay.copyHint') }}</p>
        </section>
        <section class="min-w-0">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h3 class="flex items-center gap-2 text-sm font-semibold text-gray-800 dark:text-gray-100"><Icon name="grid" size="sm" class="text-gray-400" />{{ t('intelligenceMonitor.publicDisplay.groups') }}<span class="rounded-md bg-primary-50 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">{{ form.plan_ids.length }}/200</span></h3>
            <div class="flex items-center gap-3 text-[11px] font-medium">
              <button type="button" class="text-primary-600 hover:text-primary-700 disabled:opacity-40 dark:text-primary-400" :disabled="saving || !plans.length || plans.length > 200" data-testid="public-display-select-all" @click="selectAll">{{ t('intelligenceMonitor.publicDisplay.selectAll') }}</button>
              <button type="button" class="text-gray-400 hover:text-gray-600 disabled:opacity-40 dark:hover:text-gray-200" :disabled="saving || !form.plan_ids.length" data-testid="public-display-clear" @click="form.plan_ids = []">{{ t('intelligenceMonitor.publicDisplay.clear') }}</button>
            </div>
          </div>
          <div class="max-h-[330px] space-y-2 overflow-y-auto pr-1" data-testid="public-display-plan-list">
            <label v-for="plan in plans" :key="plan.id" class="flex cursor-pointer items-center gap-3 rounded-xl border p-3 transition-colors" :class="selected.has(plan.id) ? 'border-primary-300 bg-primary-50/80 dark:border-primary-500/40 dark:bg-primary-500/10' : 'border-gray-200 hover:border-gray-300 hover:bg-gray-50 dark:border-dark-700 dark:hover:border-dark-600 dark:hover:bg-dark-700/30'" :data-public-plan="plan.id">
              <input type="checkbox" class="h-4 w-4 shrink-0 rounded border-gray-300 accent-teal-600" :checked="selected.has(plan.id)" :disabled="saving || (!selected.has(plan.id) && form.plan_ids.length >= 200)" :aria-label="groupName(plan)" @change="togglePlan(plan.id)" />
              <span class="min-w-0 flex-1"><span class="block truncate text-xs font-semibold text-gray-800 dark:text-gray-100">{{ groupName(plan) }}</span><span class="mt-1 flex items-center gap-2 text-[10px] text-gray-400"><span>#{{ plan.id }}</span><span>{{ t(plan.enabled ? 'intelligenceMonitor.scheduled' : 'intelligenceMonitor.publicDisplay.paused') }}</span></span></span>
              <span class="shrink-0 rounded-md bg-white/80 px-2 py-1 text-xs font-semibold tabular-nums text-primary-700 dark:bg-dark-800 dark:text-primary-300" :title="t('intelligenceMonitor.local.groupRate')">{{ groupRate(plan) }}</span>
            </label>
            <div v-if="!plans.length" class="flex min-h-36 items-center justify-center rounded-xl border border-dashed border-gray-200 px-4 text-center text-xs leading-6 text-gray-400 dark:border-dark-700">{{ t('intelligenceMonitor.publicDisplay.empty') }}</div>
          </div>
          <p class="mt-3 text-[11px] leading-5 text-gray-500 dark:text-dark-300">{{ t('intelligenceMonitor.publicDisplay.visibilityHint') }}</p>
          <div v-if="unavailableIDs.length" class="mt-3 rounded-lg bg-amber-50 p-3 text-xs leading-5 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300" role="alert">
            <p>{{ t('intelligenceMonitor.publicDisplay.unavailable', { ids: unavailableIDs.join('、') }) }}</p>
            <button type="button" class="mt-1 font-semibold underline underline-offset-2" :disabled="saving" data-testid="public-display-remove-unavailable" @click="removeUnavailable">{{ t('intelligenceMonitor.publicDisplay.removeUnavailable') }}</button>
          </div>
        </section>
      </div>
      <p v-if="!valid" class="mt-4 text-xs text-rose-600 dark:text-rose-400" role="alert">{{ t('intelligenceMonitor.publicDisplay.invalid') }}</p>
      <p v-if="saveError" class="mt-4 rounded-xl bg-rose-50 p-3 text-xs text-rose-600 dark:bg-rose-500/10 dark:text-rose-400" role="alert">{{ saveError }}</p>
    </template>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-primary" :disabled="!loaded || loading || saving || !valid" data-testid="save-public-display" @click="save"><Icon v-if="saving" name="refresh" size="sm" class="mr-1.5 animate-spin" />{{ t('intelligenceMonitor.publicDisplay.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, type IntelligencePlan, type IntelligencePublicDisplaySettings } from '@/api/admin/intelligenceMonitor'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const defaults = (): IntelligencePublicDisplaySettings => ({ enabled: false, title: '', description: '', notice: '', plan_ids: [] })
const form = reactive(defaults())
const plans = ref<IntelligencePlan[]>([])
const loaded = ref(false), loading = ref(false), saving = ref(false)
const loadError = ref(''), saveError = ref('')
const fields = [{ key: 'title', max: 60 }, { key: 'description', max: 240 }, { key: 'notice', max: 1000 }] as const
const length = (value: string) => Array.from(value).length
const fieldValid = (key: 'title' | 'description' | 'notice', max: number) => length(form[key]) <= max
const selected = computed(() => new Set(form.plan_ids))
const available = computed(() => new Set(plans.value.map(plan => plan.id)))
const unavailableIDs = computed(() => form.plan_ids.filter(id => !available.value.has(id)))
const valid = computed(() => fields.every(field => fieldValid(field.key, field.max)) && form.plan_ids.length <= 200 && selected.value.size === form.plan_ids.length && form.plan_ids.every(id => Number.isSafeInteger(id) && id > 0 && available.value.has(id)))
const groupName = (plan: IntelligencePlan) => plan.local_group_name?.trim() || t('intelligenceMonitor.publicDisplay.unnamedGroup')
const groupRate = (plan: IntelligencePlan) => typeof plan.local_group_rate_multiplier === 'number' && Number.isFinite(plan.local_group_rate_multiplier) ? `${plan.local_group_rate_multiplier}×` : '—'
let generation = 0
let controller: AbortController | undefined

function responseValid(settings: IntelligencePublicDisplaySettings): boolean {
  return !!settings && typeof settings.enabled === 'boolean' && fields.every(field => typeof settings[field.key] === 'string') && Array.isArray(settings.plan_ids) && settings.plan_ids.every(id => Number.isSafeInteger(id) && id > 0) && new Set(settings.plan_ids).size === settings.plan_ids.length
}
function close() { if (!saving.value) emit('close') }
function togglePlan(id: number) {
  if (saving.value || !available.value.has(id)) return
  if (selected.value.has(id)) form.plan_ids = form.plan_ids.filter(value => value !== id)
  else if (form.plan_ids.length < 200) form.plan_ids = [...form.plan_ids, id]
}
function selectAll() { if (!saving.value && plans.value.length <= 200) form.plan_ids = plans.value.map(plan => plan.id) }
function removeUnavailable() { if (!saving.value) form.plan_ids = form.plan_ids.filter(id => available.value.has(id)) }
async function load() {
  if (!props.show || loading.value || saving.value) return
  const current = generation
  controller?.abort(); controller = new AbortController()
  loading.value = true; loadError.value = ''; loaded.value = false
  try {
    const [settings, result] = await Promise.all([intelligenceMonitorAPI.publicDisplay(controller.signal), intelligenceMonitorAPI.plans(controller.signal)])
    if (current !== generation || !props.show) return
    if (!responseValid(settings) || !Array.isArray(result.items)) throw new Error(t('intelligenceMonitor.publicDisplay.loadFailed'))
    plans.value = result.items.filter(plan => plan.source_type === 'local_group' && plan.local_group_status === 'active' && Number.isSafeInteger(plan.id) && plan.id > 0)
    Object.assign(form, { ...settings, plan_ids: [...settings.plan_ids] })
    loaded.value = true
  } catch (error) {
    if (current === generation && props.show) loadError.value = extractApiErrorMessage(error, t('intelligenceMonitor.publicDisplay.loadFailed'))
  } finally { if (current === generation) loading.value = false }
}
async function save() {
  if (!props.show || !loaded.value || loading.value || saving.value || !valid.value) return
  const current = generation
  saving.value = true; saveError.value = ''
  try {
    await intelligenceMonitorAPI.updatePublicDisplay({ enabled: form.enabled, title: form.title, description: form.description, notice: form.notice, plan_ids: [...form.plan_ids] })
    if (current !== generation || !props.show) return
    app.showSuccess(t('intelligenceMonitor.publicDisplay.saved'))
    emit('saved'); emit('close')
  } catch (error) {
    if (current === generation && props.show) saveError.value = extractApiErrorMessage(error, t('intelligenceMonitor.publicDisplay.saveFailed'))
  } finally { if (current === generation) saving.value = false }
}
function reset() {
  generation++; controller?.abort(); controller = undefined
  loading.value = false; loaded.value = false; saving.value = false; loadError.value = ''; saveError.value = ''; plans.value = []
  Object.assign(form, defaults())
}
watch(() => props.show, show => { reset(); if (show) void load() }, { immediate: true })
onBeforeUnmount(reset)
</script>
