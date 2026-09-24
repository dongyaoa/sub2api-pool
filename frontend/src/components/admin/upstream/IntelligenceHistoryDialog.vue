<template>
  <BaseDialog :show="show" :title="plan?.name || t('intelligenceMonitor.history')" width="full" @close="emit('close')">
    <div class="grid min-h-[520px] gap-5 lg:grid-cols-[225px_minmax(0,1fr)]">
      <aside class="min-w-0">
        <div class="mb-3 flex items-center justify-between">
          <p class="text-xs font-medium text-gray-500">{{ t('intelligenceMonitor.times', { count: total }) }}</p>
          <button type="button" class="rounded-md p-1 text-gray-400 hover:text-primary-600" :title="t('intelligenceMonitor.refresh')" @click="loadRuns"><Icon name="refresh" size="sm" :class="loading && 'animate-spin'" /></button>
        </div>
        <div class="flex max-h-[580px] gap-2 overflow-auto lg:flex-col">
          <button v-for="run in runs" :key="run.id" type="button" class="min-w-[190px] shrink-0 rounded-xl border p-3 text-left transition-colors lg:min-w-0" :class="selectedID === run.id ? 'border-primary-300 bg-primary-50/60 dark:border-primary-700 dark:bg-primary-500/10' : 'border-gray-100 hover:border-gray-300 dark:border-dark-700 dark:hover:border-dark-500'" @click="select(run.id)">
            <div class="flex items-center justify-between"><span class="font-mono text-[10px] text-gray-400">#{{ run.id }}</span><span class="text-[10px] font-medium" :class="run.status === 'failed' ? 'text-rose-600' : run.status === 'succeeded' ? 'text-emerald-600' : 'text-amber-600'">{{ t(`intelligenceMonitor.status.${run.status}`) }}</span></div>
            <p class="mt-1.5 text-xs font-medium text-gray-800 dark:text-gray-200">{{ dateTime(run.started_at || run.created_at) }}</p>
            <p class="mt-1 truncate text-[10px] text-gray-500">{{ run.model }} · {{ run.reasoning_effort }}</p>
          </button>
        </div>
        <nav v-if="pageCount > 1" class="mt-3 grid grid-cols-[32px_minmax(0,1fr)_32px] items-center gap-2 border-t border-gray-100 pt-3 dark:border-dark-700" :aria-label="t('intelligenceMonitor.history')" data-testid="history-pagination">
          <button type="button" class="history-page-button" :disabled="loading || page <= 1" :aria-label="t('pagination.previous')" :title="t('pagination.previous')" data-testid="previous-page" @click="changePage(page - 1)"><Icon name="chevronLeft" size="sm" /></button>
          <p class="whitespace-nowrap text-center text-xs tabular-nums text-gray-400" :aria-label="t('pagination.pageOf', { page, total: pageCount })"><span class="font-semibold text-gray-700 dark:text-gray-200" aria-current="page" data-testid="page">{{ page }}</span><span class="mx-1.5">/</span>{{ pageCount }}</p>
          <button type="button" class="history-page-button" :disabled="loading || page >= pageCount" :aria-label="t('pagination.next')" :title="t('pagination.next')" data-testid="next-page" @click="changePage(page + 1)"><Icon name="chevronRight" size="sm" /></button>
        </nav>
      </aside>
      <section class="min-w-0">
        <p v-if="error" role="alert" class="mb-3 rounded-xl bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10">{{ error }}</p>
        <div v-if="detailLoading" class="flex h-[420px] items-center justify-center"><Icon name="refresh" class="animate-spin text-primary-500" size="lg"/></div>
        <template v-else-if="detail">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-3"><div class="flex rounded-lg bg-gray-100 p-1 dark:bg-dark-900"><button v-for="mode in modes" :key="mode" class="rounded-md px-3 py-1.5 text-xs font-medium" :class="view===mode ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-gray-100' : 'text-gray-500'" @click="view=mode">{{ t(`intelligenceMonitor.${mode}`) }}</button></div><button v-if="detail.html" type="button" class="btn btn-secondary btn-sm" @click="download"><Icon name="download" size="sm" class="mr-1.5"/>{{ t('intelligenceMonitor.download') }}</button></div>
          <div class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700"><IntelligenceArtifactPreview v-if="view==='preview'" :run="detail" large/><pre v-else class="max-h-[520px] min-h-[420px] overflow-auto bg-slate-950 p-5 font-mono text-xs leading-6 text-slate-200">{{ view==='sourceCode' ? detail.html || t('intelligenceMonitor.noHTML') : detail.raw_text || detail.error || '—' }}</pre></div>
          <div class="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3"><div v-for="item in metadata" :key="item.label" class="min-w-0"><p class="text-[10px] text-gray-400 dark:text-dark-400">{{ t(`intelligenceMonitor.${item.label}`) }}</p><p class="mt-1 break-words text-xs font-medium text-gray-700 dark:text-gray-200">{{ item.value }}</p></div></div>
          <p v-if="detail.error" class="mt-3 rounded-lg bg-rose-50 px-3 py-2 text-xs text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ detail.error }}</p>
          <p v-if="runNotes" class="mt-3 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.notes') }}：{{ runNotes }}</p>
          <details class="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700"><summary class="cursor-pointer text-[11px] text-gray-500">{{ t('intelligenceMonitor.prompt') }}</summary><p class="mt-2 text-xs leading-6 text-gray-600 dark:text-dark-300">{{ detail.prompt }}</p></details>
        </template>
        <div v-else-if="!detailLoading" class="flex min-h-[420px] items-center justify-center rounded-xl border border-dashed border-gray-200 text-sm text-gray-400 dark:border-dark-700">{{ t('intelligenceMonitor.selectRun') }}</div>
      </section>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, type IntelligencePlan, type IntelligenceRun } from '@/api/admin/intelligenceMonitor'
import { extractApiErrorMessage } from '@/utils/apiError'
import IntelligenceArtifactPreview from './IntelligenceArtifactPreview.vue'
import { intelligenceNotes, intelligenceRateLabel } from './intelligencePreview'
import { intelligenceDurationLabel } from './intelligenceDuration'
import { dateTime } from './format'
const props=defineProps<{show:boolean;plan:IntelligencePlan|null;initialRunId?:number|null}>()
const emit=defineEmits<{close:[]}>()
const {t}=useI18n()
const runs=ref<IntelligenceRun[]>([]),total=ref(0),page=ref(1),selectedID=ref<number|null>(null),detail=ref<IntelligenceRun|null>(null),loading=ref(false),detailLoading=ref(false),error=ref('')
const modes=['preview','sourceCode','response'] as const
const view=ref<typeof modes[number]>('preview')
const runNotes=computed(()=>intelligenceNotes(detail.value?.notes_snapshot))
const pageCount=computed(()=>Math.max(1,Math.ceil(total.value/12)))
const metadata=computed(()=>{
  const run=detail.value; if (!run) return []
  const multiplier=intelligenceRateLabel(run.rate_snapshot)
  return [{label:'started',value:dateTime(run.started_at || run.created_at)},{label:'finished',value:dateTime(run.finished_at)},{label:'duration',value:intelligenceDurationLabel(run,t)||'—'},{label:'model',value:run.model},{label:'reasoning',value:run.reasoning_effort},{label:'rateAtRun',value:multiplier ? `${multiplier}${run.rate_snapshot?.stale ? ` · ${t('intelligenceMonitor.rateStale')}` : ''}` : t('intelligenceMonitor.rateUnknown')},{label:'runSource',value:run.source_name || run.source_endpoint || t(`intelligenceMonitor.source.${run.source_type}`)},{label:'http',value:run.http_status || '—'}]
})
let listController:AbortController|undefined,detailController:AbortController|undefined,timer:ReturnType<typeof setInterval>|undefined,initialSelection:number|null=null
async function changePage(nextPage:number){
  if(loading.value||nextPage<1||nextPage>pageCount.value||nextPage===page.value)return
  page.value=nextPage
  await loadRuns()
}
async function loadRuns(){
  if (!props.show||!props.plan) return
  listController?.abort();const current=new AbortController();listController=current;loading.value=true;error.value=''
  try {
    const wanted=initialSelection
    let loadedPage=page.value
    let result=await intelligenceMonitorAPI.runs(props.plan.id,loadedPage,current.signal)
    if(current.signal.aborted)return
    // recent_runs omits active jobs, while this endpoint includes them. Locate
    // the clicked work against the actual pages so an inserted job cannot shift
    // a boundary item to the next page and silently select a different run.
    while(wanted&&!result.items?.some(run=>run.id===wanted)&&loadedPage*12<result.total){
      result=await intelligenceMonitorAPI.runs(props.plan.id,++loadedPage,current.signal)
      if(current.signal.aborted)return
    }
    initialSelection=null;page.value=loadedPage;runs.value=result.items||[];total.value=result.total
    if(!runs.value.some(run=>run.id===selectedID.value)){await select(runs.value[0]?.id||null)}else{const selected=runs.value.find(run=>run.id===selectedID.value);if(selected&&!detailLoading.value&&(!detail.value||detail.value.status!==selected.status))await select(selected.id)}
  }
  catch(err){if(!current.signal.aborted)error.value=extractApiErrorMessage(err,t('intelligenceMonitor.loadFailed'))}
  finally{if(!current.signal.aborted)loading.value=false}
}
async function select(id:number|null){
  detailController?.abort();selectedID.value=id;detail.value=null;detailLoading.value=false;error.value='';if(!id)return
  const current=new AbortController();detailController=current;detailLoading.value=true
  try{const result=await intelligenceMonitorAPI.detail(id,current.signal);if(!current.signal.aborted)detail.value=result}
  catch(err){if(!current.signal.aborted)error.value=extractApiErrorMessage(err,t('intelligenceMonitor.loadFailed'))}
  finally{if(!current.signal.aborted)detailLoading.value=false}
}
function download(){if(!detail.value?.html)return;const url=URL.createObjectURL(new Blob([detail.value.html],{type:'text/html;charset=utf-8'}));const link=document.createElement('a');link.href=url;link.download=`pelican-${detail.value.id}.html`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
watch([()=>props.show,()=>props.plan?.id,()=>props.initialRunId],()=>{listController?.abort();detailController?.abort();clearInterval(timer);runs.value=[];total.value=0;detail.value=null;loading.value=false;detailLoading.value=false;error.value='';initialSelection=props.initialRunId||null;selectedID.value=initialSelection;page.value=1;view.value='preview';if(props.show){void loadRuns();timer=setInterval(()=>{if(!document.hidden&&!loading.value)void loadRuns()},5000)}},{immediate:true})
onBeforeUnmount(()=>{listController?.abort();detailController?.abort();clearInterval(timer)})
</script>
<style scoped>
.history-page-button { @apply flex h-8 w-8 items-center justify-center rounded-lg border border-gray-200 bg-white text-gray-500 transition-colors hover:border-primary-300 hover:bg-primary-50 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-300 dark:hover:border-primary-600 dark:hover:bg-primary-500/10 dark:hover:text-primary-400; }
</style>
