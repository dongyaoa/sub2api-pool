<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2 text-[11px]"><span class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 font-mono font-semibold text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200"><Icon name="lightbulb" size="xs" class="text-primary-500"/>{{ PELICAN_MODEL }}</span><span class="rounded-lg bg-primary-50 px-2.5 py-1.5 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">high</span><details class="relative"><summary class="cursor-pointer list-none rounded-lg px-2 py-1.5 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700">{{ t('intelligenceMonitor.prompt') }}</summary><div class="absolute left-0 top-9 z-20 w-72 rounded-xl border border-gray-200 bg-white p-4 text-xs leading-6 text-gray-600 shadow-lg dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300">{{ PELICAN_PROMPT }}</div></details></div><button type="button" class="btn btn-primary btn-sm" @click="openEditor()"><Icon name="plus" size="sm" class="mr-1.5"/>{{ t(oauthOnly ? 'intelligenceMonitor.oauth.add' : 'intelligenceMonitor.add') }}</button></div>
    <div v-if="!oauthOnly && scopePlans.length" class="rounded-xl border border-gray-200/80 bg-white p-1.5 dark:border-dark-700 dark:bg-dark-800">
      <div role="tablist" :aria-label="t('intelligenceMonitor.quickSwitch')" class="flex min-w-0 items-center gap-1 overflow-x-auto" data-testid="intelligence-site-tabs" @keydown="navigateSiteTabs">
        <button v-for="site in siteTabs" :id="siteTabID(site.key)" :key="site.key" type="button" role="tab" :aria-selected="selectedSite === site.key" :tabindex="selectedSite === site.key ? 0 : -1" aria-controls="intelligence-site-results" :title="site.name" :data-site="site.key" class="flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500" :class="selectedSite === site.key ? 'bg-primary-50 font-semibold text-primary-700 dark:bg-primary-500/15 dark:text-primary-300' : 'text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white'" @click="selectedSite = site.key">
          <Icon v-if="site.key === 'all'" name="server" size="sm" />
          <span class="max-w-[180px] truncate">{{ site.name }}</span>
          <span class="rounded-md px-1.5 py-0.5 text-[10px] font-medium tabular-nums" :class="selectedSite === site.key ? 'bg-primary-100/70 text-primary-700 dark:bg-primary-500/20 dark:text-primary-300' : 'bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-400'">{{ site.count }}</span>
        </button>
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-3"><div class="relative min-w-[180px] flex-1 sm:max-w-sm"><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400"/><input v-model="search" class="input !py-2 !pl-9 text-xs" :placeholder="t('intelligenceMonitor.search')" :aria-label="t('intelligenceMonitor.search')"/></div><Select v-if="!oauthOnly" v-model="sourceFilter" class="source-filter w-36 max-w-full" :options="sourceOptions" :searchable="false" :aria-label="t('intelligenceMonitor.form.source')" /><div class="ml-auto flex items-center gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="scopePlans.length < 2" @click="orderDialog=true"><Icon name="menu" size="sm" class="mr-1.5"/>{{ t('upstreamCenter.order.open') }}</button><button type="button" class="flex items-center gap-1.5 rounded-lg px-2 py-2 text-xs text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="loading&&'animate-spin'"/>{{ t('intelligenceMonitor.refresh') }}</button></div></div>
    <p v-if="error" role="alert" class="rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10">{{ error }}</p>
    <div v-if="loading&&!loaded" class="space-y-3"><div v-for="n in 3" :key="n" class="card h-56 animate-pulse bg-gray-50 dark:bg-dark-800"/></div>
    <div :id="!oauthOnly ? 'intelligence-site-results' : undefined" :role="!oauthOnly && scopePlans.length ? 'tabpanel' : undefined" :aria-labelledby="!oauthOnly && scopePlans.length ? siteTabID(selectedSite) : undefined">
      <div v-show="visiblePlans.length" class="space-y-3">
        <IntelligencePlanCard v-for="plan in scopePlans" v-show="visiblePlanIDs.has(plan.id)" :key="plan.id" :hidden="!visiblePlanIDs.has(plan.id)" :visible="visiblePlanIDs.has(plan.id)" :plan="plan" :overview="overview" :busy="busy.has(plan.id)" @run="run(plan)" @toggle="toggle(plan)" @edit="openEditor(plan)" @archive="archiving=plan" @history="runID => openHistory(plan, runID)" />
      </div>
      <div v-if="(!loading || loaded) && !visiblePlans.length" class="flex min-h-[330px] flex-col items-center justify-center rounded-2xl border border-dashed border-gray-200 bg-white px-6 text-center dark:border-dark-700 dark:bg-dark-800/30"><div class="mb-5 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-primary-500 dark:bg-primary-500/10"><Icon name="lightbulb" size="xl"/></div><h3 class="text-base font-semibold text-gray-800 dark:text-gray-200">{{ t(search||sourceFilter||selectedSite!=='all'?'intelligenceMonitor.noMatches':oauthOnly?'intelligenceMonitor.oauth.emptyTitle':'intelligenceMonitor.emptyTitle') }}</h3><p v-if="!search&&!sourceFilter&&selectedSite==='all'" class="mt-2 max-w-lg text-xs leading-6 text-gray-500">{{ t(oauthOnly ? 'intelligenceMonitor.oauth.emptyHint' : 'intelligenceMonitor.emptyHint') }}</p><button v-if="!search&&!sourceFilter&&selectedSite==='all'" class="btn btn-primary btn-sm mt-5" @click="openEditor()"><Icon name="plus" size="sm" class="mr-1.5"/>{{ t(oauthOnly ? 'intelligenceMonitor.oauth.add' : 'intelligenceMonitor.add') }}</button></div>
    </div>
    <p class="text-[10px] text-gray-400 dark:text-dark-500">{{ t('intelligenceMonitor.retention') }}</p>
    <IntelligencePlanDialog :show="editor" :plan="editing" :overview="overview" :oauth-only="oauthOnly" @close="editor=false" @saved="saved"/>
    <IntelligenceHistoryDialog :show="history" :plan="selectedPlan" :initial-run-id="selectedRunID" @close="history=false"/>
    <UpstreamOrderDialog :show="orderDialog" :scope="oauthOnly ? 'oauth' : 'intelligence'" @close="orderDialog=false" @saved="savedOrder"/>
    <UpstreamDeleteDialog :show="!!archiving" :item="archiving ? { kind: 'intelligence', id: archiving.id, name: archiving.name } : null" :busy="deleting" :error="deleteError" @close="archiving = null" @confirm="archive" />
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import UpstreamDeleteDialog from './UpstreamDeleteDialog.vue'
import Select from '@/components/common/Select.vue'
import { intelligenceMonitorAPI, PELICAN_MODEL, PELICAN_PROMPT, type IntelligencePlan } from '@/api/admin/intelligenceMonitor'
import { upstreamCenterAPI, type UpstreamOverview } from '@/api/admin/upstreamCenter'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import IntelligencePlanCard from './IntelligencePlanCard.vue'
import IntelligencePlanDialog from './IntelligencePlanDialog.vue'
import IntelligenceHistoryDialog from './IntelligenceHistoryDialog.vue'
import UpstreamOrderDialog from './UpstreamOrderDialog.vue'
import { intelligencePanelActiveKey } from './intelligenceMonitorContext'
import { safeWebsite } from './safeWebsite'
const props=withDefaults(defineProps<{overview:UpstreamOverview|null;oauthOnly?:boolean;active?:boolean;refreshKey?:number}>(),{oauthOnly:false,active:true,refreshKey:0})
provide(intelligencePanelActiveKey, computed(() => props.active))
const {t}=useI18n(),app=useAppStore()
const plans=ref<IntelligencePlan[]>([]),loading=ref(false),loaded=ref(false),error=ref(''),search=ref(''),sourceFilter=ref(''),busy=ref(new Set<number>())
const sources=['upstream','local_group','external']
const sourceOptions=computed(()=>[{value:'',label:t('intelligenceMonitor.allSources')},...sources.map(value=>({value,label:t(`intelligenceMonitor.source.${value}`)}))])
const editor=ref(false),editing=ref<IntelligencePlan|null>(null),history=ref(false),selectedID=ref<number|null>(null),selectedRunID=ref<number|null>(null),archiving=ref<IntelligencePlan|null>(null),deleting=ref(false)
const orderDialog=ref(false)
const deleteError=ref('')
watch(archiving, () => { deleteError.value = '' })
const selectedPlan=computed(()=>plans.value.find(plan=>plan.id===selectedID.value)||null)
const scopePlans=computed(()=>plans.value.filter(plan=>props.oauthOnly ? plan.source_type==='openai_oauth' : plan.source_type!=='openai_oauth'))
const selectedSite = ref('all')
const planSites = computed(() => {
  const suppliersByTarget = new Map(props.overview?.suppliers.flatMap(supplier => supplier.targets.map(target => [target.id, supplier] as const)) || [])
  return new Map<number, { key: string; name: string }>(scopePlans.value.map(plan => {
    const supplier = plan.source_type === 'upstream' && plan.upstream_target_id ? suppliersByTarget.get(plan.upstream_target_id) : undefined
    if (supplier) return [plan.id, { key: `upstream:${supplier.id}`, name: supplier.name }] as const
    if (plan.source_type === 'local_group') return [plan.id, { key: 'local', name: t('intelligenceMonitor.source.local_group') }] as const
    const origin = safeWebsite(plan.endpoint || plan.latest_run?.source_endpoint)
    const note = plan.supplier_note.trim()
    const name = note || (origin ? new URL(origin).host : plan.source_name || plan.name)
    const identity = note ? `note:${note}` : origin || `plan:${plan.id}`
    return [plan.id, { key: `${plan.source_type}:${identity}`, name }] as const
  }))
})
const siteTabs = computed(() => {
  const sites = new Map<string, { key: string; name: string; count: number }>()
  // First appearance follows the saved plan order; filters never hide tabs.
  for (const site of planSites.value.values()) {
    const existing = sites.get(site.key)
    if (existing) existing.count++
    else sites.set(site.key, { ...site, count: 1 })
  }
  return [{ key: 'all', name: t('intelligenceMonitor.allSites'), count: scopePlans.value.length }, ...sites.values()]
})
watch(siteTabs, sites => {
  if (!sites.some(site => site.key === selectedSite.value)) selectedSite.value = 'all'
})
const siteTabID = (key: string) => `intelligence-site-${encodeURIComponent(key)}`
function navigateSiteTabs(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  const buttons = Array.from((event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('[role="tab"]'))
  const index = buttons.indexOf(event.target as HTMLButtonElement)
  if (index < 0) return
  event.preventDefault()
  const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length
  const next = buttons[nextIndex]
  next?.click()
  next?.focus({ preventScroll: true })
  next?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
}
const visiblePlans=computed(()=>scopePlans.value.filter(plan=>(selectedSite.value==='all'||planSites.value.get(plan.id)?.key===selectedSite.value)&&(!sourceFilter.value||plan.source_type===sourceFilter.value)&&[plan.name,plan.source_name,plan.supplier_note,plan.group_note,plan.notes,planSites.value.get(plan.id)?.name].join(' ').toLowerCase().includes(search.value.trim().toLowerCase())))
const visiblePlanIDs = computed(() => new Set(visiblePlans.value.map(plan => plan.id)))
let controller:AbortController|undefined,timer:ReturnType<typeof setInterval>|undefined,disposed=false
async function load(){controller?.abort();const current=new AbortController();controller=current;loading.value=true;try{const result=await intelligenceMonitorAPI.plans(current.signal);if(!current.signal.aborted){plans.value=result.items||[];error.value='';loaded.value=true}}catch(err){if(!current.signal.aborted)error.value=extractApiErrorMessage(err,t('intelligenceMonitor.loadFailed'))}finally{if(!current.signal.aborted)loading.value=false}}
function openEditor(plan:IntelligencePlan|null=null){editing.value=plan;editor.value=true}
function openHistory(plan:IntelligencePlan,runID?:number){selectedID.value=plan.id;selectedRunID.value=runID||null;history.value=true}
function saved(){app.showSuccess(t('intelligenceMonitor.saved'));void load()}
function savedOrder(){orderDialog.value=false;void load()}
async function action(id:number,callback:()=>Promise<unknown>){if(busy.value.has(id))return;busy.value=new Set([...busy.value,id]);try{await callback();if(!disposed&&props.active)await load()}catch(err){app.showError(extractApiErrorMessage(err,t('intelligenceMonitor.actionFailed')))}finally{const ids=new Set(busy.value);ids.delete(id);busy.value=ids}}
function run(plan:IntelligencePlan){void action(plan.id,async()=>{await intelligenceMonitorAPI.run(plan.id);app.showSuccess(t('intelligenceMonitor.queued'))})}
function toggle(plan:IntelligencePlan){void action(plan.id,()=>intelligenceMonitorAPI.update(plan.id,{enabled:!plan.enabled}))}
async function archive(mode:'archive'|'purge'){if(!archiving.value||deleting.value)return;const item=archiving.value;deleting.value=true;deleteError.value='';try{if(mode==='purge')await upstreamCenterAPI.purge({kind:'intelligence',id:item.id,confirm_name:item.name});else await intelligenceMonitorAPI.archive(item.id);if(disposed)return;archiving.value=null;app.showSuccess(t(mode==='purge'?'upstreamCenter.storage.purged':'intelligenceMonitor.archived'));if(props.active)await load()}catch(err){if(!disposed)deleteError.value=extractApiErrorMessage(err,t('upstreamCenter.storage.actionFailed'))}finally{deleting.value=false}}
watch(() => props.refreshKey, () => { if(props.active)void load() })
watch(() => props.active, active => {
  if (active) { void load(); return }
  controller?.abort()
  loading.value = false
  editor.value = false
  history.value = false
  orderDialog.value = false
  if (!deleting.value) archiving.value = null
})
onMounted(()=>{if(props.active)void load();timer=setInterval(()=>{if(props.active&&!document.hidden&&!loading.value&&!orderDialog.value)void load()},5000)})
onBeforeUnmount(()=>{disposed=true;controller?.abort();clearInterval(timer)})
</script>
<style scoped>
.source-filter :deep(.select-trigger) { @apply px-3 py-2 text-xs; }
</style>
