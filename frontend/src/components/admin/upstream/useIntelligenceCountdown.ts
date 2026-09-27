import { computed, onBeforeUnmount, ref, watch } from 'vue'

/** A local display clock only; plan metadata is refreshed once by the parent. */
export function useIntelligenceCountdown(deadline: () => string | null | undefined, enabled: () => boolean) {
  const now = ref(Date.now())
  const timestamp = computed(() => { const value = Date.parse(deadline() || ''); return Number.isFinite(value) ? value : null })
  const remaining = computed(() => timestamp.value === null ? null : Math.max(0, Math.ceil((timestamp.value - now.value) / 1000)))
  const label = computed(() => {
    const seconds = remaining.value ?? 0
    return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60].map(value => String(value).padStart(2, '0')).join(':')
  })
  let timer: ReturnType<typeof setInterval> | undefined
  function stop() { clearInterval(timer); timer = undefined }
  watch([timestamp, enabled], () => {
    stop(); now.value = Date.now()
    if (!enabled() || !remaining.value) return
    timer = setInterval(() => { now.value = Date.now(); if (!remaining.value) stop() }, 1000)
  }, { immediate: true })
  onBeforeUnmount(stop)
  return { remaining, label }
}
