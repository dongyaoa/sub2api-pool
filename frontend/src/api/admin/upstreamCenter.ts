import { apiClient } from '../client'

export type UpstreamWindow = '24h' | '7d' | '30d'
export type UpstreamStatus = 'operational' | 'degraded' | 'failed' | 'error' | 'unknown'
export type UpstreamProvider = 'openai' | 'anthropic' | 'gemini'

export interface UpstreamHistoryRecord {
  id: number
  target_id: number
  model: string
  status: UpstreamStatus
  latency_ms: number | null
  ping_latency_ms: number | null
  http_status: number | null
  message: string
  checked_at: string
  cost: number | null
  cost_source: 'unknown' | 'estimated' | 'reported'
}

export interface UpstreamModelStatistics {
  model: string
  status: UpstreamStatus
  availability: number | null
  availability_7d: number | null
  latest_latency_ms: number | null
  avg_latency_ms: number | null
  p95_latency_ms: number | null
  sample_count: number
  success_count: number
  last_checked_at: string | null
  timeline: UpstreamHistoryRecord[]
}

export interface UpstreamBalanceSnapshot {
  target_id: number
  wallet_ref: string
  kind: 'wallet' | 'key_quota' | 'subscription' | 'unsupported' | 'unknown'
  balance: number | null
  quota_remaining: number | null
  today_used: number | null
  total_used: number | null
  currency: string
  status: 'ok' | 'error' | 'unsupported' | 'pending'
  synced_at: string | null
  last_attempt_at?: string | null
  currency_source?: 'reported' | 'sub2api_default'
  error: string
  billing?: UpstreamBillingSnapshot | null
}

export interface UpstreamBillingSnapshot {
  group_id: number | null
  group_name: string | null
  group_rate_multiplier: number | null
  user_rate_multiplier: number | null
  resolved_rate_multiplier: number | null
  effective_rate_multiplier: number | null
  billing_scope: 'token'
  source: 'sub2api_billing' | 'sub2api_usage' | 'unknown'
  status: 'ok' | 'error' | 'unsupported' | 'pending'
  stale: boolean
  synced_at: string | null
  last_attempt_at: string | null
  observed_at: string | null
  error: string
}

export interface UpstreamFinanceSummary {
  revenue: number
  business_cost: number
  monitor_cost: number | null
  profit: number | null
  request_count: number
  total_tokens: number | null
  unknown_token_requests: number
  account_billed: number
  cost_source: 'estimated' | 'reported' | 'mixed' | 'unknown'
  currency: string
  from: string
  to: string
  remote_used: number | null
  reconciliation_delta: number | null
  unpriced_monitor_count: number
}

export interface UpstreamTargetInput {
  supplier_id: number | null
  name: string
  provider: UpstreamProvider
  api_mode: 'chat_completions' | 'responses'
  endpoint: string
  api_key?: string
  source_account_id?: number
  models: string[]
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  degraded_threshold_ms: number
  account_ids: number[]
  wallet_ref: string
  notes: string
}

export interface UpstreamTarget extends Omit<UpstreamTargetInput, 'api_key' | 'source_account_id'> {
  id: number
  api_key_masked: string
  created_at: string
  updated_at: string
  last_checked_at: string | null
  next_check_at: string | null
  statistics: UpstreamModelStatistics[]
  balance: UpstreamBalanceSnapshot | null
  finance: UpstreamFinanceSummary
}

export interface UpstreamSupplierInput { name: string; website: string; notes: string }
export interface UpstreamSupplier extends UpstreamSupplierInput {
  id: number
  created_at: string
  updated_at: string
  targets: UpstreamTarget[]
  finance: UpstreamFinanceSummary
  wallets: UpstreamBalanceSnapshot[]
}
export interface UpstreamOverview {
  suppliers: UpstreamSupplier[]
  monitors: UpstreamTarget[]
  summary: UpstreamFinanceSummary
}
export interface UpstreamFinanceRow {
  id: number
  created_at: string
  target_id: number
  target_name: string
  supplier_id: number | null
  supplier_name: string
  account_id: number | null
  group_id: number | null
  model: string
  request_id: string
  revenue: number
  business_cost: number
  profit: number
  billing_type: number
}
export interface UpstreamPage<T> { items: T[]; total: number; page: number; page_size: number }
export interface UpstreamFinancePage extends UpstreamPage<UpstreamFinanceRow> { summary: UpstreamFinanceSummary }
export interface UpstreamPageQuery { page?: number; page_size?: number; from?: string; to?: string }
const base = '/admin/upstream-center'

export const upstreamCenterAPI = {
  async overview(window: UpstreamWindow = '24h', signal?: AbortSignal): Promise<UpstreamOverview> {
    return (await apiClient.get<UpstreamOverview>(`${base}/overview`, { params: { window }, signal })).data
  },
  async createSupplier(input: UpstreamSupplierInput): Promise<UpstreamSupplier> {
    return (await apiClient.post<UpstreamSupplier>(`${base}/suppliers`, input)).data
  },
  async updateSupplier(id: number, input: UpstreamSupplierInput): Promise<UpstreamSupplier> {
    return (await apiClient.put<UpstreamSupplier>(`${base}/suppliers/${id}`, input)).data
  },
  async deleteSupplier(id: number): Promise<void> { await apiClient.delete(`${base}/suppliers/${id}`) },
  async createTarget(input: UpstreamTargetInput): Promise<UpstreamTarget> {
    return (await apiClient.post<UpstreamTarget>(`${base}/targets`, input)).data
  },
  async updateTarget(id: number, input: Partial<UpstreamTargetInput>): Promise<UpstreamTarget> {
    return (await apiClient.put<UpstreamTarget>(`${base}/targets/${id}`, input)).data
  },
  async deleteTarget(id: number): Promise<void> { await apiClient.delete(`${base}/targets/${id}`) },
  async run(id: number): Promise<UpstreamHistoryRecord[]> {
    return (await apiClient.post<UpstreamHistoryRecord[]>(`${base}/targets/${id}/run`, undefined, { timeout: 420000 })).data
  },
  async syncBalance(id: number): Promise<UpstreamBalanceSnapshot> {
    return (await apiClient.post<UpstreamBalanceSnapshot>(`${base}/targets/${id}/sync-balance`, undefined, { timeout: 60000 })).data
  },
  async models(input: { target_id?: number; account_id?: number; provider: UpstreamProvider; endpoint: string; api_key?: string }): Promise<string[]> {
    return (await apiClient.post<{ models: string[] }>(`${base}/models`, input, { timeout: 60000 })).data.models
  },
  async history(id: number, params: UpstreamPageQuery & { model?: string }, signal?: AbortSignal): Promise<UpstreamPage<UpstreamHistoryRecord>> {
    return (await apiClient.get<UpstreamPage<UpstreamHistoryRecord>>(`${base}/targets/${id}/history`, { params, signal })).data
  },
  async finance(params: UpstreamPageQuery & { supplier_id?: number; target_id?: number }, signal?: AbortSignal): Promise<UpstreamFinancePage> {
    return (await apiClient.get<UpstreamFinancePage>(`${base}/finance`, { params, signal })).data
  },
}
