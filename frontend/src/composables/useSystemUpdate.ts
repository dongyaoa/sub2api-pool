import { computed, onBeforeUnmount, onMounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getUpdateStatus, getVersion, performUpdate, type UpdateJob, type UpdateTarget } from '@/api/admin/system'
import { extractApiErrorCode } from '@/utils/apiError'

const CHECK_INTERVAL = 5 * 60 * 1000
const POLL_INTERVAL = 5000
const UPDATE_TIMEOUT = 25 * 60 * 1000
const PENDING_KEY = 'pool-container-update-pending'
const RELOADED_KEY = 'pool-container-update-reloaded'
interface PendingUpdate extends UpdateTarget {
  revision?: string
  jobId?: string
  previousJobId?: string
  startedAt: number
}
const terminal = (job: UpdateJob) => ['succeeded', 'failed', 'rolled_back'].includes(job.state)

/** A submission is sent once; all recovery uses the persisted server job and GET requests. */
export function useSystemUpdate(isAdmin: ComputedRef<boolean>) {
  const { t } = useI18n()
  const appStore = useAppStore()
  const confirmation = ref<(UpdateTarget & { revision: string }) | null>(null)
  const job = ref<UpdateJob | null>(null)
  const statusKnown = ref(false)
  const available = ref(false)
  const recoveryRequired = ref(false)
  const submitting = ref(false)
  const monitoring = ref(false)
  const completed = ref(false)
  const connectionLost = ref(false)
  const updateError = ref('')
  const authStopped = ref(false)
  const timedOut = ref(false)
  const pending = ref<PendingUpdate | null>(null)
  let lastJobId: string | undefined
  let deadline = 0
  let pollTimer: ReturnType<typeof setTimeout> | null = null
  let checkTimer: ReturnType<typeof setInterval> | null = null
  let reloadTimer: ReturnType<typeof setTimeout> | null = null
  let statusRequest: object | null = null
  let disposed = false
  let generation = 0

  const versionKnown = computed(() => appStore.versionLoaded && !appStore.versionCheckFailed)
  const hasUpdate = computed(() => versionKnown.value && appStore.hasUpdate)
  const busy = computed(() => submitting.value || monitoring.value)
  const jobFailed = computed(() => job.value?.state === 'failed' || job.value?.state === 'rolled_back')
  const jobLabel = computed(() => completed.value ? t('version.updateComplete')
    : job.value?.state === 'succeeded' ? t('version.verifying')
      : t('version.jobStates.' + (job.value?.state || 'queued')))
  const unavailableCode = computed(() => recoveryRequired.value ? 'recovery_required' : appStore.versionInfo?.update_unavailable_reason)
  const needsSetup = computed(() => ['helper_not_configured', 'runtime_not_configured', 'runtime_unwritable'].includes(unavailableCode.value || ''))
  const unavailableReason = computed(() => {
    if (authStopped.value) return t('version.authRequired')
    if (!statusKnown.value) return t('version.statusUnknown')
    const reasons: Record<string, string> = {
      helper_not_configured: 'helperNotConfigured', helper_unavailable: 'helperUnavailable',
      source_build: 'sourceModeHint', recovery_required: 'recoveryRequired',
      runtime_not_configured: 'runtimeNotConfigured', runtime_unwritable: 'runtimeUnwritable',
      unsupported_platform: 'unsupportedPlatform'
    }
    const code = unavailableCode.value
    if (code) return t('version.' + (reasons[code] || 'updateUnavailable'))
    if (!available.value) return t('version.statusUnknown')
    if (appStore.versionInfo && !appStore.versionInfo.update_available) return t('version.updateUnavailable')
    return ''
  })
  const versionWarning = computed(() => {
    const warning = appStore.versionWarning
    if (!warning) return ''
    return t(warning === 'unrecognized_current_version' ? 'version.unrecognizedVersion' : 'version.releaseUnavailable')
  })
  const targetDigest = computed(() => appStore.versionInfo?.update_digest || appStore.versionInfo?.image_digest)
  const canUpdate = computed(() => isAdmin.value && !authStopped.value && !busy.value && !pending.value && !timedOut.value
    && hasUpdate.value && statusKnown.value && available.value && !recoveryRequired.value
    && appStore.versionInfo?.update_available === true
    && ['binary', 'container'].includes(appStore.versionInfo.update_method || '')
    && appStore.buildType === 'release' && !!targetDigest.value && !!appStore.versionInfo.latest_revision)

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
    pending.value = value
    try {
      if (value) sessionStorage.setItem(PENDING_KEY, JSON.stringify(value))
      else sessionStorage.removeItem(PENDING_KEY)
    } catch { /* The server job is still authoritative without browser storage. */ }
  }
  function readPending() {
    pending.value = null
    try {
      const saved = JSON.parse(sessionStorage.getItem(PENDING_KEY) || 'null') as PendingUpdate | null
      if (saved && typeof saved.version === 'string' && typeof saved.digest === 'string'
        && Number.isFinite(saved.startedAt) && saved.startedAt <= Date.now()
        && (!saved.revision || typeof saved.revision === 'string')
        && (!saved.jobId || typeof saved.jobId === 'string')) pending.value = saved
    } catch { /* Ignore malformed browser state. */ }
  }
  function clearPoll() {
    if (pollTimer) clearTimeout(pollTimer)
    pollTimer = null
  }
  function stopForAuth() {
    authStopped.value = true
    monitoring.value = false
    submitting.value = false
    confirmation.value = null
    updateError.value = t('version.authRequired')
    clearPoll()
    if (checkTimer) clearInterval(checkTimer)
    if (reloadTimer) clearTimeout(reloadTimer)
    checkTimer = null
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
    const started = pending.value?.startedAt ?? (Number.isFinite(parsed) ? Math.min(parsed, Date.now()) : Date.now())
    deadline = deadline || started + UPDATE_TIMEOUT
    if (activeJob && !pending.value) persistPending({
      version: activeJob.version, digest: activeJob.digest, revision: activeJob.revision,
      jobId: activeJob.id, startedAt: started
    })
    if (Date.now() >= deadline) { expireMonitoring(); return }
    monitoring.value = true
  }
  function schedulePoll() {
    clearPoll()
    if (!monitoring.value || disposed || authStopped.value) return
    if (Date.now() >= deadline) { expireMonitoring(); return }
    pollTimer = setTimeout(() => { void refreshStatus() }, Math.min(POLL_INTERVAL, deadline - Date.now()))
  }
  function finishMonitoring() {
    monitoring.value = false
    timedOut.value = false
    connectionLost.value = false
    clearPoll()
    persistPending(null)
    deadline = 0
  }
  function matchesPending(next: UpdateJob) {
    const target = pending.value
    return !target || (next.version === target.version && next.digest === target.digest
      && (!target.revision || next.revision === target.revision)
      && (!target.jobId || next.id === target.jobId) && next.id !== target.previousJobId)
  }
  async function verifyCompleted(activeJob: UpdateJob, token: number) {
    const running = await getVersion()
    if (!validSession(token)) return
    if (!activeJob.revision || running.version !== activeJob.version || running.revision !== activeJob.revision) {
      beginMonitoring(activeJob)
      return
    }
    completed.value = true
    appStore.currentVersion = running.version
    finishMonitoring()
    updateError.value = ''
    appStore.clearVersionCache()
    void refreshVersion(true)
    let alreadyReloaded = false
    try { alreadyReloaded = sessionStorage.getItem(RELOADED_KEY) === activeJob.id } catch { /* optional guard */ }
    if (!alreadyReloaded && !reloadTimer) {
      try { sessionStorage.setItem(RELOADED_KEY, activeJob.id) } catch { /* optional guard */ }
      appStore.showSuccess(t('version.updateComplete'))
      reloadTimer = setTimeout(() => { if (validSession(token)) window.location.reload() }, 1500)
    }
  }
  async function refreshStatus() {
    if (statusRequest || disposed || !isAdmin.value || authStopped.value) return
    if (monitoring.value && Date.now() >= deadline) { expireMonitoring(); return }
    const token = generation
    const request = {}
    statusRequest = request
    try {
      const status = await getUpdateStatus()
      if (!validSession(token)) return
      statusKnown.value = true
      available.value = status.available
      recoveryRequired.value = status.recovery_required === true
      connectionLost.value = false
      if (status.recovery_required) {
        job.value = status.job || null
        finishMonitoring()
        updateError.value = t('version.recoveryRequired')
        return
      }
      const next = status.job
      if (next) lastJobId = next.id
      if (!next || !matchesPending(next)) {
        if (pending.value) beginMonitoring()
        return
      }
      // Completed tasks are history. They must never restart monitoring or lock a newer installation.
      if (terminal(next) && !pending.value) return
      if (job.value?.id !== next.id) completed.value = false
      job.value = next
      if (pending.value && !pending.value.jobId) persistPending({ ...pending.value, jobId: next.id, revision: next.revision })
      if (next.state === 'failed' || next.state === 'rolled_back') {
        finishMonitoring()
        updateError.value = next.state === 'rolled_back' ? t('version.rolledBackFailure') : next.message || t('version.updateFailed')
      } else if (next.state === 'succeeded') {
        await verifyCompleted(next, token)
      } else {
        updateError.value = ''
        beginMonitoring(next)
      }
    } catch (error) {
      if (!validSession(token)) return
      if ([401, 403].includes(errorStatus(error))) { stopForAuth(); return }
      if (monitoring.value || pending.value) { beginMonitoring(); connectionLost.value = true }
      else statusKnown.value = false
    } finally {
      if (statusRequest === request) statusRequest = null
      if (validSession(token)) schedulePoll()
    }
  }
  async function refreshVersion(force = true) {
    if (!isAdmin.value || disposed || authStopped.value) return
    const token = generation
    await appStore.fetchVersion(force)
    if (validSession(token) && [401, 403].includes(appStore.versionErrorStatus)) stopForAuth()
  }
  async function refreshAll(force = true, retryMonitoring = false) {
    if (retryMonitoring && timedOut.value) {
      timedOut.value = false
      updateError.value = ''
      deadline = Date.now() + UPDATE_TIMEOUT
      if (pending.value) monitoring.value = true
    }
    await Promise.all([refreshVersion(force), refreshStatus()])
  }
  function requestUpdate() {
    if (!canUpdate.value) return
    confirmation.value = { version: appStore.latestVersion, digest: targetDigest.value!, revision: appStore.versionInfo!.latest_revision! }
  }
  async function submitUpdate() {
    const target = confirmation.value
    confirmation.value = null
    if (!target || !canUpdate.value) return
    const token = generation
    submitting.value = true
    completed.value = false
    job.value = null
    updateError.value = ''
    persistPending({ ...target, startedAt: Date.now(), previousJobId: lastJobId })
    deadline = Date.now() + UPDATE_TIMEOUT
    beginMonitoring()
    try {
      const result = await performUpdate({ version: target.version, digest: target.digest })
      if (!validSession(token)) return
      if (result.job && matchesPending(result.job)) {
        job.value = result.job
        persistPending({ ...pending.value!, jobId: result.job.id })
      }
      await refreshStatus()
    } catch (error) {
      if (!validSession(token)) return
      const status = errorStatus(error)
      if ([401, 403].includes(status)) { stopForAuth(); return }
      const rejectedBeforeSubmission = ['POOL_UPDATE_CHANGED', 'POOL_UPDATE_UNAVAILABLE', 'ALREADY_UP_TO_DATE', 'POOL_UPDATE_BUSY', 'SYSTEM_OPERATION_BUSY']
        .includes(extractApiErrorCode(error) || '')
      if (rejectedBeforeSubmission || (status >= 400 && status < 500 && status !== 408 && status !== 409 && status !== 429)) {
        finishMonitoring()
        updateError.value = errorMessage(error)
        if (rejectedBeforeSubmission) await refreshAll(true)
      } else {
        connectionLost.value = true
        await refreshStatus()
      }
    } finally {
      if (validSession(token)) { submitting.value = false; schedulePoll() }
    }
  }
  function startAdminChecks() {
    if (!isAdmin.value || disposed) return
    readPending()
    if (pending.value) beginMonitoring()
    void refreshAll(true)
    checkTimer = setInterval(() => {
      if (!busy.value && !timedOut.value) void refreshAll(true)
    }, CHECK_INTERVAL)
  }
  function clearTimers() {
    clearPoll()
    if (checkTimer) clearInterval(checkTimer)
    if (reloadTimer) clearTimeout(reloadTimer)
    checkTimer = null
    reloadTimer = null
  }
  watch(isAdmin, (admin) => {
    generation += 1
    clearTimers()
    statusRequest = null
    deadline = 0
    lastJobId = undefined
    monitoring.value = submitting.value = timedOut.value = completed.value = connectionLost.value = false
    statusKnown.value = available.value = recoveryRequired.value = false
    job.value = confirmation.value = null
    pending.value = null
    updateError.value = ''
    appStore.clearVersionCache()
    if (admin) { authStopped.value = false; startAdminChecks() }
  })
  onMounted(startAdminChecks)
  onBeforeUnmount(() => { disposed = true; generation += 1; clearTimers() })

  return {
    confirmation, job, busy, submitting, monitoring, completed, connectionLost, updateError, authStopped, timedOut,
    versionKnown, hasUpdate, jobFailed, jobLabel, unavailableReason, needsSetup, versionWarning, canUpdate,
    refreshAll, requestUpdate, submitUpdate
  }
}
