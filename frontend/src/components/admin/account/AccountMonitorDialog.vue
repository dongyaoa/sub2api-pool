<template>
  <UpstreamIntelligenceDialog v-if="isOAuth" :account="account" :overview="null" @close="emit('close')" />
  <template v-else>
    <BaseDialog :show="true" :title="t('intelligenceMonitor.apiKeyAccount.title', { name: snapshot?.account_name || account.name })" width="extra-wide" motion="fade" :close-on-escape="!childOpen && !statusEditor && !ensuring" @close="close">
      <div class="account-monitor-content flex min-h-0 flex-col gap-3" data-testid="account-monitor-content">
        <div class="flex shrink-0 items-center justify-between gap-4">
          <div class="flex min-w-0 items-center gap-2.5">
            <span class="shrink-0 rounded-md bg-sky-50 px-2 py-1 text-[10px] font-semibold tracking-wide text-sky-600 dark:bg-sky-500/10 dark:text-sky-400">API Key</span>
            <p class="text-xs leading-5 text-gray-400 dark:text-dark-400">{{ t('intelligenceMonitor.apiKeyAccount.sharedHint') }}</p>
          </div>
          <button type="button" class="inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-2 text-xs font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:opacity-40 dark:hover:bg-dark-700 dark:hover:text-primary-400" :disabled="ensuring || statusEditor || childOpen" :aria-busy="loading" data-testid="account-monitor-refresh" @click="manualRefresh">
            <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />{{ t('intelligenceMonitor.refresh') }}
          </button>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain [scrollbar-gutter:stable]" data-testid="account-monitor-viewport" :aria-busy="!loaded && loading">
          <p v-if="error" role="alert" class="mb-3 rounded-xl bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
          <div v-if="!loaded && !error" class="space-y-4 motion-safe:animate-pulse" data-testid="account-monitor-loading">
            <div class="h-48 rounded-xl bg-gray-100 dark:bg-dark-800" />
            <div class="h-80 rounded-xl bg-gray-50 dark:bg-dark-800/60" />
          </div>
          <div v-if="snapshot?.target" class="space-y-4">
            <AccountUpstreamStatus :target="snapshot.target" :supplier-name="snapshot.supplier?.name" :busy="!!error || ensuring || childOpen" @configure="configureStatus" />
            <div v-if="snapshot.pelican_supported" :inert="!!error || statusEditor">
              <UpstreamIntelligenceDialog :key="snapshot.target.id" ref="pelican" :target="snapshot.target" :overview="overview" embedded :active="!closed && !error && !statusEditor" @child-open="childOpen = $event" @changed="refreshSnapshot" @refresh-overview="refreshSnapshot" />
            </div>
            <p v-else class="rounded-xl border border-gray-100 bg-gray-50/70 px-4 py-4 text-xs leading-6 text-gray-500 dark:border-dark-700 dark:bg-dark-900/30 dark:text-dark-400" data-testid="account-monitor-unsupported">{{ t('intelligenceMonitor.apiKeyAccount.pelicanUnsupported') }}</p>
          </div>
          <div v-else-if="loaded && !error" class="flex min-h-[400px] flex-col items-center justify-center rounded-xl border border-dashed border-gray-200 px-6 py-8 text-center dark:border-dark-700" data-testid="account-monitor-empty">
            <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-50 text-primary-500 dark:bg-primary-500/10"><Icon name="chart" size="xl" /></div>
            <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('intelligenceMonitor.apiKeyAccount.emptyTitle') }}</h4>
            <p class="mt-2 max-w-md text-xs leading-6 text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.apiKeyAccount.emptyHint') }}</p>
            <button type="button" class="btn btn-primary btn-sm mt-5" :disabled="ensuring || loading" data-testid="account-monitor-ensure" @click="ensureMonitor">
              <Icon :name="ensuring ? 'refresh' : 'plus'" size="sm" class="mr-1.5" :class="ensuring && 'animate-spin'" />{{ t(ensuring ? 'intelligenceMonitor.form.saving' : 'intelligenceMonitor.apiKeyAccount.connect') }}
            </button>
          </div>
        </div>
      </div>
    </BaseDialog>
    <UpstreamTargetDialog v-if="statusEditor && snapshot?.target" :show="true" :target="snapshot.target" :supplier="snapshot.supplier" @close="closeStatusEditor" @saved="closeStatusEditor" />
  </template>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { upstreamCenterAPI, type AccountUpstreamMonitor, type UpstreamOverview } from '@/api/admin/upstreamCenter'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import AccountUpstreamStatus from '@/components/admin/upstream/AccountUpstreamStatus.vue'
import UpstreamIntelligenceDialog from '@/components/admin/upstream/UpstreamIntelligenceDialog.vue'
import UpstreamTargetDialog from '@/components/admin/upstream/UpstreamTargetDialog.vue'
import { reconcileMonitorData } from '@/components/admin/upstream/monitorReconcile'
import { useMonitorRefresh } from '@/composables/useMonitorRefresh'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ account: Pick<Account, 'id' | 'name' | 'platform' | 'type' | 'parent_account_id' | 'extra'> }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const isOAuth = computed(() => props.account.type === 'oauth' && props.account.platform === 'openai')
const snapshot = ref<AccountUpstreamMonitor | null>(null)
const loaded = ref(false), error = ref(''), closed = ref(false), ensuring = ref(false)
const statusEditor = ref(false), childOpen = ref(false)
const pelican = ref<InstanceType<typeof UpstreamIntelligenceDialog> | null>(null)
let ensureController: AbortController | undefined
let disposed = false
const overview = computed<UpstreamOverview | null>(() => {
  const target = snapshot.value?.target
  if (!target) return null
  const supplier = snapshot.value?.supplier
  return { suppliers: supplier ? [{ ...supplier, targets: [target] }] : [], monitors: supplier ? [] : [target], summary: target.finance }
})
function applySnapshot(result: AccountUpstreamMonitor) {
  if (disposed || closed.value || result.account_id !== props.account.id) return
  if (snapshot.value?.target?.id !== result.target?.id) childOpen.value = false
  snapshot.value = reconcileMonitorData(snapshot.value, result)
  loaded.value = true
  error.value = ''
}
function showError(cause: unknown) {
  const code = extractApiErrorCode(cause)
  error.value = code === 'UPSTREAM_ACCOUNT_MONITOR_AMBIGUOUS'
    ? t('intelligenceMonitor.apiKeyAccount.ambiguous')
    : extractApiErrorMessage(cause, t('intelligenceMonitor.loadFailed'))
  const status = (cause as { status?: number; response?: { status?: number } })?.status ?? (cause as { response?: { status?: number } })?.response?.status
  if ([400, 403, 404, 409].includes(status || 0) || code === 'UPSTREAM_ACCOUNT_MONITOR_AMBIGUOUS') {
    snapshot.value = null
    statusEditor.value = false
    childOpen.value = false
  }
}
const { loading, refresh: refreshSnapshot } = useMonitorRefresh({
  active: () => !isOAuth.value && !closed.value && !ensuring.value,
  paused: () => statusEditor.value || childOpen.value,
  intervalMs: () => snapshot.value?.target?.enabled ? 3000 : 5000,
  request: signal => upstreamCenterAPI.accountMonitor(props.account.id, signal),
  apply: applySnapshot,
  onError: showError,
})
function manualRefresh() {
  if (ensuring.value || closed.value || childOpen.value || statusEditor.value) return
  void refreshSnapshot()
  void pelican.value?.refresh()
}
async function ensureMonitor() {
  if (ensuring.value || loading.value || error.value || snapshot.value?.target || closed.value) return
  const accountID = props.account.id
  const controller = new AbortController()
  ensureController = controller
  ensuring.value = true
  try {
    const result = await upstreamCenterAPI.ensureAccountMonitor(accountID, controller.signal)
    if (!disposed && !closed.value && !controller.signal.aborted && accountID === props.account.id) applySnapshot(result)
  } catch (cause) {
    if (!disposed && !closed.value && !controller.signal.aborted && accountID === props.account.id) showError(cause)
  } finally {
    if (ensureController === controller) {
      ensureController = undefined
      ensuring.value = false
    }
  }
}
function configureStatus() {
  if (error.value || childOpen.value || !snapshot.value?.target) return
  statusEditor.value = true
}
function closeStatusEditor() {
  statusEditor.value = false
  void refreshSnapshot()
}
function close() {
  if (closed.value || ensuring.value || statusEditor.value || childOpen.value) return
  closed.value = true
  emit('close')
}
watch(() => props.account.id, () => {
  ensureController?.abort()
  ensureController = undefined
  ensuring.value = false
  snapshot.value = null
  loaded.value = false
  error.value = ''
  statusEditor.value = false
  childOpen.value = false
  void refreshSnapshot()
})
onBeforeUnmount(() => { disposed = true; ensureController?.abort() })
</script>

<style scoped>
/* Keep the frame stable across initial reads, live updates and artwork loading. */
.account-monitor-content { height: min(660px, calc(90vh - 112px)); height: min(660px, calc(90dvh - 112px)); }
</style>
