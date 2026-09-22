import { describe, expect, it } from 'vitest'
import { getOpenAIAutoReauthStateClass, getOpenAIAutoReauthStateLabelKey } from '../openaiAutoReauthStatus'

describe('automatic authorization status presentation', () => {
  it('uses current login stages, falls back for unknown stages, and respects disabled bindings', () => {
    const account = { enabled: true, status: 'running', stage: 'browser_login' }
    expect(getOpenAIAutoReauthStateLabelKey(account)).toBe('admin.accounts.autoReauth.statuses.browser_login')
    expect(getOpenAIAutoReauthStateLabelKey({ ...account, stage: 'future-stage' })).toBe('admin.accounts.autoReauth.statuses.running')
    expect(getOpenAIAutoReauthStateLabelKey({ ...account, enabled: false })).toBe('admin.accounts.autoReauth.statuses.disabled')
    expect(getOpenAIAutoReauthStateClass(account)).toContain('text-blue-600')
  })

  it('distinguishes a new queued task from a scheduled retry', () => {
    const account = { enabled: true, status: 'pending', stage: 'queued', attempts: 0, next_attempt_at: '2099-01-01' }
    expect(getOpenAIAutoReauthStateLabelKey(account)).toBe('admin.accounts.autoReauth.statuses.queued')
    expect(getOpenAIAutoReauthStateLabelKey({ ...account, attempts: 1 })).toBe('admin.accounts.autoReauth.statuses.retry_wait')
  })
})
