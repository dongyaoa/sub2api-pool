import type { InjectionKey, Ref } from 'vue'

// Retained tabs keep their iframe DOM, but inactive tabs must not play artwork.
export const intelligencePanelActiveKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('intelligence-panel-active')

// Incremented by the monitor panel after a manual refresh/mutation so previews
// can retry only missing or failed artwork details without rebuilding cards.
export const intelligencePreviewRefreshKey: InjectionKey<Readonly<Ref<number>>> = Symbol('intelligence-preview-refresh')
