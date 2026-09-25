import { describe, expect, it } from 'vitest'
import type { UpstreamBillingSnapshot } from '@/api/admin/upstreamCenter'
import { isNewapiBilling, upstreamSyncError } from './newapi'

describe('New API sync display', () => {
  it('recognizes only an identified New API snapshot', () => {
    expect(isNewapiBilling()).toBe(false)
    expect(isNewapiBilling({ source: 'sub2api_billing' } as UpstreamBillingSnapshot)).toBe(false)
    expect(isNewapiBilling({ source: 'newapi_account' } as UpstreamBillingSnapshot)).toBe(true)
    expect(isNewapiBilling({ provider: 'newapi' } as UpstreamBillingSnapshot)).toBe(true)
  })
  it('maps known errors and HTTP status while preserving unfamiliar diagnostic details', () => {
    const t = (key: string) => key
    expect(upstreamSyncError(null, t)).toBe('')
    expect(upstreamSyncError('newapi_account_auth_failed; newapi_upstream_http_403', t)).toBe('upstreamCenter.newapi.errors.authorizationFailed; upstreamCenter.newapi.errors.httpError (HTTP 403)')
    expect(upstreamSyncError('newapi_auto_group', t)).toBe('upstreamCenter.newapi.errors.autoGroup')
    expect(upstreamSyncError('newapi_rate_limited', t)).toBe('upstreamCenter.newapi.errors.rateLimited')
    expect(upstreamSyncError('Upstream connection timed out.', t)).toBe('Upstream connection timed out.')
  })
})
