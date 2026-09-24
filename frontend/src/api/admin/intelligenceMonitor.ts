import { apiClient } from '../client'

export const PELICAN_MODEL = 'gpt-6-astra'
export const PELICAN_REASONING = 'high'
export const PELICAN_PROMPT = '创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。'
export type IntelligenceSource = 'external' | 'upstream' | 'local_group' | 'openai_oauth'
export type IntelligenceRunStatus = 'pending' | 'running' | 'succeeded' | 'failed'
export type IntelligenceRate = Record<string, unknown> | null

export interface IntelligencePlanInput {
  name: string
  source_type: IntelligenceSource
  endpoint?: string
  api_key?: string
  upstream_target_id?: number | null
  account_id?: number | null
  group_id?: number | null
  supplier_note: string
  group_note: string
  rate_note: string
  notes: string
  api_mode: 'responses' | 'chat_completions'
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
}
export interface IntelligenceRun {
  id: number
  plan_id: number
  status: IntelligenceRunStatus
  trigger: 'manual' | 'scheduled'
  created_at: string
  started_at: string | null
  finished_at: string | null
  duration_ms: number | null
  http_status: number | null
  error: string
  model: string
  reasoning_effort: string
  prompt: string
  source_type: IntelligenceSource
  source_name: string
  source_endpoint: string
  source_snapshot: Record<string, unknown> | null
  rate_snapshot: IntelligenceRate
  notes_snapshot: Record<string, unknown> | string | null
  html?: string
  raw_text?: string
}
export interface IntelligencePlan extends Omit<IntelligencePlanInput, 'api_key'> {
  id: number
  model: string
  reasoning_effort: string
  prompt: string
  api_key_masked: string
  source_name: string
  rate_snapshot: IntelligenceRate
  created_by: number
  created_at: string
  updated_at: string
  last_run_at: string | null
  next_run_at: string | null
  latest_run: IntelligenceRun | null
  recent_runs?: IntelligenceRun[]
}
export interface IntelligenceRunPage {
  items: IntelligenceRun[]
  total: number
  page: number
  page_size: number
}
const base = '/admin/intelligence-monitors'
export const intelligenceMonitorAPI = {
  async plans(signal?: AbortSignal): Promise<{ items: IntelligencePlan[] }> {
    return (await apiClient.get(`${base}/plans`, { signal })).data
  },
  async create(input: IntelligencePlanInput): Promise<IntelligencePlan> {
    return (await apiClient.post(`${base}/plans`, input)).data
  },
  async update(id: number, input: Partial<IntelligencePlanInput>): Promise<IntelligencePlan> {
    return (await apiClient.put(`${base}/plans/${id}`, input)).data
  },
  async archive(id: number): Promise<void> { await apiClient.delete(`${base}/plans/${id}`) },
  async run(id: number): Promise<IntelligenceRun> {
    return (await apiClient.post(`${base}/plans/${id}/run`)).data
  },
  async runs(planID: number, page = 1, signal?: AbortSignal): Promise<IntelligenceRunPage> {
    return (await apiClient.get(`${base}/runs`, { params: { plan_id: planID, page, page_size: 12 }, signal })).data
  },
  async detail(id: number, signal?: AbortSignal): Promise<IntelligenceRun> {
    return (await apiClient.get(`${base}/runs/${id}`, { signal })).data
  },
}
