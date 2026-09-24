import type { Proxy } from '@/types'

export type ProxyUnavailableReason = 'expired' | 'inactive' | 'unavailable'

export function proxyUnavailableReason(
  proxy: Proxy | null | undefined,
  now = Date.now()
): ProxyUnavailableReason | null {
  if (!proxy || !Number.isSafeInteger(proxy.id) || proxy.id <= 0) return 'unavailable'
  if (proxy.status === 'expired') return 'expired'
  if (proxy.expires_at) {
    const expiresAt = Date.parse(proxy.expires_at)
    if (!Number.isFinite(expiresAt)) return 'unavailable'
    if (expiresAt <= now) return 'expired'
  }
  if (proxy.status !== 'active') return 'inactive'
  return null
}

export function isProxySelectable(proxy: Proxy | null | undefined, now = Date.now()): proxy is Proxy {
  return proxyUnavailableReason(proxy, now) === null
}
