import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { openaiAutoReauthAPI, type OpenAIAutoReauthOverview } from '@/api/admin/openaiAutoReauth'

export const OPENAI_REAUTH_POLL_INTERVAL = 5000

export type OpenAIReauthLoadError = 'endpoint_unavailable' | 'schema_unavailable' | 'status_unavailable' | 'unauthorized' | 'forbidden' | 'server_error' | 'network_error'

function classifyLoadError(error: unknown): OpenAIReauthLoadError {
  const value = error as { status?: number; code?: string; reason?: string; message?: string; response?: { status?: number } } | null
  const status = value?.status ?? value?.response?.status
  const code = String(value?.reason || value?.message || value?.code || '').toLowerCase()
  if (code === 'auto_reauth_schema_unavailable') return 'schema_unavailable'
  if (code === 'auto_reauth_status_unavailable') return 'status_unavailable'
  if (status === 404 || value?.code === 'OPENAI_REAUTH_ENDPOINT_UNAVAILABLE') return 'endpoint_unavailable'
  if (status === 401) return 'unauthorized'
  if (status === 403) return 'forbidden'
  if (status && status >= 500) return 'server_error'
  return 'network_error'
}

/** Poll only while visible; ignore responses belonging to a closed/older panel. */
export function useOpenAIAutoReauth(active: Ref<boolean>) {
  const overview = ref<OpenAIAutoReauthOverview | null>(null)
  const loading = ref(false)
  const loadError = ref<OpenAIReauthLoadError | null>(null)
  const loadFailed = computed(() => loadError.value !== null)
  let timer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined
  let revision = 0

  function stop() {
    revision++
    clearTimeout(timer)
    timer = undefined
    controller?.abort()
    controller = undefined
    loading.value = false
  }

  async function refresh() {
    stop()
    if (!active.value) return
    const requestRevision = revision
    controller = new AbortController()
    loading.value = true
    try {
      const data = await openaiAutoReauthAPI.list(controller.signal)
      if (requestRevision !== revision || !active.value) return
      overview.value = data
      loadError.value = null
    } catch (error) {
      // Never render raw transport errors: they may contain request credentials.
      if (requestRevision === revision && active.value) loadError.value = classifyLoadError(error)
    } finally {
      if (requestRevision === revision && active.value) {
        loading.value = false
        if (!loadError.value || ['network_error', 'server_error', 'status_unavailable'].includes(loadError.value)) {
          timer = setTimeout(() => { void refresh() }, OPENAI_REAUTH_POLL_INTERVAL)
        }
      }
    }
  }

  watch(active, (visible) => {
    stop()
    overview.value = null
    loadError.value = null
    if (visible) void refresh()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(stop)

  return { overview, loading, loadFailed, loadError, refresh }
}
