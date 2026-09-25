import type { InjectionKey, Ref } from 'vue'

// Retained tabs keep their iframe DOM, but inactive tabs must not play artwork.
export const intelligencePanelActiveKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('intelligence-panel-active')
