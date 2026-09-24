/**
 * System API endpoints for admin operations
 */

import { apiClient } from '../client'

export interface ReleaseInfo {
  name: string
  body: string
  published_at: string
  html_url: string
}

export interface VersionInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: ReleaseInfo
  cached?: boolean
  warning?: string
  build_type: string // "source" for manual builds, "release" for CI builds
  update_method?: 'container'
  update_available?: boolean
  update_unavailable_reason?: string
  current_revision?: string
  latest_revision?: string
  image_digest?: string
}

export type UpdateJobState = 'queued' | 'pulling' | 'recreating' | 'checking' | 'succeeded' | 'failed' | 'rolled_back'

export interface UpdateJob {
  id: string
  state: UpdateJobState
  message: string
  version: string
  revision: string
  digest: string
  started_at: string
  finished_at?: string
}

export interface UpdateStatus {
  available: boolean
  recovery_required?: boolean
  job?: UpdateJob
}

export interface UpdateTarget {
  version: string
  digest: string
}

/**
 * Get current version
 */
export async function getVersion(): Promise<{ version: string; revision?: string }> {
  const { data } = await apiClient.get<{ version: string; revision?: string }>('/admin/system/version', {
    timeout: 10000
  })
  return data
}

/**
 * Check for updates
 * @param force - Force refresh from GitHub API
 */
export async function checkUpdates(force = false): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/check-updates', {
    params: force ? { force: 'true' } : undefined,
    timeout: 30000
  })
  return data
}

export interface UpdateResult {
  message: string
  need_restart: boolean
  job?: UpdateJob
}

export async function getUpdateStatus(): Promise<UpdateStatus> {
  const { data } = await apiClient.get<UpdateStatus>('/admin/system/update-status', { timeout: 10000 })
  return data
}

export interface RollbackVersionInfo {
  version: string
  published_at: string
  html_url: string
}

/**
 * Get versions available for rollback (up to 3 versions older than current)
 */
export async function getRollbackVersions(): Promise<{ versions: RollbackVersionInfo[] }> {
  const { data } = await apiClient.get<{ versions: RollbackVersionInfo[] }>(
    '/admin/system/rollback-versions'
  )
  return data
}

/**
 * In-place update/rollback downloads a full release binary from GitHub, which
 * can take several minutes on slow links. The global 30s axios timeout would
 * abort the request mid-download (#4504), so these calls wait as long as the
 * backend allows (15 minutes server-side).
 */
const UPDATE_REQUEST_TIMEOUT_MS = 15 * 60 * 1000

/**
 * Submit a pinned Pool container update. The backend returns immediately;
 * monitor getUpdateStatus instead of resubmitting when the connection drops.
 * The optional parameter preserves the legacy export signature.
 */
export async function performUpdate(target?: UpdateTarget): Promise<UpdateResult> {
  const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const { data } = await apiClient.post<UpdateResult>('/admin/system/update', target, {
    headers: { 'Idempotency-Key': `pool-image-update-${requestID}` },
    timeout: 65000
  })
  return data
}

/**
 * Rollback to a previous version
 * @param version - Target version (e.g. "0.1.146"); omit to restore the local backup binary
 */
export async function rollback(version?: string): Promise<UpdateResult> {
  const { data } = await apiClient.post<UpdateResult>(
    '/admin/system/rollback',
    version ? { version } : undefined,
    { timeout: UPDATE_REQUEST_TIMEOUT_MS }
  )
  return data
}

/**
 * Restart the service
 */
export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart')
  return data
}

export const systemAPI = {
  getVersion,
  checkUpdates,
  performUpdate,
  getUpdateStatus,
  getRollbackVersions,
  rollback,
  restartService
}

export default systemAPI
