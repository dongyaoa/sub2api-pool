<template>
  <AppLayout>
    <div class="space-y-3 pb-5" data-testid="pelican-monitor-page">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div class="min-w-0"><div class="flex items-center gap-2.5"><span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-300"><Icon name="lightbulb" size="md" /></span><h1 class="text-xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ settings.config.title || t('pelicanMonitor.title') }}</h1></div><p class="mt-2 max-w-3xl whitespace-pre-wrap text-xs leading-5 text-gray-500 dark:text-gray-400">{{ settings.config.description || t('pelicanMonitor.description') }}</p></div>
        <button type="button" class="btn btn-secondary btn-sm mt-1" :aria-busy="loading" data-testid="pelican-refresh" @click="manualRefresh"><Icon name="refresh" size="sm" class="mr-1.5" :class="loading && 'animate-spin'" />{{ t('pelicanMonitor.refresh') }}</button>
      </header>
      <p v-if="settings.config.notice && settings.enabled" class="whitespace-pre-wrap rounded-xl border border-primary-100 bg-primary-50/70 px-3.5 py-2.5 text-xs leading-5 text-primary-800 dark:border-primary-500/20 dark:bg-primary-500/5 dark:text-primary-200">{{ settings.config.notice }}</p>
      <p v-if="error" role="alert" class="rounded-xl bg-rose-50 px-4 py-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-300">{{ error }}</p>
      <div v-if="!loaded && loading" class="space-y-3" role="status"><div v-for="n in 2" :key="n" class="h-[272px] animate-pulse rounded-2xl border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800" /><span class="sr-only">{{ t('common.loading') }}</span></div>
      <div v-else-if="loaded && !settings.enabled" class="flex min-h-[360px] flex-col items-center justify-center rounded-2xl border border-dashed border-gray-200 bg-white p-8 text-center dark:border-dark-700 dark:bg-dark-800" data-testid="pelican-disabled"><Icon name="lightbulb" size="xl" class="mb-4 text-gray-300 dark:text-gray-500" /><h2 class="text-lg font-semibold text-gray-700 dark:text-gray-200">{{ t('pelicanMonitor.disabled') }}</h2><p class="mt-2 text-sm text-gray-400">{{ t('pelicanMonitor.disabledHint') }}</p></div>
      <template v-else-if="loaded && settings.enabled">
        <div class="flex flex-wrap items-center justify-between gap-3"><div class="relative w-full sm:max-w-xs"><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" /><input v-model="search" class="input !py-2 !pl-9 text-xs" :placeholder="t('pelicanMonitor.search')" :aria-label="t('pelicanMonitor.search')" /></div><div class="flex items-center gap-2 text-[11px] text-gray-400"><span class="h-1.5 w-1.5 rounded-full bg-emerald-400" /><span>{{ t('pelicanMonitor.groupCount', { count: groups.length }) }}</span><span class="mx-1 text-gray-200 dark:text-gray-600">/</span><span>{{ t('pelicanMonitor.updated', { seconds: updatedSeconds }) }}</span></div></div>
        <div v-if="visibleGroups.length" class="space-y-3"><PelicanGroupCard v-for="group in visibleGroups" :key="group.id" :group="group" :now="now + clockOffset" @open="openArtwork(group, $event)" /></div>
        <div v-else class="flex min-h-[300px] flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-gray-200 bg-white text-sm text-gray-400 dark:border-dark-700 dark:bg-dark-800"><Icon name="lightbulb" size="lg" />{{ t(search ? 'pelicanMonitor.noMatches' : 'pelicanMonitor.empty') }}</div>
        <p class="text-[11px] text-gray-400">{{ t('pelicanMonitor.retention') }}</p>
      </template>
      <PelicanArtworkDialog v-if="selectedRun && selectedGroup" :key="selectedRun.id" :run="selectedRun" :group-name="selectedGroup.group_name" @close="selected = null" />
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { pelicanMonitorAPI, type PelicanMonitorGroup, type PelicanRun } from '@/api/pelicanMonitor'
import { usePelicanMonitorStore } from '@/stores/pelicanMonitor'
import { useAuthStore } from '@/stores/auth'
import { useMonitorRefresh } from '@/composables/useMonitorRefresh'
import PelicanGroupCard from '@/components/pelican/PelicanGroupCard.vue'
import PelicanArtworkDialog from '@/components/pelican/PelicanArtworkDialog.vue'
import { pelicanPageActiveKey, pelicanPreviewRefreshKey } from '@/components/pelican/pelicanContext'
import { pelicanWorks } from '@/components/pelican/pelicanFormat'
import { resetPelicanArtworkAccess, retainPelicanArtworks } from '@/components/pelican/pelicanArtworkLoader'
import { reconcileMonitorData } from '@/components/admin/upstream/monitorReconcile'

const { t } = useI18n(), settings = usePelicanMonitorStore(), auth = useAuthStore()
const groups = ref<PelicanMonitorGroup[]>([]), loaded = ref(false), error = ref(''), search = ref('')
const selected = ref<{ groupID: number; runID: number } | null>(null)
const now = ref(Date.now()), clockOffset = ref(0), updatedAt = ref(Date.now()), polling = ref(true), previewRevision = ref(0)
const pageVisible = ref(!document.hidden)
const selectedGroup = computed(() => groups.value.find(group => group.id === selected.value?.groupID))
const selectedRun = computed(() => selectedGroup.value ? pelicanWorks(selectedGroup.value).find(run => run.id === selected.value?.runID) : undefined)
const visibleGroups = computed(() => groups.value.filter(group => group.group_name.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
const updatedSeconds = computed(() => Math.max(0, Math.floor((now.value - updatedAt.value) / 1000)))
provide(pelicanPageActiveKey, computed(() => auth.isAuthenticated && settings.enabled && pageVisible.value))
provide(pelicanPreviewRefreshKey, previewRevision)
function clearVisibleData() { groups.value = []; selected.value = null; resetPelicanArtworkAccess() }
const { loading, refresh } = useMonitorRefresh({
  active: () => auth.isAuthenticated && polling.value,
  // One lightweight metadata poll for the entire page, including manual runs
  // started by an administrator while this user is already watching.
  intervalMs: () => 1500,
  request: async signal => {
    const token = auth.token, userID = auth.user?.id
    const result = await pelicanMonitorAPI.list(signal)
    if (signal.aborted || !auth.isAuthenticated || auth.token !== token || auth.user?.id !== userID) throw new DOMException('Session changed', 'AbortError')
    return result
  },
  apply: result => {
    settings.apply(result.config)
    loaded.value = true; error.value = ''; updatedAt.value = Date.now()
    const serverTime = Date.parse(result.server_time)
    if (Number.isFinite(serverTime)) clockOffset.value = serverTime - Date.now()
    if (!result.config.enabled) { clearVisibleData(); polling.value = false; return }
    groups.value = reconcileMonitorData(groups.value, result.items || [])
    retainPelicanArtworks(new Set(groups.value.flatMap(group => pelicanWorks(group).map(run => run.id))))
    if (selected.value && !selectedRun.value) selected.value = null
  },
  onError: cause => {
    if ((cause as Error)?.name === 'AbortError') return
    const status = (cause as { response?: { status?: number }; status?: number })?.response?.status ?? (cause as { status?: number })?.status
    if ([401, 403, 404].includes(status || 0)) { settings.disable(); clearVisibleData(); polling.value = false; loaded.value = true }
    else error.value = t('pelicanMonitor.loadFailed')
  },
})
function manualRefresh() { previewRevision.value++; if (!polling.value) polling.value = true; else void refresh() }
function openArtwork(group: PelicanMonitorGroup, run: PelicanRun) { if (run.status === 'succeeded') selected.value = { groupID: group.id, runID: run.id } }
let clock: ReturnType<typeof setInterval> | undefined
function syncClock() { clearInterval(clock); clock = undefined; pageVisible.value = !document.hidden; now.value = Date.now(); if (pageVisible.value) clock = setInterval(() => { now.value = Date.now() }, 1000) }
watch(() => settings.enabled, enabled => { if (!enabled) clearVisibleData() })
watch([() => auth.token, () => auth.user?.id], () => {
  const wasPolling = polling.value
  clearVisibleData(); loaded.value = false; polling.value = auth.isAuthenticated
  // Abort the old session immediately even when both accounts are logged in.
  if (auth.isAuthenticated && wasPolling) void refresh()
})
onMounted(() => { syncClock(); document.addEventListener('visibilitychange', syncClock) })
onBeforeUnmount(() => { clearInterval(clock); document.removeEventListener('visibilitychange', syncClock); resetPelicanArtworkAccess() })
</script>
