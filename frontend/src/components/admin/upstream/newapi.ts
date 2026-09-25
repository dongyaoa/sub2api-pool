import type { UpstreamBillingSnapshot } from '@/api/admin/upstreamCenter'

export function isNewapiBilling(billing?: UpstreamBillingSnapshot | null): boolean {
  return billing?.provider === 'newapi' || Boolean(billing?.source?.startsWith('newapi_'))
}

const errorKeys: Record<string, string> = {
  newapi_account_auth_required: 'authorizationRequired',
  newapi_account_auth_failed: 'authorizationFailed',
  newapi_account_identity_mismatch: 'identityMismatch',
  newapi_token_not_found: 'tokenNotFound',
  newapi_token_ambiguous: 'tokenAmbiguous',
  newapi_auto_group: 'autoGroup',
  newapi_group_rate_unavailable: 'rateUnavailable',
  newapi_quota_unit_unknown: 'quotaUnitUnknown',
  newapi_token_lookup_unsupported: 'lookupUnsupported',
  newapi_request_failed: 'requestFailed',
  newapi_response_unsupported: 'responseUnsupported',
  newapi_rate_limited: 'rateLimited',
}

export function upstreamSyncError(error: string | null | undefined, translate: (key: string) => string): string {
  return (error || '').replace(/\bnewapi_[a-z_0-9]+\b/g, code => {
    if (errorKeys[code]) return translate(`upstreamCenter.newapi.errors.${errorKeys[code]}`)
    const status = code.match(/^newapi_upstream_http_(\d{3})$/)?.[1]
    return status ? `${translate('upstreamCenter.newapi.errors.httpError')} (HTTP ${status})` : code
  })
}
