import type { OpenAIAutoReauthAccount } from '@/api/admin/openaiAutoReauth'

const knownStatuses = new Set(['idle', 'queued', 'pending', 'starting', 'refreshing', 'browser_login', 'logging_in', 'awaiting_2fa', 'exchanging', 'verifying', 'running', 'ready', 'recovered', 'success', 'succeeded', 'failed', 'retry_wait', 'cooldown', 'blocked', 'needs_manual', 'awaiting_manual', 'manual_required', 'disabled', 'created', 'updated', 'imported', 'bound'])
const runningStatuses = new Set(['queued', 'pending', 'refreshing', 'browser_login', 'logging_in', 'awaiting_2fa', 'exchanging', 'verifying', 'running'])
type Status = Pick<OpenAIAutoReauthAccount, 'status' | 'enabled' | 'stage' | 'next_attempt_at'> & { attempts?: number }

export function isOpenAIAutoReauthRunning(status: string) { return runningStatuses.has(status) }

function displayState(account: Status): string {
  if (!account.enabled) return 'disabled'
  if (account.next_attempt_at && account.status === 'pending' && (account.attempts ?? 0) > 0) return 'retry_wait'
  return account.stage && knownStatuses.has(account.stage) ? account.stage : account.status
}

export function getOpenAIAutoReauthStateLabelKey(account: Status | string): string {
  const state = typeof account === 'string' ? account : displayState(account)
  return `admin.accounts.autoReauth.statuses.${knownStatuses.has(state) ? state : 'unknown'}`
}

export function getOpenAIAutoReauthStateClass(account: Status): string {
  const state = displayState(account)
  if (['idle', 'ready', 'recovered', 'success', 'succeeded', 'bound'].includes(state)) return 'text-emerald-700 dark:text-emerald-400'
  if (['failed', 'blocked', 'needs_manual', 'awaiting_manual', 'manual_required'].includes(state)) return 'text-red-600 dark:text-red-400'
  if (runningStatuses.has(state) || state === 'retry_wait') return 'text-blue-600 dark:text-blue-400'
  return 'text-gray-500 dark:text-dark-400'
}
