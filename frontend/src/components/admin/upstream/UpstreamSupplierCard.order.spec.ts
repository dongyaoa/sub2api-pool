import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamSupplierCard from './UpstreamSupplierCard.vue'
import type { UpstreamSupplier } from '@/api/admin/upstreamCenter'

vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
describe('supplier key-group ordering entry', () => {
  it('emits only a group-order request for its supplier and keeps target order', async () => {
    const supplier={id:5,name:'Supplier',website:'',targets:[{id:12},{id:7}],wallets:[],finance:{}} as unknown as UpstreamSupplier
    const view=mount(UpstreamSupplierCard,{props:{supplier,busyIds:new Set<number>(),runningIds:new Set<number>()},global:{stubs:{Icon:true,UpstreamWallet:true,UpstreamGroupRow:true}}})
    const button=view.findAll('button').find(item=>item.text()==='upstreamCenter.order.groups')!
    await button.trigger('click')
    expect(view.emitted('order-groups')).toEqual([[supplier]])
    expect(view.findAllComponents({name:'UpstreamGroupRow'}).map(item=>item.props('target').id)).toEqual([12,7])
    expect(view.emitted('run')).toBeUndefined()
    await view.setProps({supplier:{...supplier,targets:supplier.targets.slice(0,1)}})
    expect(view.findAll('button').some(item=>item.text()==='upstreamCenter.order.groups')).toBe(false)
    view.unmount()
  })
})
