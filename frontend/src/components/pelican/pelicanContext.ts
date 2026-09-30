import type { InjectionKey, Ref } from 'vue'
export const pelicanPageActiveKey: InjectionKey<Ref<boolean>> = Symbol('pelican-page-active')
export const pelicanPreviewRefreshKey: InjectionKey<Ref<number>> = Symbol('pelican-preview-refresh')
