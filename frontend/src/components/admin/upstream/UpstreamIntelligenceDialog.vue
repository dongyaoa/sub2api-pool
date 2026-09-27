<template>
  <BaseDialog :show="true" :title="t('intelligenceMonitor.groupMonitor.title', { name: target.name })" width="extra-wide" motion="fade" :close-on-escape="!childOpen" show-close-button @close="close">
    <div class="group-intelligence-content flex min-h-0 flex-col gap-4" data-testid="group-intelligence-content">
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-100 bg-gray-50/70 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/40">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400"><Icon name="lightbulb" size="sm" /></div>
          <div class="min-w-0"><p class="truncate text-sm font-semibold text-gray-800 dark:text-gray-100">{{ target.name }}</p><p class="mt-0.5 truncate font-mono text-[10px] text-gray-400" :title="target.endpoint">{{ target.endpoint }}</p></div>
        </div>
        <div class="flex shrink-0 items-center gap-3">
          <span class="hidden text-[11px] text-gray-400 sm:inline">{{ PELICAN_MODEL }} · {{ PELICAN_REASONING }}</span>
          <button type="button" class="inline-flex items-center gap-1.5 rounded-lg px-2 py-2 text-xs font-medium text-gray-500 hover:bg-white hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400" :aria-busy="loading" data-testid="group-intelligence-refresh" @click="manualRefresh"><Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />{{ t('intelligenceMonitor.refresh') }}</button>
        </div>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain [scrollbar-gutter:stable]" data-testid="group-intelligence-viewport" :aria-busy="loading && !loaded">
        <p v-if="error" role="alert" class="mb-3 rounded-xl bg-rose-50 px-4 py-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
        <div v-if="!loaded && !error" class="h-full animate-pulse rounded-2xl border border-gray-100 bg-gray-50 motion-reduce:animate-none dark:border-dark-700 dark:bg-dark-800" data-testid="group-intelligence-loading" />
        <div v-if="plans.length" class="space-y-3">
          <IntelligencePlanCard v-for="plan in plans" :key="plan.id" :plan="plan" :overview="overview" :busy="busy.has(plan.id)" :visible="!childOpen" @run="run(plan)" @candy-run="runCandy(plan)" @candy-select="openCandy" @toggle="toggle(plan)" @edit="openEditor(plan)" @history="runID => openHistory(plan, runID)" @archive="openArchive(plan)" />
        </div>
        <div v-else-if="loaded && !error" class="flex min-h-full flex-col items-center justify-center rounded-2xl border border-dashed border-gray-200 px-6 py-6 text-center dark:border-dark-700" data-testid="group-intelligence-empty">
          <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-50 text-primary-500 dark:bg-primary-500/10"><Icon name="lightbulb" size="xl" /></div>
          <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('intelligenceMonitor.groupMonitor.emptyTitle') }}</h4>
          <p class="mt-2 max-w-md text-xs leading-6 text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.groupMonitor.emptyHint') }}</p>
          <button type="button" class="btn btn-primary btn-sm mt-4" :disabled="loading" data-testid="group-intelligence-create" @click="openEditor()"><Icon name="plus" size="sm" class="mr-1.5" />{{ t('intelligenceMonitor.groupMonitor.create') }}</button>
        </div>
      </div>
      <div class="flex shrink-0 flex-wrap justify-between gap-2 text-[10px] leading-5 text-gray-400 dark:text-dark-400"><span>{{ t('intelligenceMonitor.groupMonitor.sharedHint') }}</span><span>{{ t('intelligenceMonitor.retention') }}</span></div>
    </div>
  </BaseDialog>
  <IntelligencePlanDialog v-if="editor" :show="true" :plan="editing" :overview="overview" :upstream-target-id="target.id" @close="closeEditor" @saved="saved" />
  <IntelligenceHistoryDialog v-if="history" :show="true" :plan="selectedPlan" :initial-run-id="selectedRunID" @close="history = false" />
  <IntelligenceCandyDetailDialog v-if="selectedCandyRun" :run="selectedCandyRun" @close="selectedCandy = null" />
  <UpstreamDeleteDialog v-if="archiving" :show="true" :item="{ kind: 'intelligence', id: archiving.id, name: archiving.name }" :busy="deleting" :error="deleteError" @close="!deleting && (archiving = null)" @confirm="archive" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, provide, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, PELICAN_MODEL, PELICAN_REASONING, type IntelligencePlan, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import { upstreamCenterAPI, type UpstreamOverview, type UpstreamTarget } from '@/api/admin/upstreamCenter'
import { useAppStore } from '@/stores/app'
import { useMonitorRefresh } from '@/composables/useMonitorRefresh'
import { extractApiErrorMessage } from '@/utils/apiError'
import IntelligencePlanCard from './IntelligencePlanCard.vue'
import IntelligencePlanDialog from './IntelligencePlanDialog.vue'
import IntelligenceHistoryDialog from './IntelligenceHistoryDialog.vue'
import IntelligenceCandyDetailDialog from './IntelligenceCandyDetailDialog.vue'
import { isIntelligencePlanActive, isIntelligenceRunActive } from './intelligenceCandy'
import UpstreamDeleteDialog from './UpstreamDeleteDialog.vue'
import { intelligencePanelActiveKey, intelligencePreviewRefreshKey } from './intelligenceMonitorContext'
import { reconcileMonitorData } from './monitorReconcile'

const props = defineProps<{ target: UpstreamTarget; overview: UpstreamOverview | null }>()
const emit = defineEmits<{ close: []; changed: []; refreshOverview: [] }>()
const { t } = useI18n()
const app = useAppStore()
const plans = ref<IntelligencePlan[]>([]), loaded = ref(false), error = ref('')
const busy = ref(new Set<number>()), closed = ref(false), previewRefresh = ref(0)
const editor = ref(false), editing = ref<IntelligencePlan | null>(null)
const history = ref(false), selectedID = ref<number | null>(null), selectedRunID = ref<number | null>(null)
const archiving = ref<IntelligencePlan | null>(null), deleting = ref(false), deleteError = ref('')
const selectedCandy = ref<IntelligenceRun | null>(null)
const childOpen = computed(() => editor.value || history.value || !!archiving.value || !!selectedCandy.value)
const selectedPlan = computed(() => plans.value.find(plan => plan.id === selectedID.value) || null)
const selectedCandyRun = computed(() => {
  const selected = selectedCandy.value
  if (!selected) return null
  const plan = plans.value.find(item => item.id === selected.plan_id)
  return [plan?.candy_latest_run, ...(plan?.candy_recent_runs || [])].find(run => run?.id === selected.id) || selected
})
let disposed = false
const live = () => !disposed && !closed.value
const belongs = (plan: IntelligencePlan) => plan.source_type === 'upstream' && plan.upstream_target_id === props.target.id
provide(intelligencePanelActiveKey, computed(() => !closed.value))
provide(intelligencePreviewRefreshKey, previewRefresh)

const { loading, refresh } = useMonitorRefresh({
  active: () => !closed.value,
  intervalMs: () => plans.value.some(isIntelligencePlanActive) ? 2000 : 5000,
  request: signal => intelligenceMonitorAPI.plans(signal, props.target.id),
  apply: result => {
    if (!live()) return
    plans.value = reconcileMonitorData(plans.value, (result.items || []).filter(belongs))
    loaded.value = true
    error.value = ''
  },
  onError: cause => { if (live()) error.value = extractApiErrorMessage(cause, t('intelligenceMonitor.loadFailed')) },
})
function close() {
  if (!live() || childOpen.value) return
  closed.value = true
  emit('close')
}
function manualRefresh() {
  if (!live()) return
  previewRefresh.value++
  emit('refreshOverview')
  void refresh()
}
function openEditor(plan: IntelligencePlan | null = null) {
  if (!live() || childOpen.value || (!plan && (!loaded.value || loading.value || error.value || plans.value.length))) return
  editing.value = plan
  editor.value = true
}
function closeEditor() {
  if (!live() || !editor.value) return
  editor.value = false
  editing.value = null
  void refresh()
}
function openHistory(plan: IntelligencePlan, runID?: number) {
  if (!live() || childOpen.value) return
  selectedID.value = plan.id
  selectedRunID.value = runID ?? null
  history.value = true
}
function openCandy(run: IntelligenceRun) {
  if (!live() || childOpen.value) return
  selectedCandy.value = run
}
function openArchive(plan: IntelligencePlan) {
  if (!live() || childOpen.value) return
  deleteError.value = ''
  archiving.value = plan
}
function saved(plan?: IntelligencePlan) {
  if (!live()) return
  editor.value = false
  if (plan && belongs(plan)) {
    const previous = plans.value.find(item => item.id === plan.id)
    const updated = previous ? { ...previous, ...plan, latest_run: plan.latest_run || previous.latest_run, recent_runs: plan.recent_runs || previous.recent_runs, candy_latest_run: plan.candy_latest_run || previous.candy_latest_run, candy_recent_runs: plan.candy_recent_runs || previous.candy_recent_runs } : plan
    plans.value = previous ? plans.value.map(item => item.id === plan.id ? updated : item) : [...plans.value, updated]
    loaded.value = true
  }
  app.showSuccess(t('intelligenceMonitor.saved'))
  emit('changed')
  previewRefresh.value++
  void refresh()
}
async function action(plan: IntelligencePlan, callback: () => Promise<void>) {
  if (!live() || busy.value.has(plan.id)) return
  busy.value = new Set([...busy.value, plan.id])
  try {
    await callback()
    if (!live()) return
    emit('changed')
    previewRefresh.value++
    await refresh()
  } catch (cause) {
    if (live()) app.showError(extractApiErrorMessage(cause, t('intelligenceMonitor.actionFailed')))
  } finally {
    if (live()) { const next = new Set(busy.value); next.delete(plan.id); busy.value = next }
  }
}
function run(plan: IntelligencePlan) {
  if (isIntelligenceRunActive((plans.value.find(item => item.id === plan.id) || plan).latest_run)) return
  void action(plan, async () => {
    const queued = await intelligenceMonitorAPI.run(plan.id)
    if (!live()) return
    plans.value = plans.value.map(item => item.id === plan.id ? { ...item, latest_run: queued } : item)
    app.showSuccess(t('intelligenceMonitor.queued'))
  })
}
function runCandy(plan: IntelligencePlan) {
  const current = plans.value.find(item => item.id === plan.id)
  if (!current?.candy_enabled || isIntelligenceRunActive(current.candy_latest_run)) return
  void action(plan, async () => {
    const queued = await intelligenceMonitorAPI.runCandy(plan.id)
    if (!live()) return
    plans.value = plans.value.map(item => item.id === plan.id ? { ...item, candy_latest_run: queued } : item)
    app.showSuccess(t('intelligenceMonitor.candy.queued'))
  })
}
function toggle(plan: IntelligencePlan) {
  void action(plan, async () => {
    const updated = await intelligenceMonitorAPI.update(plan.id, { enabled: !plan.enabled })
    if (!live()) return
    plans.value = plans.value.map(item => item.id === plan.id ? { ...item, enabled: updated.enabled, next_run_at: updated.next_run_at, candy_next_run_at: updated.candy_next_run_at } : item)
  })
}
async function archive(mode: 'archive' | 'purge') {
  if (!live() || !archiving.value || deleting.value) return
  const plan = archiving.value
  deleting.value = true
  deleteError.value = ''
  try {
    if (mode === 'purge') await upstreamCenterAPI.purge({ kind: 'intelligence', id: plan.id, confirm_name: plan.name })
    else await intelligenceMonitorAPI.archive(plan.id)
    if (!live()) return
    plans.value = plans.value.filter(item => item.id !== plan.id)
    archiving.value = null
    app.showSuccess(t(mode === 'purge' ? 'upstreamCenter.storage.purged' : 'intelligenceMonitor.archived'))
    emit('changed')
    await refresh()
  } catch (cause) {
    if (live()) deleteError.value = extractApiErrorMessage(cause, t('upstreamCenter.storage.actionFailed'))
  } finally { if (live()) deleting.value = false }
}
onBeforeUnmount(() => { disposed = true })
</script>

<style scoped>
/* Reserve the same viewport before the first response, on refresh and after iframe loads. */
.group-intelligence-content {
  height: min(496px, calc(90vh - 112px));
  height: min(496px, calc(90dvh - 112px));
}
</style>
