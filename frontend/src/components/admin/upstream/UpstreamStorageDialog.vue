<template>
  <BaseDialog :show="show" :title="t('upstreamCenter.storage.title')" width="wide" :show-close-button="!busy && !purging" :close-on-escape="!busy && !purging" @close="close">
    <p class="text-xs leading-6 text-gray-500 dark:text-dark-300">{{ t('upstreamCenter.storage.description') }}</p>
    <div v-if="loading" class="flex items-center justify-center gap-2 py-10 text-sm text-gray-400" role="status"><Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}</div>
    <div v-else-if="loadError" class="mt-4 rounded-xl bg-rose-50 p-4 dark:bg-rose-500/10" role="alert"><p class="text-sm text-rose-600 dark:text-rose-400">{{ loadError }}</p><button type="button" class="btn btn-secondary btn-sm mt-3" @click="load">{{ t('upstreamCenter.retry') }}</button></div>
    <template v-else-if="policy">
      <section class="mt-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-center justify-between gap-3"><div><h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('upstreamCenter.storage.policy') }}</h3><p class="mt-1 text-xs text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.policyHint') }}</p></div><Toggle id="storage-enabled" :model-value="form.enabled" :aria-label="t('upstreamCenter.storage.enabled')" :disabled="busy" @update:model-value="form.enabled = $event" /></div>
        <div class="mt-4 grid gap-4 sm:grid-cols-2">
          <div><label for="storage-history-days" class="input-label">{{ t('upstreamCenter.storage.historyRetention') }}</label><Select id="storage-history-days" v-model="form.history_retention_days" :options="historyOptions" :searchable="false" :disabled="busy" /><p class="mt-1.5 text-[11px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.historyHint') }}</p></div>
          <div><label for="storage-snapshot-days" class="input-label">{{ t('upstreamCenter.storage.snapshotRetention') }}</label><Select id="storage-snapshot-days" v-model="form.snapshot_retention_days" :options="snapshotOptions" :searchable="false" :disabled="busy" /><p class="mt-1.5 text-[11px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.snapshotHint') }}</p></div>
        </div>
        <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-3 dark:border-dark-700"><span class="text-xs" :class="dirty ? 'text-amber-600 dark:text-amber-400' : 'text-gray-400 dark:text-dark-400'">{{ t(dirty ? 'upstreamCenter.storage.unsaved' : form.enabled ? 'upstreamCenter.storage.automaticOn' : 'upstreamCenter.storage.automaticOff') }}</span><button type="button" class="btn btn-primary btn-sm" :disabled="busy || !dirty" data-testid="save-storage" @click="save"><Icon v-if="saving" name="refresh" size="sm" class="mr-1.5 animate-spin" />{{ t('upstreamCenter.storage.save') }}</button></div>
      </section>
      <section class="mt-4 rounded-xl bg-gray-50 p-4 dark:bg-dark-900/60">
        <div class="flex flex-wrap items-start justify-between gap-3"><div><h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('upstreamCenter.storage.lastCleanup') }}</h3><p class="mt-1 text-xs text-gray-400 dark:text-dark-400">{{ policy.last_cleanup_at ? dateTime(policy.last_cleanup_at) : t('upstreamCenter.storage.neverCleaned') }}</p></div><button type="button" class="btn btn-secondary btn-sm" :disabled="busy || dirty" data-testid="cleanup-storage" @click="cleanup"><Icon name="refresh" size="sm" class="mr-1.5" :class="cleaning && 'animate-spin'" />{{ t(policy.last_result?.has_more ? 'upstreamCenter.storage.continueCleanup' : 'upstreamCenter.storage.cleanup') }}</button></div>
        <dl v-if="policy.last_result" class="mt-4 grid grid-cols-3 gap-3"><div v-for="metric in cleanupMetrics" :key="metric.key"><dt class="text-[11px] text-gray-500 dark:text-dark-400">{{ t(`upstreamCenter.storage.${metric.key}`) }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums text-gray-800 dark:text-gray-100">{{ metric.value.toLocaleString() }}</dd></div></dl>
        <p class="mt-3 text-[11px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.cleanupHint') }}</p>
        <p v-if="policy.last_result?.has_more" class="mt-2 text-xs leading-5 text-amber-600 dark:text-amber-400" data-testid="cleanup-has-more">{{ t('upstreamCenter.storage.hasMore') }}</p>
      </section>
      <section class="mt-5">
        <div class="flex items-center justify-between gap-3"><h3 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('upstreamCenter.storage.archives') }} <span class="ml-1 text-xs font-normal tabular-nums text-gray-400">{{ archiveTotal }}</span></h3><button type="button" class="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700" :disabled="busy || archivesLoading" :aria-label="t('upstreamCenter.refresh')" @click="loadArchives"><Icon name="refresh" size="sm" :class="archivesLoading && 'animate-spin'" /></button></div>
        <p class="mt-1 text-xs leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.archivesHint') }}</p>
        <p v-if="archivesError" role="alert" class="mt-3 text-xs text-rose-600 dark:text-rose-400">{{ archivesError }}</p>
        <div class="mt-3 max-h-[320px] divide-y divide-gray-100 overflow-y-auto rounded-xl border border-gray-200 dark:divide-dark-700 dark:border-dark-700" :aria-busy="archivesLoading">
          <article v-for="item in archives" :key="`${item.kind}-${item.id}`" class="flex items-center gap-3 px-3 py-3" :data-archive="`${item.kind}-${item.id}`">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300"><Icon :name="item.kind === 'supplier' ? 'server' : item.kind === 'target' ? 'chart' : item.source_type === 'openai_oauth' ? 'shield' : 'lightbulb'" size="sm" /></div>
            <div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-1.5"><strong class="max-w-full truncate text-xs font-medium text-gray-800 dark:text-gray-100" :title="item.name">{{ item.name }}</strong><span class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-dark-300">{{ t(item.kind === 'intelligence' && item.source_type === 'openai_oauth' ? 'upstreamCenter.storage.kinds.oauth' : `upstreamCenter.storage.kinds.${item.kind}`) }}</span><span v-if="item.kind === 'intelligence' && ['external', 'upstream', 'local_group'].includes(item.source_type)" class="text-[10px] text-gray-400 dark:text-dark-400">{{ t(`intelligenceMonitor.source.${item.source_type}`) }}</span></div><p class="mt-1 truncate text-[10px] text-gray-400 dark:text-dark-400">{{ [item.supplier_name, dateTime(item.deleted_at)].filter(Boolean).join(' · ') }}</p></div>
            <button type="button" class="shrink-0 rounded-lg px-2 py-1.5 text-xs text-rose-600 hover:bg-rose-50 disabled:opacity-50 dark:text-rose-400 dark:hover:bg-rose-500/10" :disabled="busy || archivesLoading" @click="purging = item; purgeError = ''">{{ t('upstreamCenter.storage.purge') }}</button>
          </article>
          <p v-if="!archives.length && archivesLoading" class="flex items-center justify-center gap-2 px-4 py-8 text-xs text-gray-400 dark:text-dark-400" role="status"><Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}</p>
          <p v-if="!archives.length && !archivesLoading && !archivesError" class="px-4 py-8 text-center text-xs text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.emptyArchives') }}</p>
        </div>
        <p v-if="archiveTotal > archives.length" class="mt-2 text-[11px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.storage.archiveLimit', { count: archives.length, total: archiveTotal }) }}</p>
      </section>
      <p v-if="actionError" role="alert" class="mt-4 rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ actionError }}</p>
      <p v-if="success" role="status" class="mt-4 text-xs text-emerald-600 dark:text-emerald-400">{{ success }}</p>
    </template>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="busy || !!purging" @click="close">{{ t('common.close') }}</button></template>
  </BaseDialog>
  <UpstreamDeleteDialog v-if="purging" :show="true" :item="purging" :busy="deleting" :error="purgeError" :archive-allowed="false" @close="purging = null" @confirm="purge" />
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { upstreamCenterAPI, type UpstreamArchiveItem, type UpstreamStoragePolicy } from '@/api/admin/upstreamCenter'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamDeleteDialog from './UpstreamDeleteDialog.vue'
import { dateTime } from './format'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const policy = ref<UpstreamStoragePolicy | null>(null)
const form = reactive({ enabled: true, history_retention_days: 30, snapshot_retention_days: 7 })
const loading = ref(false), saving = ref(false), cleaning = ref(false), deleting = ref(false)
const loadError = ref(''), actionError = ref(''), archivesError = ref(''), success = ref(''), purgeError = ref('')
const archives = ref<UpstreamArchiveItem[]>([]), archiveTotal = ref(0), archivesLoading = ref(false), purging = ref<UpstreamArchiveItem | null>(null)
const busy = computed(() => saving.value || cleaning.value || deleting.value)
const dirty = computed(() => !!policy.value && (form.enabled !== policy.value.enabled || Number(form.history_retention_days) !== policy.value.history_retention_days || Number(form.snapshot_retention_days) !== policy.value.snapshot_retention_days))
const historyOptions = computed(() => [30, 60, 90, 180, 365].map(value => ({ value, label: t('upstreamCenter.storage.days', { days: value }) })))
const snapshotOptions = computed(() => [1, 3, 7, 30, 90].map(value => ({ value, label: t('upstreamCenter.storage.days', { days: value }) })))
const cleanupMetrics = computed(() => [
  { key: 'historyDeleted', value: policy.value?.last_result?.history_deleted || 0 },
  { key: 'balanceDeleted', value: policy.value?.last_result?.balance_deleted || 0 },
  { key: 'billingDeleted', value: policy.value?.last_result?.billing_deleted || 0 },
])
let session = 0, archiveRequest = 0
let controller: AbortController | undefined
function close() { if (!busy.value && !purging.value) emit('close') }
function assignPolicy(value: UpstreamStoragePolicy) { policy.value = value; Object.assign(form, { enabled: value.enabled, history_retention_days: value.history_retention_days, snapshot_retention_days: value.snapshot_retention_days }) }
async function loadArchives() {
  const generation = session, request = ++archiveRequest
  archivesLoading.value = true
  try { const result = await upstreamCenterAPI.archives(controller?.signal); if (generation !== session || request !== archiveRequest) return; archives.value = result.items || []; archiveTotal.value = result.total || 0; archivesError.value = '' }
  catch (error) { if (generation === session && request === archiveRequest) archivesError.value = extractApiErrorMessage(error, t('upstreamCenter.storage.loadFailed')) }
  finally { if (generation === session && request === archiveRequest) archivesLoading.value = false }
}
async function load() {
  const generation = ++session
  controller?.abort(); controller = new AbortController(); loading.value = true; loadError.value = ''; actionError.value = ''; success.value = ''; archives.value = []; archiveTotal.value = 0; archivesError.value = ''
  void loadArchives()
  try { const result = await upstreamCenterAPI.storage(controller.signal); if (generation === session) assignPolicy(result) }
  catch (error) { if (generation === session) loadError.value = extractApiErrorMessage(error, t('upstreamCenter.storage.loadFailed')) }
  finally { if (generation === session) loading.value = false }
}
async function save() {
  if (!policy.value || busy.value || !dirty.value) return
  const generation = session; saving.value = true; actionError.value = ''; success.value = ''
  const input = { enabled: form.enabled, history_retention_days: Number(form.history_retention_days), snapshot_retention_days: Number(form.snapshot_retention_days) }
  try { const result = await upstreamCenterAPI.updateStorage(input); if (generation !== session) return; assignPolicy(result || { ...policy.value, ...input }); success.value = t('upstreamCenter.storage.saved') }
  catch (error) { if (generation === session) actionError.value = extractApiErrorMessage(error, t('upstreamCenter.storage.actionFailed')) }
  finally { if (generation === session) saving.value = false }
}
async function cleanup() {
  if (!policy.value || busy.value || dirty.value) return
  const generation = session; cleaning.value = true; actionError.value = ''; success.value = ''
  try {
    const result = await upstreamCenterAPI.cleanupStorage()
    if (generation !== session) return
    policy.value = { ...policy.value, last_result: result }
    success.value = t('upstreamCenter.storage.cleaned'); emit('changed')
    try {
      const refreshed = await upstreamCenterAPI.storage(controller?.signal)
      if (generation === session) assignPolicy(refreshed)
    } catch {
      if (generation === session) actionError.value = t('upstreamCenter.storage.refreshAfterCleanupFailed')
    }
  } catch (error) { if (generation === session) actionError.value = extractApiErrorMessage(error, t('upstreamCenter.storage.actionFailed')) }
  finally { if (generation === session) cleaning.value = false }
}
async function purge() {
  if (!purging.value || busy.value) return
  const generation = session, item = purging.value; deleting.value = true; purgeError.value = ''
  try { await upstreamCenterAPI.purge({ kind: item.kind, id: item.id, confirm_name: item.name }); if (generation !== session) return; archives.value = archives.value.filter(archived => archived.kind !== item.kind || archived.id !== item.id); archiveTotal.value = Math.max(0, archiveTotal.value - 1); purging.value = null; success.value = t('upstreamCenter.storage.purged'); emit('changed'); await loadArchives() }
  catch (error) { if (generation === session) purgeError.value = extractApiErrorMessage(error, t('upstreamCenter.storage.actionFailed')) }
  finally { if (generation === session) deleting.value = false }
}
watch(() => props.show, show => {
  if (show) { void load(); return }
  session++; archiveRequest++; controller?.abort(); loading.value = false; archivesLoading.value = false; saving.value = false; cleaning.value = false; deleting.value = false; purging.value = null
}, { immediate: true })
watch(purging, async (item, previous) => {
  if (!previous || item) return
  await nextTick()
  // BaseDialog releases its body lock on unmount; the parent remains open.
  if (props.show) document.body.classList.add('modal-open')
})
onBeforeUnmount(() => { session++; archiveRequest++; controller?.abort() })
</script>
