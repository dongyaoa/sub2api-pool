import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UpstreamCenterView from '../UpstreamCenterView.vue'
import type { UpstreamOverview, UpstreamSupplier, UpstreamTarget } from '@/api/admin/upstreamCenter'

const mocks = vi.hoisted(() => ({ overview: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/admin/upstreamCenter', () => ({ upstreamCenterAPI: { overview: mocks.overview } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const supplierCard = defineComponent({ props: ['supplier'], emits: ['order-groups'], template: '<article data-supplier><h2>{{ supplier.name }}</h2><button data-group-order @click="$emit(\'order-groups\',supplier)">groups</button></article>' })
const targetCard = defineComponent({ props: ['target'], template: '<article data-monitor>{{ target.name }}</article>' })
const orderDialog = defineComponent({ name: 'UpstreamOrderDialog', props: ['show','scope','supplierId','supplierName'], emits: ['close','saved'], template: '<div v-if="show" data-order-dialog><button data-cancel-order @click="$emit(\'close\')">cancel</button><button data-save-order @click="$emit(\'saved\')">save</button></div>' })
const target = (id: number, name: string) => ({ id, name, endpoint: 'https://example.com', models: ['model'], statistics: [] } as unknown as UpstreamTarget)
const supplier = (id: number, name: string) => ({ id, name, website: 'https://example.com', targets: [target(id*10,'Group A'),target(id*10+1,'Group B')] } as unknown as UpstreamSupplier)
const overview = (): UpstreamOverview => ({ suppliers: [supplier(1,'Alpha'),supplier(2,'Beta')], monitors: [target(3,'Monitor A'),target(4,'Monitor B')], summary: {} } as UpstreamOverview)
let wrapper: VueWrapper | undefined
function render() {
  wrapper = mount(UpstreamCenterView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, Icon: true, BaseDialog: true, EmptyState: true,
    IntelligenceMonitorPanel: true, UpstreamSupplierCard: supplierCard, UpstreamTargetCard: targetCard,
    UpstreamOrderDialog: orderDialog, UpstreamSupplierDialog: true, UpstreamTargetDialog: true, UpstreamDetailDialog: true,
  } } })
  return wrapper
}
beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers({ toFake: ['setInterval','clearInterval'] }); vi.spyOn(document,'hidden','get').mockReturnValue(false); mocks.overview.mockResolvedValue(overview()) })
afterEach(() => { wrapper?.unmount(); wrapper=undefined; vi.useRealTimers(); vi.restoreAllMocks() })

describe('upstream center manual ordering', () => {
  it('opens each full-list scope even when search hides all cards and does not save on cancel', async () => {
    const view = render(); await flushPromises()
    await view.get('input[aria-label="upstreamCenter.search"]').setValue('no match')
    expect(view.findAll('[data-supplier]')).toHaveLength(0)
    expect(view.get('[data-testid="upstream-order"]').attributes('disabled')).toBeUndefined()
    await view.get('[data-testid="upstream-order"]').trigger('click')
    expect(view.getComponent(orderDialog).props()).toMatchObject({ show:true, scope:'suppliers' })
    await view.get('[data-cancel-order]').trigger('click')
    expect(view.getComponent(orderDialog).props('show')).toBe(false)
    expect(mocks.overview).toHaveBeenCalledTimes(1)
    await view.get('#upstream-tab-monitors').trigger('click')
    await view.get('[data-testid="upstream-order"]').trigger('click')
    expect(view.getComponent(orderDialog).props('scope')).toBe('monitors')
  })

  it('passes the selected supplier as the key-group ordering scope', async () => {
    const view=render(); await flushPromises()
    await view.findAll('[data-group-order]')[1]!.trigger('click')
    expect(view.getComponent(orderDialog).props()).toMatchObject({show:true,scope:'groups',supplierId:2,supplierName:'Beta'})
  })

  it('keeps server order after saving, filtering and a later automatic refresh', async () => {
    const view=render(); await flushPromises()
    await view.get('[data-testid="upstream-order"]').trigger('click')
    await vi.advanceTimersByTimeAsync(30000)
    expect(mocks.overview).toHaveBeenCalledTimes(1)
    const ordered=overview(); ordered.suppliers.reverse()
    mocks.overview.mockResolvedValue(ordered)
    await view.get('[data-save-order]').trigger('click'); await flushPromises()
    expect(view.findAll('[data-supplier] h2').map(item=>item.text())).toEqual(['Beta','Alpha'])
    expect(view.getComponent(orderDialog).props('show')).toBe(false)
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    await view.get('input').setValue('Alpha')
    expect(view.findAll('[data-supplier] h2').map(item=>item.text())).toEqual(['Alpha'])
    await view.get('input').setValue('')
    await vi.advanceTimersByTimeAsync(30000); await flushPromises()
    expect(view.findAll('[data-supplier] h2').map(item=>item.text())).toEqual(['Beta','Alpha'])
  })

  it('aborts an older overview read when order saving refreshes the list', async () => {
    const view=render(); await flushPromises()
    let resolveOld: (value:UpstreamOverview)=>void=()=>undefined
    mocks.overview.mockReturnValueOnce(new Promise(resolve=>{resolveOld=resolve}))
    await vi.advanceTimersByTimeAsync(30000)
    const oldSignal=mocks.overview.mock.calls.at(-1)![1] as AbortSignal
    await view.get('[data-testid="upstream-order"]').trigger('click')
    const ordered=overview(); ordered.suppliers.reverse(); mocks.overview.mockResolvedValue(ordered)
    await view.get('[data-save-order]').trigger('click'); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    resolveOld(overview()); await flushPromises()
    expect(view.findAll('[data-supplier] h2').map(item=>item.text())).toEqual(['Beta','Alpha'])
  })

  it('disables list ordering when there are fewer than two items', async () => {
    mocks.overview.mockResolvedValue({...overview(),suppliers:[supplier(1,'Only')],monitors:[]})
    const view=render(); await flushPromises()
    expect(view.get('[data-testid="upstream-order"]').attributes('disabled')).toBeDefined()
    await view.get('#upstream-tab-monitors').trigger('click')
    expect(view.get('[data-testid="upstream-order"]').attributes('disabled')).toBeDefined()
  })
})
