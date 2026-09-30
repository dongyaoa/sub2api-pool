import { apiClient } from './client'

export interface PelicanMonitorConfig { enabled: boolean; title: string; description: string; notice: string }
export interface PelicanRun {
  id: number
  plan_id: number
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  model: string
  reasoning_effort: string
  created_at: string
  started_at: string | null
  finished_at: string | null
  duration_ms: number | null
  error: string
  html?: string
}
export interface PelicanMonitorGroup {
  id: number
  group_name: string
  group_rate_multiplier: number | null
  enabled: boolean
  interval_seconds: number
  next_run_at: string | null
  last_run_at: string | null
  model: string
  reasoning_effort: string
  latest_run: PelicanRun | null
  recent_runs: PelicanRun[]
}
export interface PelicanMonitorSnapshot { config: PelicanMonitorConfig; server_time: string; items: PelicanMonitorGroup[] }
const base = '/pelican-monitor'
export const pelicanMonitorAPI = {
  async config(signal?: AbortSignal): Promise<PelicanMonitorConfig> { return (await apiClient.get(`${base}/config`, { signal })).data },
  async list(signal?: AbortSignal): Promise<PelicanMonitorSnapshot> { return (await apiClient.get(base, { signal })).data },
  async detail(id: number, signal?: AbortSignal): Promise<PelicanRun> { return (await apiClient.get(`${base}/runs/${id}`, { signal })).data },
}
