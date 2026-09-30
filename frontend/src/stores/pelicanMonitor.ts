import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { pelicanMonitorAPI, type PelicanMonitorConfig } from '@/api/pelicanMonitor'
import { useAuthStore } from './auth'
import { resetPelicanArtworkAccess } from '@/components/pelican/pelicanArtworkLoader'

export const usePelicanMonitorStore = defineStore('pelican-monitor', () => {
  const auth = useAuthStore()
  const config = ref<PelicanMonitorConfig>({ enabled: false, title: '', description: '', notice: '' })
  const enabled = computed(() => auth.isAuthenticated && config.value.enabled)
  let loadedAt = 0, revision = 0
  let pending: Promise<void> | undefined, controller: AbortController | undefined
  function apply(value: PelicanMonitorConfig) {
    revision++
    controller?.abort(); controller = undefined; pending = undefined
    config.value = value
    loadedAt = Date.now()
    if (!value.enabled) resetPelicanArtworkAccess()
  }
  function disable() { apply({ enabled: false, title: '', description: '', notice: '' }) }
  function load(force = false): Promise<void> {
    if (!auth.isAuthenticated) { disable(); return Promise.resolve() }
    if (pending && !force) return pending
    if (force) { revision++; controller?.abort(); controller = undefined; pending = undefined }
    if (!force && loadedAt && Date.now() - loadedAt < 60000) return Promise.resolve()
    const current = ++revision, token = auth.token, userID = auth.user?.id
    controller = new AbortController()
    const signal = controller.signal
    pending = pelicanMonitorAPI.config(signal).then(value => {
      if (signal.aborted || revision !== current || auth.token !== token || auth.user?.id !== userID) return
      config.value = value; loadedAt = Date.now()
      if (!value.enabled) resetPelicanArtworkAccess()
    }).catch(() => {
      if (!signal.aborted && revision === current) { config.value = { enabled: false, title: '', description: '', notice: '' }; resetPelicanArtworkAccess() }
    }).finally(() => { if (revision === current) { pending = undefined; controller = undefined } })
    return pending
  }
  watch([() => auth.token, () => auth.isAuthenticated, () => auth.user?.id], () => {
    disable(); loadedAt = 0
    if (auth.isAuthenticated) void load()
  }, { immediate: true })
  return { config, enabled, apply, load, disable }
})
