import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import VersionBadge from '../VersionBadge.vue'
import { useAppStore } from '@/stores/app'
import { checkUpdates, getUpdateStatus, getVersion, performUpdate, type UpdateJob, type VersionInfo } from '@/api/admin/system'

const auth = reactive({ isAdmin: true })
vi.mock('@/stores', async () => {
  const { useAppStore } = await import('@/stores/app')
  return { useAppStore, useAuthStore: () => auth }
})
vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(), getUpdateStatus: vi.fn(), getVersion: vi.fn(), performUpdate: vi.fn()
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

const digest = 'sha256:' + 'a'.repeat(64)
const revision = 'b'.repeat(40)
const current = '0.2.7-pool.4'
const target = '0.2.7-pool.5'
function version(overrides: Partial<VersionInfo> = {}): VersionInfo {
  return { current_version: current, latest_version: target, has_update: true, build_type: 'release',
    update_method: 'container', update_available: true, current_revision: 'c'.repeat(40), latest_revision: revision,
    image_digest: digest, ...overrides }
}
function updateJob(state: UpdateJob['state'] = 'pulling', overrides: Partial<UpdateJob> = {}): UpdateJob {
  return { id: 'job-1', state, message: '', version: target, revision, digest,
    started_at: new Date().toISOString(), ...overrides }
}
let wrapper: VueWrapper | undefined
async function render() {
  wrapper = mount(VersionBadge, { props: { version: current }, global: { stubs: {
    Icon: true,
    ConfirmDialog: { props: ['show'], emits: ['confirm', 'cancel'],
      template: '<div v-if="show" data-testid="confirmation"><button data-testid="confirm-update" @click="$emit(\'confirm\')">confirm</button><button data-testid="cancel-update" @click="$emit(\'cancel\')">cancel</button></div>' }
  } } })
  await flushPromises()
  return wrapper
}
async function open() {
  await wrapper!.get('[data-testid="version-badge"]').trigger('click')
  await flushPromises()
}
async function confirmUpdate() {
  await wrapper!.get('[data-testid="update-now"]').trigger('click')
  await wrapper!.get('[data-testid="confirm-update"]').trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-24T10:00:00Z'))
  setActivePinia(createPinia())
  auth.isAdmin = true
  sessionStorage.clear()
  vi.mocked(checkUpdates).mockReset().mockResolvedValue(version())
  vi.mocked(getUpdateStatus).mockReset().mockResolvedValue({ available: true })
  vi.mocked(getVersion).mockReset().mockResolvedValue({ version: current, revision: 'c'.repeat(40) })
  vi.mocked(performUpdate).mockReset().mockResolvedValue({ message: 'queued', need_restart: false, job: updateJob('queued') })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.clearAllTimers()
  vi.useRealTimers()
  sessionStorage.clear()
})

describe('Pool version badge', () => {
  it('only shows the local version for non-admins and makes no admin request', async () => {
    auth.isAdmin = false
    useAppStore().currentVersion = target
    await render()
    expect(wrapper!.text()).toBe('v' + current)
    expect(wrapper!.find('button').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)
    expect(checkUpdates).not.toHaveBeenCalled()
    expect(getUpdateStatus).not.toHaveBeenCalled()
  })

  it('checks at startup, every five minutes and on reopen without installing', async () => {
    await render()
    expect(checkUpdates).toHaveBeenCalledWith(true)
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    expect(checkUpdates).toHaveBeenCalledTimes(2)
    await open()
    expect(checkUpdates).toHaveBeenCalledTimes(3)
    await wrapper!.get('[data-testid="refresh-version"]').trigger('click')
    await flushPromises()
    expect(checkUpdates).toHaveBeenCalledTimes(4)
    expect(performUpdate).not.toHaveBeenCalled()
    expect(wrapper!.find('a[href*="Wei-Shaw"]').exists()).toBe(false)
  })

  it('shows unknown instead of up to date when the registry check fails', async () => {
    vi.mocked(checkUpdates).mockRejectedValue({ status: 502 })
    await render()
    await open()
    expect(wrapper!.get('[data-testid="version-summary"]').text()).toBe('version.checkUnknown')
    expect(wrapper!.text()).not.toContain('version.upToDate')
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
  })

  it('shows helper setup instructions with no update button when unconfigured', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(version({ update_available: false, update_unavailable_reason: 'helper_not_configured' }))
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: false })
    await render()
    await open()
    expect(wrapper!.text()).toContain('version.helperNotConfigured')
    expect(wrapper!.find('a[href*="dongyaoa/sub2api-pool/blob/main/deploy/POOL_ONLINE_UPDATE.md"]').exists()).toBe(true)
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
  })

  it('requires confirmation and posts the pinned version and digest once', async () => {
    await render()
    await open()
    await wrapper!.get('[data-testid="update-now"]').trigger('click')
    expect(wrapper!.find('[data-testid="confirmation"]').exists()).toBe(true)
    expect(performUpdate).not.toHaveBeenCalled()
    await wrapper!.get('[data-testid="cancel-update"]').trigger('click')
    expect(performUpdate).not.toHaveBeenCalled()
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('pulling') })
    await confirmUpdate()
    await vi.advanceTimersByTimeAsync(15_000)
    expect(performUpdate).toHaveBeenCalledTimes(1)
    expect(performUpdate).toHaveBeenCalledWith({ version: target, digest })
    expect(wrapper!.text()).toContain('version.jobStates.pulling')
  })

  it('recovers an ambiguous submission through GET and does not mistake an old job for it', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('failed', { id: 'old-job' }) })
    await render()
    await open()
    vi.mocked(performUpdate).mockRejectedValue({ status: 0 })
    await confirmUpdate()
    await vi.advanceTimersByTimeAsync(5000)
    expect(performUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
    vi.mocked(getUpdateStatus).mockRejectedValueOnce({ status: 503 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper!.text()).toContain('version.reconnecting')
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('checking') })
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper!.text()).toContain('version.jobStates.checking')
    expect(performUpdate).toHaveBeenCalledTimes(1)
  })

  it.each(['POOL_UPDATE_CHANGED', 'POOL_UPDATE_UNAVAILABLE', 'ALREADY_UP_TO_DATE'])('clears pending state for a definite %s rejection and refreshes the release', async (reason) => {
    await render()
    await open()
    const checks = vi.mocked(checkUpdates).mock.calls.length
    vi.mocked(performUpdate).mockRejectedValue({ status: 409, code: 409, reason, message: 'Refresh before retrying' })
    await confirmUpdate()
    expect(sessionStorage.getItem('pool-container-update-pending')).toBeNull()
    expect(wrapper!.get('[data-testid="update-error"]').text()).toBe('Refresh before retrying')
    expect(checkUpdates).toHaveBeenCalledTimes(checks + 1)
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(true)
    const statusCalls = vi.mocked(getUpdateStatus).mock.calls.length
    await vi.advanceTimersByTimeAsync(15_000)
    expect(getUpdateStatus).toHaveBeenCalledTimes(statusCalls)
    expect(performUpdate).toHaveBeenCalledTimes(1)
  })

  it('keeps GET recovery for POOL_UPDATE_REJECTED because the host may already have accepted it', async () => {
    await render()
    await open()
    vi.mocked(performUpdate).mockRejectedValue({ status: 409, code: 409, reason: 'POOL_UPDATE_REJECTED' })
    await confirmUpdate()
    expect(sessionStorage.getItem('pool-container-update-pending')).not.toBeNull()
    const statusCalls = vi.mocked(getUpdateStatus).mock.calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect(getUpdateStatus).toHaveBeenCalledTimes(statusCalls + 1)
    expect(performUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
  })

  it('resumes an existing job on mount and waits for matching running version and revision', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('recreating') })
    await render()
    await open()
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('succeeded') })
    vi.mocked(getVersion).mockResolvedValue({ version: target, revision: 'wrong-revision' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper!.text()).toContain('version.verifying')
    expect(useAppStore().toasts).toHaveLength(0)
    vi.mocked(getVersion).mockResolvedValue({ version: current, revision })
    await vi.advanceTimersByTimeAsync(5000)
    expect(useAppStore().toasts).toHaveLength(0)
    vi.mocked(getVersion).mockResolvedValue({ version: target, revision })
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper!.text()).toContain('version.updateComplete')
    expect(useAppStore().toasts[0].message).toBe('version.updateComplete')
    expect(sessionStorage.getItem('pool-container-update-reloaded')).toBe('job-1')
    expect(performUpdate).not.toHaveBeenCalled()
  })

  it('treats rolled_back as failure even when the old service is healthy', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('rolled_back') })
    await render()
    await open()
    expect(wrapper!.text()).toContain('version.rolledBackFailure')
    expect(wrapper!.text()).not.toContain('version.updateComplete')
    expect(getVersion).not.toHaveBeenCalled()
    expect(useAppStore().toasts).toHaveLength(0)
  })

  it.each([401, 403])('stops polling and periodic checks after status %s', async (status) => {
    vi.mocked(getUpdateStatus).mockResolvedValueOnce({ available: true, job: updateJob() }).mockRejectedValue({ status })
    await render()
    await vi.advanceTimersByTimeAsync(5000)
    const calls = vi.mocked(getUpdateStatus).mock.calls.length
    const checks = vi.mocked(checkUpdates).mock.calls.length
    await vi.advanceTimersByTimeAsync(30 * 60 * 1000)
    expect(getUpdateStatus).toHaveBeenCalledTimes(calls)
    expect(checkUpdates).toHaveBeenCalledTimes(checks)
    expect(performUpdate).not.toHaveBeenCalled()
  })

  it('stops retrying after twenty-five minutes during a service outage', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValueOnce({ available: true, job: updateJob('recreating') }).mockRejectedValue({ status: 503 })
    await render()
    await vi.advanceTimersByTimeAsync(25 * 60 * 1000)
    const calls = vi.mocked(getUpdateStatus).mock.calls.length
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)
    expect(getUpdateStatus).toHaveBeenCalledTimes(calls)
    await open()
    expect(wrapper!.text()).toContain('version.monitorTimeout')
    expect(performUpdate).not.toHaveBeenCalled()
  })

  it('shows recovery required and prevents a new update', async () => {
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: false, recovery_required: true, job: updateJob('failed') })
    await render()
    await open()
    expect(wrapper!.text()).toContain('version.recoveryRequired')
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
  })

  it('restores an ambiguous in-flight submission after reopening the page without another POST', async () => {
    sessionStorage.setItem('pool-container-update-pending', JSON.stringify({
      version: target, digest, startedAt: Date.now() - 60_000, previousJobId: 'old-job'
    }))
    vi.mocked(getUpdateStatus).mockRejectedValueOnce({ status: 503 }).mockResolvedValue({ available: true, job: updateJob() })
    await render()
    await vi.advanceTimersByTimeAsync(5000)
    await open()
    expect(wrapper!.text()).toContain('version.jobStates.pulling')
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
    expect(performUpdate).not.toHaveBeenCalled()
  })

  it('does not automatically reload repeatedly for a completed job on page reopen', async () => {
    sessionStorage.setItem('pool-container-update-reloaded', 'job-1')
    vi.mocked(getUpdateStatus).mockResolvedValue({ available: true, job: updateJob('succeeded') })
    vi.mocked(getVersion).mockResolvedValue({ version: target, revision })
    await render()
    await open()
    expect(wrapper!.text()).toContain('version.updateComplete')
    expect(useAppStore().toasts).toHaveLength(0)
    expect(performUpdate).not.toHaveBeenCalled()
  })

  it('does not offer container installation for source builds even when a newer release exists', async () => {
    vi.mocked(checkUpdates).mockResolvedValue(version({ build_type: 'source', update_available: false, update_unavailable_reason: 'source_build' }))
    await render()
    await open()
    expect(wrapper!.text()).toContain('version.sourceModeHint')
    expect(wrapper!.find('[data-testid="update-now"]').exists()).toBe(false)
  })

  it('stops requests and ignores in-flight results when admin access is removed', async () => {
    let resolveStatus!: (value: { available: boolean; job: UpdateJob }) => void
    vi.mocked(getUpdateStatus).mockReturnValueOnce(new Promise(resolve => { resolveStatus = resolve }))
    await render()
    auth.isAdmin = false
    await flushPromises()
    resolveStatus({ available: true, job: updateJob('succeeded') })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000)
    expect(getVersion).not.toHaveBeenCalled()
    expect(getUpdateStatus).toHaveBeenCalledTimes(1)
    expect(wrapper!.text()).toBe('v' + current)
  })
})
