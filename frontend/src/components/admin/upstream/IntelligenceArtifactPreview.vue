<template>
  <div ref="container" class="relative isolate overflow-hidden bg-slate-50 dark:bg-dark-900" :class="large ? 'aspect-[16/10]' : 'min-h-[144px] flex-1'" :aria-busy="active || loading" @mouseenter="hovered = true" @mouseleave="hovered = false" @focusin="focused = true" @focusout="focused = false">
    <iframe v-if="preview && !active" ref="frame" :key="`${run?.id}-${replay}`" :srcdoc="preview" sandbox="allow-scripts" credentialless referrerpolicy="no-referrer" scrolling="no" :title="t('intelligenceMonitor.preview')" class="absolute origin-top-left border-0 bg-white" :style="canvasStyle" :class="!large && 'pointer-events-none'" @load="syncPreview" />
    <PelicanLoadingScene v-else-if="active" :label="t(`intelligenceMonitor.status.${run?.status}`)" :queued="run?.status === 'pending'" />
    <div v-if="loading || (preview && !previewReady)" class="absolute inset-0 flex flex-col items-center justify-center gap-3 px-6" role="status">
      <div class="preview-skeleton relative h-10 w-16 overflow-hidden rounded-lg border border-slate-200 bg-white dark:border-dark-600 dark:bg-dark-800" aria-hidden="true"><div class="absolute bottom-2 left-2 right-2 h-1 rounded bg-slate-100 dark:bg-dark-600" /><div class="absolute left-2 top-2 h-2 w-2 rounded-full bg-slate-200 dark:bg-dark-600" /></div>
      <p class="text-[10px] font-medium text-slate-400 dark:text-dark-400">{{ t('intelligenceMonitor.loadingPreview') }}</p>
    </div>
    <div v-else-if="!preview && !active" class="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
      <svg class="text-slate-300 dark:text-dark-600" :class="large ? 'h-16 w-20' : 'h-9 w-12'" viewBox="0 0 96 72" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><circle cx="24" cy="52" r="14"/><circle cx="74" cy="52" r="14"/><path d="M24 52l17-26 15 26H24l11-17h31l8 17M39 25h10M65 35l-5-13h10"/><path d="M42 32c-13-5-14-19-5-25 8-5 18 0 20 9l23 4-26 7-9-3-3 8Z"/><circle cx="48" cy="13" r="1.2" fill="currentColor"/><path d="m39 31 6 12-8 8M44 44l8 7"/></svg>
      <p class="line-clamp-2 font-medium" :class="[large ? 'text-xs' : 'text-[10px]', run?.status === 'failed' || error ? 'text-rose-600 dark:text-rose-400' : 'text-slate-500 dark:text-dark-400']">{{ error || (run?.status === 'failed' ? t('intelligenceMonitor.status.failed') : run ? t('intelligenceMonitor.noHTML') : t('intelligenceMonitor.waiting')) }}</p>
      <p v-if="run?.status === 'failed' && run.error" class="max-w-full break-words text-[10px] leading-4 text-slate-400 dark:text-dark-400" :class="large ? 'line-clamp-4' : 'line-clamp-2'" :title="run.error">{{ run.error }}</p>
      <button v-if="error" type="button" class="text-xs text-primary-600" @click="load">{{ t('intelligenceMonitor.refresh') }}</button>
    </div>
    <button v-if="preview && !large" type="button" class="preview-open absolute inset-0 z-10 flex items-end justify-end bg-transparent p-2 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500" :aria-label="t('intelligenceMonitor.open')" @click="emit('open')"><span class="preview-open-label inline-flex items-center gap-1 rounded-md bg-white/90 px-2 py-1 text-[10px] font-medium text-gray-700 shadow-sm backdrop-blur-sm"><Icon name="eye" size="xs" />{{ t('intelligenceMonitor.open') }}</span></button>
    <div v-if="preview && large" class="absolute right-3 top-3 z-10"><button type="button" class="rounded-lg border border-gray-200 bg-white/90 p-2 text-gray-500 shadow-sm" :title="t('intelligenceMonitor.reloadPreview')" @click="replay++"><Icon name="refresh" size="sm" /></button></div>
  </div>
</template>
<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import { intelligencePreviewContent } from './intelligencePreview'
import PelicanLoadingScene from './PelicanLoadingScene.vue'
import { intelligencePanelActiveKey, intelligencePreviewRefreshKey } from './intelligenceMonitorContext'
import { loadIntelligenceArtwork } from './intelligenceArtworkLoader'
const props = withDefaults(defineProps<{ run: IntelligenceRun | null; large?: boolean }>(), { large: false })
const emit = defineEmits<{ open: [] }>()
const { t } = useI18n()
const container = ref<HTMLElement | null>(null), visible = ref(false), loading = ref(false), error = ref(''), html = ref(''), replay = ref(0)
const frame = ref<HTMLIFrameElement | null>(null)
const previewReady = ref(false)
const panelActive = inject(intelligencePanelActiveKey, ref(true))
const hovered = ref(false), focused = ref(false), pageVisible = ref(!document.hidden)
const refreshRevision = inject(intelligencePreviewRefreshKey, ref(0))
const playing = computed(() => panelActive.value && pageVisible.value && visible.value && (props.large || hovered.value || focused.value))
const canvasWidth = 960
const canvasHeight = 600
const viewport = ref({ width: 0, height: 0 })
const canvasScale = computed(() => {
  const { width, height } = viewport.value
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return 0
  const fit = props.large ? Math.min : Math.max
  return fit(width / canvasWidth, height / canvasHeight)
})
const canvasStyle = computed(() => {
  // Thumbnails fill the card; the detail view contains the complete artwork.
  // Keep the logical viewport fixed so resizing never restarts or reflows it.
  const scale = canvasScale.value
  return {
    width: `${canvasWidth}px`, height: `${canvasHeight}px`, transform: `scale(${scale})`,
    opacity: previewReady.value ? 1 : 0,
    pointerEvents: !previewReady.value || !props.large ? 'none' as const : 'auto' as const,
    left: `${(viewport.value.width - canvasWidth * scale) / 2}px`,
    top: `${(viewport.value.height - canvasHeight * scale) / 2}px`
  }
})
const active = computed(() => Boolean(props.run && ['pending', 'running'].includes(props.run.status)))
const prepared = computed(() => html.value ? intelligencePreviewContent(html.value, { autoplay: props.large, fit: props.large ? 'contain' : 'cover' }) : null)
const preview = computed(() => prepared.value?.document || '')
const fittedViewport = computed(() => {
  if (props.large) return { width: canvasWidth, height: canvasHeight }
  const scale = canvasScale.value
  return scale <= 0 ? null : { width: Math.min(canvasWidth, viewport.value.width / scale), height: Math.min(canvasHeight, viewport.value.height / scale) }
})
function onPreviewMessage(event: MessageEvent) {
  if (!frame.value?.contentWindow || event.source !== frame.value.contentWindow || event.data?.type !== 'intelligence-preview-fitted') return
  const expected = fittedViewport.value
  const { width, height } = event.data
  if (!expected || !Number.isFinite(width) || !Number.isFinite(height)) return
  if (Math.abs(width - expected.width) > 0.01 || Math.abs(height - expected.height) > 0.01) return
  previewReady.value = true
}
function syncPlayback() {
  frame.value?.contentWindow?.postMessage({ type: 'intelligence-preview-playback', playing: playing.value }, '*')
}
function syncViewport() {
  const expected = fittedViewport.value
  if (!expected) return
  // This is the visible slice of the fixed canvas after the card's cover scale.
  // Fit the document to that slice, avoiding a second layer of letterboxing.
  frame.value?.contentWindow?.postMessage({
    type: 'intelligence-preview-viewport',
    width: expected.width,
    height: expected.height
  }, '*')
}
function syncPreview() { syncViewport(); syncPlayback() }
function updatePageVisibility() { pageVisible.value = !document.hidden }
watch(playing, syncPlayback)
watch(fittedViewport, (current, previous) => {
  if (current && previous && (Math.abs(current.width - previous.width) > 0.01 || Math.abs(current.height - previous.height) > 0.01)) previewReady.value = false
  syncViewport()
})
watch([preview, replay], () => { previewReady.value = false })
watch(panelActive, async active => {
  if (!active) { hovered.value = false; focused.value = false; return }
  await nextTick()
  measureViewport()
  syncPreview()
})
let controller: AbortController | undefined, observer: IntersectionObserver | undefined, resizeObserver: ResizeObserver | undefined
let loadedIdentity = ''
let loadingIdentity = ''
const runIdentity = computed(() => {
  const run = props.run
  // Polls may fill duration/HTTP metadata after the HTML is already loaded.
  // Those fields do not identify a different artwork.
  return run ? `${run.id}:${run.status}:${run.finished_at || ''}` : ''
})
function measureViewport() {
  if (!container.value) return
  const { width, height } = container.value.getBoundingClientRect()
  updateViewport(width, height)
}
function updateViewport(width: number, height: number) {
  // A v-show tab reports zero when hidden. Preserve its last layout so opening
  // the tab doesn't briefly collapse the already-fitted iframe to scale(0).
  if (width <= 0 || height <= 0 || !Number.isFinite(width) || !Number.isFinite(height)) return
  if (width !== viewport.value.width || height !== viewport.value.height) viewport.value = { width, height }
}
function cancelLoad() {
  controller?.abort()
  controller = undefined
  loadingIdentity = ''
  loading.value = false
}
async function load() {
  if (!panelActive.value || !pageVisible.value || !visible.value || !props.run || props.run.status !== 'succeeded') return
  const identity = runIdentity.value
  if ((loadedIdentity === identity && html.value) || loadingIdentity === identity) return
  cancelLoad()
  error.value = ''
  loading.value = true
  loadingIdentity = identity
  const current = new AbortController(); controller = current
  try {
    if (props.run.html?.trim()) {
      if (!current.signal.aborted) {
        html.value = props.run.html
        loadedIdentity = identity
      }
      return
    }
    let artwork: string
    try {
      artwork = await loadIntelligenceArtwork(props.run, current.signal)
    } catch (cause) {
      // The shared loader also cancels when authorization changes, including a
      // token refresh. Retry once with the new authorization if still visible.
      if (current.signal.aborted || (cause as Error)?.name !== 'AbortError') throw cause
      artwork = await loadIntelligenceArtwork(props.run, current.signal)
    }
    if (!current.signal.aborted && runIdentity.value === identity) {
      html.value = artwork
      loadedIdentity = identity
    }
  } catch {
    if (!current.signal.aborted) error.value = t('intelligenceMonitor.loadFailed')
  } finally {
    if (controller === current) {
      loading.value = false
      loadingIdentity = ''
      controller = undefined
    }
  }
}
watch(runIdentity, identity => {
  if (identity === loadedIdentity) return
  cancelLoad()
  html.value = ''
  error.value = ''
  loading.value = false
  previewReady.value = false
  void load()
})
watch([visible, panelActive, pageVisible], ([inView, activePanel, activePage]) => {
  if (!inView || !activePanel || !activePage) cancelLoad()
  else void load()
})
watch(refreshRevision, () => {
  if (props.run?.status === 'succeeded' && (!html.value || error.value)) {
    cancelLoad()
    void load()
  }
})
onMounted(() => {
  window.addEventListener('message', onPreviewMessage)
  document.addEventListener('visibilitychange', updatePageVisibility)
  measureViewport()
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(entries => {
      const entry = entries.find(item => item.target === container.value)
      if (entry) updateViewport(entry.contentRect.width, entry.contentRect.height)
    })
    if (container.value) resizeObserver.observe(container.value)
  } else window.addEventListener('resize', measureViewport)
  if (props.large || typeof IntersectionObserver === 'undefined') { visible.value = true; return }
  observer = new IntersectionObserver(entries => {
    const entry = entries.find(item => item.target === container.value)
    if (entry) visible.value = entry.isIntersecting
  }, { rootMargin: '120px' })
  if (container.value) observer.observe(container.value)
})
onBeforeUnmount(() => { controller?.abort(); observer?.disconnect(); resizeObserver?.disconnect(); window.removeEventListener('resize', measureViewport); window.removeEventListener('message', onPreviewMessage); document.removeEventListener('visibilitychange', updatePageVisibility) })
</script>
<style scoped>
.preview-open-label { opacity: 0; transform: translateY(3px); transition: opacity .18s ease, transform .18s ease; }
.preview-open:hover .preview-open-label, .preview-open:focus-visible .preview-open-label { opacity: 1; transform: translateY(0); }
.preview-skeleton::after { content: ''; position: absolute; inset: 0; transform: translateX(-100%); background: linear-gradient(90deg, transparent, rgba(148, 163, 184, .12), transparent); animation: preview-shimmer 1.8s ease-in-out infinite; }
@keyframes preview-shimmer { to { transform: translateX(100%); } }
@media (prefers-reduced-motion: reduce) { .preview-open-label { transition: none; } .preview-skeleton::after { animation: none; } }
</style>
