<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-2 text-[11px]"><span class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 font-mono font-semibold text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200"><Icon name="lightbulb" size="xs" class="text-primary-500"/>{{ PELICAN_MODEL }}</span><span class="rounded-lg bg-primary-50 px-2.5 py-1.5 font-medium text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">high</span><details class="relative"><summary class="cursor-pointer list-none rounded-lg px-2 py-1.5 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700">{{ t('intelligenceMonitor.prompt') }}</summary><div class="absolute left-0 top-9 z-20 w-72 rounded-xl border border-gray-200 bg-white p-4 text-xs leading-6 text-gray-600 shadow-lg dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300">{{ PELICAN_PROMPT }}</div></details></div><button type="button" class="btn btn-primary btn-sm" @click="openEditor()"><Icon name="plus" size="sm" class="mr-1.5"/>{{ t(oauthOnly ? 'intelligenceMonitor.oauth.add' : 'intelligenceMonitor.add') }}</button></div>
    <div class="flex flex-wrap items-center gap-3"><div class="relative min-w-[180px] flex-1 sm:max-w-sm"><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400"/><input v-model="search" class="input !py-2 !pl-9 text-xs" :placeholder="t('intelligenceMonitor.search')" :aria-label="t('intelligenceMonitor.search')"/></div><Select v-if="!oauthOnly" v-model="sourceFilter" class="source-filter w-36 max-w-full" :options="sourceOptions" :searchable="false" :aria-label="t('intelligenceMonitor.form.source')" /><button class="ml-auto flex items-center gap-1.5 rounded-lg px-2 py-2 text-xs text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="loading&&'animate-spin'"/>{{ t('intelligenceMonitor.refresh') }}</button></div>
    <p v-if="error" role="alert" class="rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10">{{ error }}</p>
    <div v-if="loading&&!loaded" class="space-y-3"><div v-for="n in 3" :key="n" class="card h-56 animate-pulse bg-gray-50 dark:bg-dark-800"/></div>
    <div v-else-if="visiblePlans.length" class="space-y-3">
      <IntelligencePlanCard v-for="plan in visiblePlans" :key="plan.id" :plan="plan" :overview="overview" :busy="busy.has(plan.id)" @run="run(plan)" @toggle="toggle(plan)" @edit="openEditor(plan)" @archive="archiving=plan" @history="runID => openHistory(plan, runID)" />
    </div>
    <div v-else class="flex min-h-[330px] flex-col items-center justify-center rounded-2xl border border-dashed border-gray-200 bg-white px-6 text-center dark:border-dark-700 dark:bg-dark-800/30"><div class="mb-5 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-primary-500 dark:bg-primary-500/10"><Icon name="lightbulb" size="xl"/></div><h3 class="text-base font-semibold text-gray-800 dark:text-gray-200">{{ t(search||sourceFilter?'intelligenceMonitor.noMatches':oauthOnly?'intelligenceMonitor.oauth.emptyTitle':'intelligenceMonitor.emptyTitle') }}</h3><p v-if="!search&&!sourceFilter" class="mt-2 max-w-lg text-xs leading-6 text-gray-500">{{ t(oauthOnly ? 'intelligenceMonitor.oauth.emptyHint' : 'intelligenceMonitor.emptyHint') }}</p><button v-if="!search&&!sourceFilter" class="btn btn-primary btn-sm mt-5" @click="openEditor()"><Icon name="plus" size="sm" class="mr-1.5"/>{{ t(oauthOnly ? 'intelligenceMonitor.oauth.add' : 'intelligenceMonitor.add') }}</button></div>
    <p class="text-[10px] text-gray-400 dark:text-dark-500">{{ t('intelligenceMonitor.retention') }}</p>
    <IntelligencePlanDialog :show="editor" :plan="editing" :overview="overview" :oauth-only="oauthOnly" @close="editor=false" @saved="saved"/>
    <IntelligenceHistoryDialog :show="history" :plan="selectedPlan" :initial-run-id="selectedRunID" @close="history=false"/>
    <BaseDialog :show="!!archiving" :title="t('intelligenceMonitor.archiveTitle')" width="narrow" :show-close-button="!deleting" :close-on-escape="!deleting" @close="!deleting&&(archiving=null)"><p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('intelligenceMonitor.archiveHint',{name:archiving?.name||''}) }}</p><template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="deleting" @click="archiving=null">{{ t('common.cancel') }}</button><button class="btn bg-rose-600 text-white hover:bg-rose-700" :disabled="deleting" @click="archive">{{ t('intelligenceMonitor.archive') }}</button></div></template></BaseDialog>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { intelligenceMonitorAPI, PELICAN_MODEL, PELICAN_PROMPT, type IntelligencePlan } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import IntelligencePlanCard from './IntelligencePlanCard.vue'
import IntelligencePlanDialog from './IntelligencePlanDialog.vue'
import IntelligenceHistoryDialog from './IntelligenceHistoryDialog.vue'
const props=withDefaults(defineProps<{overview:UpstreamOverview|null;oauthOnly?:boolean}>(),{oauthOnly:false})
const {t}=useI18n(),app=useAppStore()
const plans=ref<IntelligencePlan[]>([]),loading=ref(false),loaded=ref(false),error=ref(''),search=ref(''),sourceFilter=ref(''),busy=ref(new Set<number>())
const sources=['upstream','local_group','external']
const sourceOptions=computed(()=>[{value:'',label:t('intelligenceMonitor.allSources')},...sources.map(value=>({value,label:t(`intelligenceMonitor.source.${value}`)}))])
const editor=ref(false),editing=ref<IntelligencePlan|null>(null),history=ref(false),selectedID=ref<number|null>(null),selectedRunID=ref<number|null>(null),archiving=ref<IntelligencePlan|null>(null),deleting=ref(false)
const selectedPlan=computed(()=>plans.value.find(plan=>plan.id===selectedID.value)||null)
const visiblePlans=computed(()=>plans.value.filter(plan=>(props.oauthOnly ? plan.source_type==='openai_oauth' : plan.source_type!=='openai_oauth')&&(!sourceFilter.value||plan.source_type===sourceFilter.value)&&[plan.name,plan.source_name,plan.supplier_note,plan.group_note,plan.notes].join(' ').toLowerCase().includes(search.value.trim().toLowerCase())))
let controller:AbortController|undefined,timer:ReturnType<typeof setInterval>|undefined,disposed=false
async function load(){controller?.abort();const current=new AbortController();controller=current;loading.value=true;try{const result=await intelligenceMonitorAPI.plans(current.signal);if(!current.signal.aborted){plans.value=result.items||[];error.value='';loaded.value=true}}catch(err){if(!current.signal.aborted)error.value=extractApiErrorMessage(err,t('intelligenceMonitor.loadFailed'))}finally{if(!current.signal.aborted)loading.value=false}}
function openEditor(plan:IntelligencePlan|null=null){editing.value=plan;editor.value=true}
function openHistory(plan:IntelligencePlan,runID?:number){selectedID.value=plan.id;selectedRunID.value=runID||null;history.value=true}
function saved(){app.showSuccess(t('intelligenceMonitor.saved'));void load()}
async function action(id:number,callback:()=>Promise<unknown>){if(busy.value.has(id))return;busy.value=new Set([...busy.value,id]);try{await callback();if(!disposed)await load()}catch(err){app.showError(extractApiErrorMessage(err,t('intelligenceMonitor.actionFailed')))}finally{const ids=new Set(busy.value);ids.delete(id);busy.value=ids}}
function run(plan:IntelligencePlan){void action(plan.id,async()=>{await intelligenceMonitorAPI.run(plan.id);app.showSuccess(t('intelligenceMonitor.queued'))})}
function toggle(plan:IntelligencePlan){void action(plan.id,()=>intelligenceMonitorAPI.update(plan.id,{enabled:!plan.enabled}))}
async function archive(){if(!archiving.value||deleting.value)return;deleting.value=true;try{await intelligenceMonitorAPI.archive(archiving.value.id);archiving.value=null;app.showSuccess(t('intelligenceMonitor.archived'));await load()}catch(err){app.showError(extractApiErrorMessage(err,t('intelligenceMonitor.actionFailed')))}finally{deleting.value=false}}
onMounted(()=>{void load();timer=setInterval(()=>{if(!document.hidden&&!loading.value)void load()},5000)})
onBeforeUnmount(()=>{disposed=true;controller?.abort();clearInterval(timer)})
</script>
<style scoped>
.source-filter :deep(.select-trigger) { @apply px-3 py-2 text-xs; }
</style>
