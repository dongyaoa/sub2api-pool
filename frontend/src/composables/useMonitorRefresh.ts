import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

interface MonitorRefreshOptions<T> {
  request: (signal: AbortSignal) => Promise<T>
  apply: (result: T) => void
  onError: (error: unknown) => void
  active?: () => boolean
  paused?: () => boolean
  intervalMs: () => number
}

// One request at a time. Background reads never cancel a slow read; an explicit
// refresh supersedes it, and stale responses cannot overwrite the newer result.
export function useMonitorRefresh<T>(options: MonitorRefreshOptions<T>) {
  const loading = ref(false)
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setInterval> | undefined
  let mounted = false
  let disposed = false
  let failures = 0
  const active = () => (options.active?.() ?? true) && !document.hidden
  function clearTimer() { clearInterval(timer); timer = undefined }
  function schedule() {
    clearTimer()
    if (!mounted || disposed || !active()) return
    const delay = Math.min(30000, options.intervalMs() * 2 ** Math.min(failures, 3))
    timer = setInterval(() => {
      if (options.paused?.() || loading.value) return
      void request(false)
    }, delay)
  }
  async function request(manual: boolean) {
    if (disposed || !active()) return
    if (!manual && (loading.value || options.paused?.())) { schedule(); return }
    clearTimer()
    controller?.abort()
    const current = new AbortController()
    controller = current
    loading.value = true
    try {
      const result = await options.request(current.signal)
      if (disposed || current.signal.aborted || controller !== current) return
      options.apply(result)
      failures = 0
    } catch (error) {
      if (disposed || current.signal.aborted || controller !== current) return
      failures++
      options.onError(error)
    } finally {
      if (controller === current) {
        loading.value = false
        controller = undefined
        schedule()
      }
    }
  }
  function refresh() { failures = 0; return request(true) }
  function suspend() { clearTimer(); controller?.abort(); controller = undefined; loading.value = false }
  function resume() { if (active()) void refresh(); else suspend() }
  watch(() => options.active?.() ?? true, () => { if (mounted) resume() })
  watch(options.intervalMs, () => { if (mounted && !loading.value) schedule() })
  onMounted(() => {
    mounted = true
    document.addEventListener('visibilitychange', resume)
    window.addEventListener('online', resume)
    if (active()) void refresh()
  })
  onBeforeUnmount(() => {
    disposed = true
    suspend()
    document.removeEventListener('visibilitychange', resume)
    window.removeEventListener('online', resume)
  })
  return { loading, refresh }
}
