import { beforeEach, describe, expect, it, vi } from 'vitest'
import { openaiAutoReauthAPI } from '../openaiAutoReauth'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
beforeEach(() => { vi.clearAllMocks(); client.get.mockResolvedValue({ data: { worker_configured: true, encryption_key_configured: true, accounts: [] } }); client.post.mockResolvedValue({ data: { results: [] } }); client.put.mockResolvedValue({ data: {} }) })

describe('OpenAI automatic authorization API', () => {
  it('passes credential content unchanged and uses the documented endpoints', async () => {
    const payload = { content: 'fixture@example.invalid----  test\\password  ----JBSWY3DPEHPK3PXP\r\n', proxy_id: 8, group_ids: [7] }
    const controller = new AbortController()
    await openaiAutoReauthAPI.list(controller.signal)
    await openaiAutoReauthAPI.importAccounts(payload)
    await openaiAutoReauthAPI.setEnabled(42, false)
    await openaiAutoReauthAPI.run(42)
    const credentials = { email: 'fixture@example.invalid', password: '  test\\password  ', totp_secret: 'JBSWY3DPEHPK3PXP', enabled: true }
    await openaiAutoReauthAPI.saveCredentials(42, credentials)
    expect(client.get).toHaveBeenCalledWith('/admin/openai/auto-reauth', { signal: controller.signal })
    expect(client.post).toHaveBeenCalledWith('/admin/openai/auto-reauth/import', payload)
    expect(client.put).toHaveBeenCalledWith('/admin/openai/accounts/42/auto-reauth', { enabled: false })
    expect(client.post).toHaveBeenCalledWith('/admin/openai/accounts/42/auto-reauth/run')
    expect(client.post).toHaveBeenCalledWith('/admin/openai/accounts/42/auto-reauth/credentials', credentials)
  })

  it('rejects an old backend HTML fallback without exposing response content', async () => {
    client.get.mockResolvedValue({ data: '<html>Old frontend</html>' })
    await expect(openaiAutoReauthAPI.list()).rejects.toEqual({ code: 'OPENAI_REAUTH_ENDPOINT_UNAVAILABLE' })
  })

  it('accepts an empty database and normalizes a legacy null list', async () => {
    client.get.mockResolvedValue({ data: { worker_configured: false, encryption_key_configured: true, accounts: null } })
    await expect(openaiAutoReauthAPI.list()).resolves.toEqual({ worker_configured: false, encryption_key_configured: true, accounts: [] })
  })
})
