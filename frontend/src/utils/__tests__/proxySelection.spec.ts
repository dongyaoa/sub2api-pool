import { describe, expect, it } from 'vitest'
import type { Proxy } from '@/types'
import { isProxySelectable, proxyUnavailableReason } from '../proxySelection'

const now = Date.parse('2026-09-24T00:00:00Z')
const proxy = (overrides: Partial<Proxy> = {}) => ({ id: 1, status: 'active', expires_at: null, ...overrides }) as Proxy

describe('proxy eligibility', () => {
  it('allows active unexpired proxies', () => {
    expect(isProxySelectable(proxy(), now)).toBe(true)
    expect(isProxySelectable(proxy({ expires_at: '2026-09-25T00:00:00Z' }), now)).toBe(true)
  })

  it('rejects expiry at the boundary even before the server updates the status', () => {
    expect(proxyUnavailableReason(proxy({ expires_at: '2026-09-24T00:00:00Z' }), now)).toBe('expired')
    expect(proxyUnavailableReason(proxy({ status: 'expired' }), now)).toBe('expired')
    expect(proxyUnavailableReason(proxy({ status: 'inactive' }), now)).toBe('inactive')
  })

  it('fails closed for missing, invalid or malformed proxy records', () => {
    expect(isProxySelectable(undefined, now)).toBe(false)
    expect(isProxySelectable(proxy({ id: 0 }), now)).toBe(false)
    expect(isProxySelectable(proxy({ expires_at: 'invalid' }), now)).toBe(false)
  })
})
