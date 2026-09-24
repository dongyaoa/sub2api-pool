<template>
  <div ref="badgeRef" class="relative" @keydown.esc="dropdownOpen = false">
    <template v-if="isAdmin">
      <button
        type="button"
        data-testid="version-badge"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
        :class="hasUpdate ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400' : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400'"
        :title="versionSummary"
        :aria-expanded="dropdownOpen"
        @click="toggleDropdown"
      >
        <span class="font-medium">{{ currentVersion ? 'v' + currentVersion : '—' }}</span>
        <span v-if="hasUpdate" class="h-2 w-2 rounded-full bg-amber-500"></span>
        <Icon v-if="monitoring || submitting" name="refresh" size="xs" class="animate-spin" />
      </button>
      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          data-testid="version-dropdown"
          class="absolute left-0 z-50 mt-2 w-80 max-w-[calc(100vw-2rem)] overflow-hidden whitespace-normal rounded-xl border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <span class="text-sm font-medium text-gray-700 dark:text-dark-300">{{ t('version.currentVersion') }}</span>
            <button
              type="button"
              data-testid="refresh-version"
              class="rounded-lg p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700"
              :disabled="appStore.versionLoading || authStopped"
              :title="t('version.refresh')"
              :aria-label="t('version.refresh')"
              @click="refreshAll(true)"
            ><Icon name="refresh" size="sm" :class="{ 'animate-spin': appStore.versionLoading }" /></button>
          </div>
          <div class="space-y-3 p-4">
            <div class="text-center">
              <p class="text-2xl font-bold text-gray-900 dark:text-white">{{ currentVersion ? 'v' + currentVersion : '—' }}</p>
              <p data-testid="version-summary" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ versionSummary }}</p>
            </div>
            <p v-if="appStore.versionWarning" class="break-words text-xs text-amber-700 dark:text-amber-400">{{ appStore.versionWarning }}</p>
            <div v-if="job" data-testid="update-job" class="space-y-1 rounded-lg bg-gray-50 p-3 dark:bg-dark-700/50" role="status" aria-live="polite">
              <p class="text-sm font-medium" :class="jobFailed ? 'text-red-600 dark:text-red-400' : completed ? 'text-green-600 dark:text-green-400' : 'text-gray-700 dark:text-dark-200'">{{ jobLabel }}</p>
              <p class="text-xs text-gray-500 dark:text-dark-400">v{{ job.version }}</p>
              <p v-if="job.message" class="break-words text-xs text-gray-500 dark:text-dark-400">{{ job.message }}</p>
            </div>
            <p v-if="connectionLost" data-testid="update-reconnecting" class="text-xs text-amber-700 dark:text-amber-400">{{ t('version.reconnecting') }}</p>
            <p v-if="updateError" data-testid="update-error" class="break-words text-xs text-red-600 dark:text-red-400" role="alert">{{ updateError }}</p>
            <div v-if="unavailableReason && !monitoring" data-testid="update-unavailable" class="space-y-1 rounded-lg bg-blue-50 p-3 text-xs text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">
              <p>{{ unavailableReason }}</p>
              <a v-if="needsHelperSetup" :href="POOL_SETUP_URL" target="_blank" rel="noopener noreferrer" class="underline">{{ t('version.setupInstructions') }}</a>
            </div>
            <button
              v-if="canUpdate"
              type="button"
              data-testid="update-now"
              class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white hover:bg-primary-600"
              @click="requestUpdate"
            ><Icon name="download" size="sm" />{{ t('version.updateNow') }}</button>
            <p v-if="submitting" class="text-xs text-gray-500">{{ t('version.submitting') }}</p>
            <a :href="POOL_RELEASES_URL" target="_blank" rel="noopener noreferrer" class="flex items-center justify-center gap-1 text-xs text-gray-500 hover:underline dark:text-dark-400">
              {{ t('version.viewRelease') }}<Icon name="externalLink" size="xs" />
            </a>
          </div>
        </div>
      </transition>
      <ConfirmDialog
        :show="confirmation !== null"
        :title="t('version.confirmTitle')"
        :message="t('version.confirmContainerUpdate', { version: confirmation?.version || '' })"
        :confirm-text="t('version.updateNow')"
        @cancel="confirmation = null"
        @confirm="submitUpdate"
      />
    </template>
    <span v-else-if="currentVersion" class="text-xs text-gray-500 dark:text-dark-400">v{{ currentVersion }}</span>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import { getUpdateStatus, getVersion, performUpdate, type UpdateJob, type UpdateTarget } from '@/api/admin/system'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { extractApiErrorCode } from '@/utils/apiError'

const POOL_RELEASES_URL = 'https://github.com/dongyaoa/sub2api-pool/releases'
const POOL_SETUP_URL = 'https://github.com/dongyaoa/sub2api-pool/blob/main/deploy/POOL_ONLINE_UPDATE.md'
const CHECK_INTERVAL = 5 * 60 * 1000
const POLL_INTERVAL = 5000
const UPDATE_TIMEOUT = 25 * 60 * 1000
const PENDING_KEY = 'pool-container-update-pending'
const RELOADED_KEY = 'pool-container-update-reloaded'
interface PendingUpdate extends UpdateTarget { startedAt: number; previousJobId?: string }

const props = defineProps<{ version?: string }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.isAdmin)
const badgeRef = ref<HTMLElement | null>(null)
const dropdownOpen = ref(false)
const confirmation = ref<UpdateTarget | null>(null)
const job = ref<UpdateJob | null>(null)
const helperAvailable = ref(false)
const recoveryRequired = ref(false)
const statusKnown = ref(false)
const submitting = ref(false)
const monitoring = ref(false)
const completed = ref(false)
const connectionLost = ref(false)
const updateError = ref('')
const authStopped = ref(false)
const timedOut = ref(false)
let pending: PendingUpdate | null = null
let deadline = 0
let reloadAfterCompletion = false
let pollTimer: ReturnType<typeof setTimeout> | null = null
let checkTimer: ReturnType<typeof setInterval> | null = null
let reloadTimer: ReturnType<typeof setTimeout> | null = null
let statusBusy = false
let disposed = false
let generation = 0

const currentVersion = computed(() => isAdmin.value
  ? appStore.currentVersion || props.version || appStore.siteVersion
  : props.version || appStore.siteVersion)
const versionKnown = computed(() => appStore.versionLoaded && !appStore.versionCheckFailed)
const hasUpdate = computed(() => versionKnown.value && appStore.hasUpdate)
const jobFailed = computed(() => job.value?.state === 'failed' || job.value?.state === 'rolled_back')
const jobLabel = computed(() => completed.value ? t('version.updateComplete')
  : job.value?.state === 'succeeded' ? t('version.verifying')
    : t('version.jobStates.' + (job.value?.state || 'queued')))
const versionSummary = computed(() => appStore.versionLoading && !versionKnown.value ? t('version.checking')
  : !versionKnown.value ? t('version.checkUnknown')
    : hasUpdate.value ? t('version.latestVersion') + ': v' + appStore.latestVersion : t('version.upToDate'))
const unavailableCode = computed(() => recoveryRequired.value ? 'recovery_required' : appStore.versionInfo?.update_unavailable_reason)
const needsHelperSetup = computed(() => unavailableCode.value === 'helper_not_configured')
const unavailableReason = computed(() => {
  if (authStopped.value) return t('version.authRequired')
  if (!statusKnown.value) return t('version.statusUnknown')
  const code = unavailableCode.value
  if (code === 'helper_not_configured') return t('version.helperNotConfigured')
  if (code === 'helper_unavailable') return t('version.helperUnavailable')
  if (code === 'source_build') return t('version.sourceModeHint')
  if (code === 'recovery_required') return t('version.recoveryRequired')
  if (code) return code
  if (!helperAvailable.value) return t('version.helperUnavailable')
  if (appStore.versionInfo && !appStore.versionInfo.update_available) return t('version.updateUnavailable')
  return ''
})
const canUpdate = computed(() => isAdmin.value && !authStopped.value && !monitoring.value && !submitting.value && !timedOut.value
  && !pending && hasUpdate.value && statusKnown.value && helperAvailable.value && !recoveryRequired.value
  && appStore.versionInfo?.update_available === true && appStore.versionInfo.update_method === 'container'
  && appStore.buildType === 'release' && !!appStore.versionInfo.image_digest && !!appStore.versionInfo.latest_revision)

function validSession(token: number) { return !disposed && isAdmin.value && generation === token && !authStopped.value }
function errorStatus(error: unknown) {
  const value = error as { status?: number; response?: { status?: number } }
  return value?.status || value?.response?.status || 0
}
function errorMessage(error: unknown) {
  const value = error as { message?: string; response?: { data?: { message?: string } } }
  return value?.response?.data?.message || value?.message || t('version.updateFailed')
}
function persistPending(value: PendingUpdate | null) {
  pending = value
  try {
    if (value) sessionStorage.setItem(PENDING_KEY, JSON.stringify(value))
    else sessionStorage.removeItem(PENDING_KEY)
  } catch { /* Monitoring still works when browser storage is unavailable. */ }
}
function readPending() {
  try {
    const saved = JSON.parse(sessionStorage.getItem(PENDING_KEY) || 'null') as PendingUpdate | null
    if (saved && typeof saved.version === 'string' && typeof saved.digest === 'string' && Number.isFinite(saved.startedAt)) pending = saved
  } catch { /* Ignore malformed browser state; the helper remains authoritative. */ }
}
function clearPoll() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
}
function stopForAuth() {
  authStopped.value = true
  monitoring.value = false
  confirmation.value = null
  updateError.value = t('version.authRequired')
  clearPoll()
  if (checkTimer) clearInterval(checkTimer)
  checkTimer = null
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = null
}
function expireMonitoring() {
  monitoring.value = false
  timedOut.value = true
  connectionLost.value = false
  updateError.value = t('version.monitorTimeout')
  clearPoll()
}
function beginMonitoring(activeJob?: UpdateJob) {
  const parsed = activeJob ? Date.parse(activeJob.started_at) : NaN
  const started = pending?.startedAt ?? (Number.isFinite(parsed) ? Math.min(parsed, Date.now()) : Date.now())
  deadline = deadline || started + UPDATE_TIMEOUT
  if (Date.now() >= deadline) { expireMonitoring(); return }
  monitoring.value = true
  reloadAfterCompletion = true
  if (activeJob && !pending) persistPending({ version: activeJob.version, digest: activeJob.digest, startedAt: started })
}
function schedulePoll() {
  clearPoll()
  if (!monitoring.value || disposed || authStopped.value) return
  if (Date.now() >= deadline) { expireMonitoring(); return }
  pollTimer = setTimeout(() => { void refreshStatus() }, Math.min(POLL_INTERVAL, deadline - Date.now()))
}
function finishMonitoring() {
  monitoring.value = false
  connectionLost.value = false
  clearPoll()
  persistPending(null)
  deadline = 0
}
async function verifyCompleted(activeJob: UpdateJob, token: number) {
  const running = await getVersion()
  if (!validSession(token)) return
  if (activeJob.revision && running.version === activeJob.version && running.revision === activeJob.revision) {
    completed.value = true
    appStore.currentVersion = running.version
    finishMonitoring()
    updateError.value = ''
    appStore.clearVersionCache()
    void refreshVersion(true)
    if (reloadAfterCompletion) {
      let alreadyReloaded = false
      try { alreadyReloaded = sessionStorage.getItem(RELOADED_KEY) === activeJob.id } catch { /* optional guard */ }
      if (!alreadyReloaded) {
        try { sessionStorage.setItem(RELOADED_KEY, activeJob.id) } catch { /* optional guard */ }
        appStore.showSuccess(t('version.updateComplete'))
        reloadTimer = setTimeout(() => { if (validSession(token)) window.location.reload() }, 1500)
      }
    }
  } else {
    beginMonitoring(activeJob)
  }
}
async function refreshStatus() {
  if (statusBusy || disposed || !isAdmin.value || authStopped.value) return
  if (monitoring.value && Date.now() >= deadline) { expireMonitoring(); return }
  const token = generation
  statusBusy = true
  try {
    const status = await getUpdateStatus()
    if (!validSession(token)) return
    statusKnown.value = true
    helperAvailable.value = status.available
    recoveryRequired.value = status.recovery_required === true
    connectionLost.value = false
    if (status.recovery_required) {
      job.value = status.job || null
      finishMonitoring()
      updateError.value = t('version.recoveryRequired')
      return
    }
    const next = status.job
    // After an ambiguous POST, an old terminal job is not acknowledgement of this request.
    const matchesPending = !pending || (next && next.version === pending.version && next.digest === pending.digest && next.id !== pending.previousJobId)
    if (!next || !matchesPending) {
      if (pending) beginMonitoring()
      return
    }
    if (job.value?.id !== next.id) completed.value = false
    job.value = next
    if (next.state === 'failed' || next.state === 'rolled_back') {
      finishMonitoring()
      updateError.value = next.state === 'rolled_back' ? t('version.rolledBackFailure') : next.message || t('version.updateFailed')
    } else if (next.state === 'succeeded') {
      await verifyCompleted(next, token)
    } else {
      beginMonitoring(next)
    }
  } catch (error) {
    if (!validSession(token)) return
    if ([401, 403].includes(errorStatus(error))) { stopForAuth(); return }
    if (monitoring.value || pending) { beginMonitoring(); connectionLost.value = true }
    else { statusKnown.value = false }
  } finally {
    statusBusy = false
    if (validSession(token)) schedulePoll()
  }
}
async function refreshVersion(force = true) {
  if (!isAdmin.value || disposed || authStopped.value) return
  await appStore.fetchVersion(force)
  if ([401, 403].includes(appStore.versionErrorStatus)) stopForAuth()
}
async function refreshAll(force = true) {
  await Promise.all([refreshVersion(force), refreshStatus()])
}
function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
  if (dropdownOpen.value) void refreshAll(true)
}
function requestUpdate() {
  if (!canUpdate.value) return
  confirmation.value = { version: appStore.latestVersion, digest: appStore.versionInfo!.image_digest! }
}
async function submitUpdate() {
  const target = confirmation.value
  confirmation.value = null
  if (!target || !canUpdate.value) return
  const token = generation
  submitting.value = true
  completed.value = false
  updateError.value = ''
  persistPending({ ...target, startedAt: Date.now(), previousJobId: job.value?.id })
  deadline = Date.now() + UPDATE_TIMEOUT
  beginMonitoring()
  try {
    const result = await performUpdate(target)
    if (!validSession(token)) return
    if (result.job) job.value = result.job
    await refreshStatus()
  } catch (error) {
    if (!validSession(token)) return
    const status = errorStatus(error)
    if ([401, 403].includes(status)) { stopForAuth(); return }
    // These reasons are emitted before the host helper can accept a task.
    // Other 409 responses can follow an ambiguous helper submission.
    const rejectedBeforeSubmission = ['POOL_UPDATE_CHANGED', 'POOL_UPDATE_UNAVAILABLE', 'ALREADY_UP_TO_DATE']
      .includes(extractApiErrorCode(error) || '')
    if (rejectedBeforeSubmission || (status >= 400 && status < 500 && status !== 408 && status !== 409 && status !== 429)) {
      finishMonitoring()
      updateError.value = errorMessage(error)
      if (rejectedBeforeSubmission) await refreshAll(true)
    } else {
      // A dropped response may already have created the job. Only GET is retried.
      connectionLost.value = true
      await refreshStatus()
    }
  } finally {
    submitting.value = false
    if (validSession(token)) schedulePoll()
  }
}
function handleClickOutside(event: MouseEvent) {
  if (badgeRef.value && !badgeRef.value.contains(event.target as Node)) dropdownOpen.value = false
}
function startAdminChecks() {
  if (!isAdmin.value || disposed) return
  readPending()
  if (pending) beginMonitoring()
  void refreshAll(true)
  checkTimer = setInterval(() => {
    if (!monitoring.value && !submitting.value && !timedOut.value) void refreshAll(true)
  }, CHECK_INTERVAL)
}
watch(isAdmin, (admin) => {
  generation += 1
  clearPoll()
  if (checkTimer) clearInterval(checkTimer)
  if (reloadTimer) clearTimeout(reloadTimer)
  checkTimer = null
  reloadTimer = null
  monitoring.value = false
  dropdownOpen.value = false
  confirmation.value = null
  appStore.clearVersionCache()
  if (admin) { authStopped.value = false; startAdminChecks() }
})
onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  startAdminChecks()
})
onBeforeUnmount(() => {
  disposed = true
  generation += 1
  clearPoll()
  if (checkTimer) clearInterval(checkTimer)
  if (reloadTimer) clearTimeout(reloadTimer)
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active, .dropdown-leave-active { transition: all 0.2s ease; }
.dropdown-enter-from, .dropdown-leave-to { opacity: 0; transform: scale(0.95) translateY(-4px); }
</style>
