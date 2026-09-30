<template>
  <AppLayout>
    <MonitorHero
      :overall-status="overallStatus"
      :interval-seconds="DEFAULT_INTERVAL_SECONDS"
      :window="currentWindow"
      :loading="loading"
      :auto-refresh="autoRefresh"
      :refreshing="refreshing"
      :last-updated="lastUpdated"
      :refresh-error="refreshError"
      @update:window="handleWindowChange"
      @refresh="manualReload"
    />

    <MonitorCardGrid
      :items="items"
      :window="currentWindow"
      :countdown-seconds="countdown"
      :loading="loading"
      :detail-cache="detailCache"
      :display-order="displayOrder"
      :now="now"
      @card-click="openDetail"
    />

    <MonitorDetailDialog
      :show="showDetail"
      :monitor-id="detailTarget?.id ?? null"
      :title="detailTitle"
      @close="closeDetail"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  status as fetchChannelMonitorDetail,
  type UserMonitorView,
  type UserMonitorDetail,
  type MonitorDisplayOrder,
} from '@/api/channelMonitor'
import AppLayout from '@/components/layout/AppLayout.vue'
import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/components/user/MonitorDetailDialog.vue'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { getMonitorFreshness } from '@/utils/channelMonitorDisplay'

const { t } = useI18n()
const appStore = useAppStore()

// ── State ──
const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const refreshing = ref(false)
const refreshError = ref(false)
const lastUpdated = ref<string | null>(null)
const now = ref(Date.now())
const displayOrder = ref<MonitorDisplayOrder>()
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const showDetail = ref(false)
const detailTarget = ref<UserMonitorView | null>(null)

let abortController: AbortController | null = null
let freshnessTimer: ReturnType<typeof setInterval> | undefined

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  defaultEnabled: true,
  onRefresh: () => reload(true),
  shouldPause: () => document.hidden || refreshing.value,
})
const countdown = autoRefresh.countdown

// ── Computed ──
const overallStatus = computed<OverallStatus>(() => {
  if (items.value.length === 0) return 'unknown'
  if (items.value.every(item => getMonitorFreshness(item, now.value) === 'unknown')) return 'unknown'
  for (const it of items.value) {
    if (it.primary_status !== STATUS_OPERATIONAL || getMonitorFreshness(it, now.value) !== 'fresh') return 'degraded'
  }
  return 'operational'
})

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

// ── Loaders ──
async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  const requestedWindow = currentWindow.value
  loading.value = !silent || items.value.length === 0
  refreshing.value = true
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    const nextItems = res.items || []
    const nextDetails: Record<number, UserMonitorDetail> = {}
    // Fetch only the selected historical window, with bounded concurrency. Commit
    // list + details together so the timestamp describes the entire visible view.
    if (requestedWindow !== '7d') {
      for (let offset = 0; offset < nextItems.length; offset += 6) {
        if (ctrl.signal.aborted) return
        const batch = await Promise.all(nextItems.slice(offset, offset + 6).map(async item => ({
          id: item.id,
          detail: await fetchChannelMonitorDetail(item.id, { signal: ctrl.signal }),
        })))
        for (const { id, detail } of batch) nextDetails[id] = detail
      }
    }
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = nextItems
    displayOrder.value = res.display_order
    for (const key of Object.keys(detailCache)) delete detailCache[Number(key)]
    Object.assign(detailCache, nextDetails)
    now.value = Date.now()
    lastUpdated.value = new Date(now.value).toISOString()
    refreshError.value = false
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (ctrl.signal.aborted || abortController !== ctrl || e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    refreshError.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      refreshing.value = false
      autoRefresh.resetCountdown()
      abortController = null
    }
  }
}

async function manualReload() {
  await reload(false)
}

// ── Handlers ──
async function handleWindowChange(value: MonitorWindow) {
  if (currentWindow.value === value) return
  currentWindow.value = value
  await reload(true)
}

function openDetail(row: UserMonitorView) {
  detailTarget.value = row
  showDetail.value = true
}

function closeDetail() {
  showDetail.value = false
  detailTarget.value = null
}

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reload(false)
  freshnessTimer = setInterval(() => {
    if (!document.hidden) now.value = Date.now()
  }, 15000)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(autoRefresh.enabled.value)
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
  if (freshnessTimer) clearInterval(freshnessTimer)
})
</script>
