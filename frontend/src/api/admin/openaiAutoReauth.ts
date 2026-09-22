import { apiClient } from '../client'

export interface OpenAIAutoReauthAccount {
  account_id: number
  email: string
  enabled: boolean
  status: string
  stage?: string
  attempts: number
  last_error_code?: string | null
  last_success_at?: string | null
  next_attempt_at?: string | null
}

export interface OpenAIAutoReauthOverview {
  worker_configured: boolean
  encryption_key_configured: boolean
  accounts: OpenAIAutoReauthAccount[]
}

export interface OpenAIAutoReauthImportResult {
  line: number
  account_id?: number
  email?: string
  status: string
  error_code?: string
}

export interface OpenAIAutoReauthImportInput {
  content: string
  mode?: 'create' | 'bind'
  proxy_id?: number
  group_ids?: number[]
}

export interface OpenAIAutoReauthCredentialsInput {
  email: string
  password: string
  totp_secret: string
  enabled: boolean
}

export const openaiAutoReauthAPI = {
  async list(signal?: AbortSignal): Promise<OpenAIAutoReauthOverview> {
    const { data } = await apiClient.get<OpenAIAutoReauthOverview>('/admin/openai/auto-reauth', { signal })
    // Older servers can return the SPA HTML with HTTP 200 for an unknown route.
    if (!data || typeof data !== 'object' || typeof data.worker_configured !== 'boolean'
      || typeof data.encryption_key_configured !== 'boolean'
      || (data.accounts !== null && !Array.isArray(data.accounts))) {
      throw { code: 'OPENAI_REAUTH_ENDPOINT_UNAVAILABLE' }
    }
    return { ...data, accounts: data.accounts ?? [] }
  },
  async importAccounts(input: OpenAIAutoReauthImportInput): Promise<{ results: OpenAIAutoReauthImportResult[] }> {
    // Preserve the source verbatim: whitespace and backslashes may be part of a password.
    const { data } = await apiClient.post<{ results: OpenAIAutoReauthImportResult[] }>('/admin/openai/auto-reauth/import', input)
    return data
  },
  async setEnabled(accountID: number, enabled: boolean): Promise<void> {
    await apiClient.put(`/admin/openai/accounts/${accountID}/auto-reauth`, { enabled })
  },
  async saveCredentials(accountID: number, input: OpenAIAutoReauthCredentialsInput): Promise<OpenAIAutoReauthAccount> {
    const { data } = await apiClient.post<OpenAIAutoReauthAccount>(`/admin/openai/accounts/${accountID}/auto-reauth/credentials`, input)
    return data
  },
  async run(accountID: number): Promise<void> {
    await apiClient.post(`/admin/openai/accounts/${accountID}/auto-reauth/run`)
  }
}
