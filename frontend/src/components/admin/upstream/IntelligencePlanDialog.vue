<template>
  <BaseDialog :show="show" :title="t(plan ? 'intelligenceMonitor.edit' : oauthOnly ? 'intelligenceMonitor.oauth.add' : 'intelligenceMonitor.add')" width="wide" :show-close-button="!saving" :close-on-escape="!saving" @close="!saving && emit('close')">
    <form id="intelligence-plan-form" class="space-y-5" @submit.prevent="save">
      <div class="flex items-center gap-3 rounded-xl border border-gray-100 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/50"><Icon name="lightbulb" size="md" class="text-primary-500"/><div class="min-w-0 flex-1"><p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ PELICAN_MODEL }} <span class="ml-2 rounded border border-primary-200 px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:border-primary-800">{{ PELICAN_REASONING }}</span></p><p class="mt-1 text-xs text-gray-500">{{ t('intelligenceMonitor.form.fixedPrompt') }}</p></div></div>
      <div v-if="!oauthOnly"><label for="intelligence-name" class="input-label">{{ t('intelligenceMonitor.form.name') }}</label><input id="intelligence-name" v-model="form.name" class="input" required maxlength="100" :placeholder="t('intelligenceMonitor.form.namePlaceholder')"/></div>
      <div v-if="!oauthOnly"><label class="input-label">{{ t('intelligenceMonitor.form.source') }}</label><div class="grid grid-cols-3 gap-2"><button v-for="source in sources" :key="source.value" type="button" class="flex items-center justify-center gap-2 rounded-xl border px-2 py-3 text-xs font-medium transition-colors" :class="form.source_type === source.value ? 'border-primary-400 bg-primary-50 text-primary-700 dark:border-primary-600 dark:bg-primary-500/10 dark:text-primary-300' : 'border-gray-200 text-gray-500 hover:border-gray-300 dark:border-dark-600 dark:text-dark-300'" :aria-pressed="form.source_type === source.value" @click="form.source_type = source.value"><Icon :name="source.icon" size="sm"/>{{ t(`intelligenceMonitor.source.${source.value}`) }}</button></div></div>
      <div v-if="oauthOnly" class="space-y-3">
        <div class="flex items-center gap-2"><span class="rounded-md bg-violet-50 px-2 py-1 text-xs font-semibold text-violet-600 dark:bg-violet-500/10 dark:text-violet-300">OpenAI OAuth</span><span class="text-xs text-gray-500">{{ t('intelligenceMonitor.oauth.nameHint') }}</span></div>
        <label class="input-label" for="intelligence-oauth-search">{{ t('intelligenceMonitor.oauth.select') }}</label>
        <div class="flex gap-2"><input id="intelligence-oauth-search" v-model="accountSearch" class="input" :placeholder="t('intelligenceMonitor.oauth.search')" @input="scheduleAccountSearch"/><button type="button" class="btn btn-secondary btn-sm" :disabled="accountsLoading" :aria-label="t('intelligenceMonitor.refresh')" @click="loadAccounts(1)"><Icon name="refresh" size="sm" :class="accountsLoading && 'animate-spin'"/></button></div>
        <div class="max-h-52 space-y-1 overflow-auto rounded-xl border border-gray-200 p-2 dark:border-dark-700">
          <button v-for="account in oauthAccounts" :key="account.id" type="button" class="flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2.5 text-left text-sm" :class="form.account_id===account.id ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-700'" :aria-pressed="form.account_id===account.id" @click="form.account_id=account.id; form.name=account.name"><span class="truncate">{{ account.name }}</span><span class="text-[10px] text-violet-500">OAuth</span></button>
          <p v-if="!accountsLoading&&!oauthAccounts.length" class="p-3 text-center text-xs text-gray-400">{{ t('intelligenceMonitor.oauth.noAccounts') }}</p>
        </div>
        <div v-if="accountTotal>30" class="flex justify-end gap-3 text-xs"><button type="button" :disabled="accountsLoading||accountPage<=1" class="text-primary-600 disabled:opacity-40" @click="loadAccounts(accountPage-1)">{{ t('intelligenceMonitor.oauth.previous') }}</button><span class="text-gray-400">{{ accountPage }} / {{ Math.ceil(accountTotal/30) }}</span><button type="button" :disabled="accountsLoading||accountPage*30>=accountTotal" class="text-primary-600 disabled:opacity-40" @click="loadAccounts(accountPage+1)">{{ t('intelligenceMonitor.oauth.more') }}</button></div>
        <p v-if="form.name" class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ form.name }}</p><p class="text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.oauth.hint') }}</p><p v-if="accountError" role="alert" class="text-xs text-rose-500">{{ accountError }}</p>
      </div>
      <div v-else-if="form.source_type === 'upstream'">
        <label for="intelligence-upstream" class="input-label">{{ t('intelligenceMonitor.form.selectUpstream') }}</label><select id="intelligence-upstream" v-model.number="form.upstream_target_id" class="input" required><option :value="null" disabled>{{ t('intelligenceMonitor.form.selectUpstream') }}</option><optgroup v-for="supplier in eligibleSuppliers" :key="supplier.id" :label="supplier.name"><option v-for="target in supplier.targets" :key="target.id" :value="target.id">{{ target.name }} · {{ upstreamRate(target.id) }}</option></optgroup></select>
        <div v-if="selectedTarget" class="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500"><span class="truncate">{{ domain(selectedTarget.endpoint) }}</span><span :class="selectedTarget.balance?.billing?.stale ? 'text-amber-600' : 'text-primary-600'">{{ t('intelligenceMonitor.rate') }} {{ upstreamRate(selectedTarget.id) }} · {{ t('intelligenceMonitor.autoRate') }}</span></div>
      </div>
      <div v-else-if="form.source_type === 'local_group'"><label for="intelligence-group" class="input-label">{{ t('intelligenceMonitor.form.selectGroup') }}</label><select id="intelligence-group" v-model.number="form.group_id" class="input" required><option :value="null" disabled>{{ t('intelligenceMonitor.form.selectGroup') }}</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }} · {{ group.rate_multiplier }}×</option></select><p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.form.sourceHint') }}</p><p v-if="groupError" role="alert" class="mt-1 text-xs text-rose-500">{{ groupError }}</p></div>
      <template v-else>
        <div class="grid gap-4 sm:grid-cols-2"><div><label for="intelligence-endpoint" class="input-label">{{ t('intelligenceMonitor.form.endpoint') }}</label><input id="intelligence-endpoint" v-model="form.endpoint" class="input" type="url" placeholder="https://api.example.com" required/></div><div><label for="intelligence-key" class="input-label">{{ t('intelligenceMonitor.form.key') }}</label><input id="intelligence-key" v-model="form.api_key" class="input" type="password" autocomplete="new-password" :required="!plan || plan.source_type !== 'external'" :placeholder="plan?.source_type === 'external' ? t('intelligenceMonitor.form.keepKey') : 'sk-…'"/></div></div>
        <div class="grid gap-3 sm:grid-cols-3"><div><label for="intelligence-supplier-note" class="input-label">{{ t('intelligenceMonitor.form.supplierNote') }}</label><input id="intelligence-supplier-note" v-model="form.supplier_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.supplierPlaceholder')"/></div><div><label for="intelligence-group-note" class="input-label">{{ t('intelligenceMonitor.form.groupNote') }}</label><input id="intelligence-group-note" v-model="form.group_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.groupPlaceholder')"/></div><div><label for="intelligence-rate-note" class="input-label">{{ t('intelligenceMonitor.form.rateNote') }}</label><input id="intelligence-rate-note" v-model="form.rate_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.ratePlaceholder')"/></div></div>
      </template>
      <div class="grid gap-4 sm:grid-cols-2"><div v-if="!oauthOnly"><label for="intelligence-api-mode" class="input-label">{{ t('intelligenceMonitor.form.protocol') }}</label><select id="intelligence-api-mode" v-model="form.api_mode" class="input"><option value="responses">Responses</option><option value="chat_completions">Chat Completions</option></select></div><div><label for="intelligence-timeout" class="input-label">{{ t('intelligenceMonitor.form.timeout') }}</label><select id="intelligence-timeout" v-model.number="form.timeout_seconds" class="input"><option v-for="seconds in [180,240,300]" :key="seconds" :value="seconds">{{ t('intelligenceMonitor.seconds',{count:seconds}) }}</option></select></div></div>
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700"><div class="flex items-center justify-between gap-4"><div><label for="intelligence-enabled" class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('intelligenceMonitor.form.enabled') }}</label><p class="mt-1 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.form.scheduleHint') }}</p></div><Toggle id="intelligence-enabled" v-model="form.enabled"/></div><div v-if="form.enabled" class="mt-4"><label for="intelligence-interval" class="input-label">{{ t('intelligenceMonitor.form.interval') }}</label><select id="intelligence-interval" v-model.number="form.interval_seconds" class="input"><option v-for="seconds in intervals" :key="seconds" :value="seconds">{{ seconds < 3600 ? t('intelligenceMonitor.minutes',{count:seconds/60}) : t('intelligenceMonitor.hours',{count:seconds/3600}) }}</option></select></div></div>
      <div><label for="intelligence-notes" class="input-label">{{ t('intelligenceMonitor.notes') }}</label><textarea id="intelligence-notes" v-model="form.notes" class="input min-h-[76px]" maxlength="2000" :placeholder="t('intelligenceMonitor.form.notesPlaceholder')"/></div>
      <details class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900/50"><summary class="cursor-pointer text-xs font-medium text-gray-500">{{ t('intelligenceMonitor.prompt') }}</summary><p class="mt-2 text-xs leading-6 text-gray-600 dark:text-dark-300">{{ PELICAN_PROMPT }}</p></details>
      <p v-if="error" role="alert" class="rounded-lg bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button><button type="submit" form="intelligence-plan-form" class="btn btn-primary" :disabled="saving">{{ t(saving ? 'intelligenceMonitor.form.saving' : 'intelligenceMonitor.form.save') }}</button></div></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, PELICAN_MODEL, PELICAN_REASONING, PELICAN_PROMPT, type IntelligencePlan, type IntelligencePlanInput, type IntelligenceSource } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import { getAll } from '@/api/admin/groups'
import type { AdminGroup, AccountListItem } from '@/types'
import { list as listAccounts } from '@/api/admin/accounts'
import { extractApiErrorMessage, extractApiErrorMetadata } from '@/utils/apiError'
import { intelligenceRateLabel } from './intelligencePreview'
import { domain } from './format'
const props = defineProps<{ show: boolean; plan: IntelligencePlan | null; overview: UpstreamOverview | null; oauthOnly?: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const sources: {value:IntelligenceSource;icon:'server'|'link'|'grid'}[] = [{ value:'upstream',icon:'server' },{ value:'local_group',icon:'grid' },{ value:'external',icon:'link' }]
const intervals = [300,900,1800,3600,7200,21600,43200,86400]
const defaults = (): IntelligencePlanInput => ({ name:'',source_type:props.oauthOnly?'openai_oauth':'upstream',account_id:null,endpoint:'',api_key:'',upstream_target_id:null,group_id:null,supplier_note:'',group_note:'',rate_note:'',notes:'',api_mode:'responses',enabled:false,interval_seconds:3600,timeout_seconds:300 })
const form = reactive<IntelligencePlanInput>(defaults()), groups = ref<AdminGroup[]>([]), saving = ref(false), error = ref(''), groupError = ref('')
const eligibleSuppliers = computed(() => (props.overview?.suppliers || []).map(supplier => ({ ...supplier, targets: supplier.targets.filter(target => target.provider === 'openai') })).filter(supplier => supplier.targets.length))
const selectedTarget = computed(() => eligibleSuppliers.value.flatMap(supplier => supplier.targets).find(target => target.id === form.upstream_target_id))
function upstreamRate(id:number) { const billing = props.overview?.suppliers.flatMap(supplier => supplier.targets).find(target=>target.id===id)?.balance?.billing; return intelligenceRateLabel(billing as unknown as Record<string,unknown>) || t('intelligenceMonitor.rateUnknown') }
const oauthAccounts = ref<AccountListItem[]>([]), accountSearch = ref(''), accountPage = ref(1), accountTotal = ref(0), accountsLoading = ref(false), accountError = ref('')
let accountController: AbortController | undefined, accountSearchTimer: ReturnType<typeof setTimeout> | undefined
async function loadAccounts(page = 1) {
  accountController?.abort()
  const current = new AbortController(); accountController = current; accountsLoading.value = true; accountError.value = ''
  try {
    const result = await listAccounts(page, 30, { platform: 'openai', type: 'oauth', lite: '1', search: accountSearch.value.trim() }, { signal: current.signal })
    if (!current.signal.aborted) { oauthAccounts.value = result.items.filter(account => account.platform === 'openai' && account.type === 'oauth'); accountTotal.value = result.total; accountPage.value = page }
  } catch { if (!current.signal.aborted) accountError.value = t('intelligenceMonitor.oauth.loadFailed') }
  finally { if (!current.signal.aborted) accountsLoading.value = false }
}
function scheduleAccountSearch() { clearTimeout(accountSearchTimer); accountSearchTimer = setTimeout(() => void loadAccounts(1), 250) }
let generation = 0
watch(() => props.show, async show => {
  const current = ++generation; if (!show) { accountController?.abort(); clearTimeout(accountSearchTimer); return }
  Object.assign(form, defaults(), props.plan ? { ...props.plan, api_key:'' } : {})
  error.value = ''; groupError.value = ''; accountError.value = ''
  if (props.oauthOnly) { accountSearch.value = ''; await loadAccounts(1); return }
  try { const result = await getAll(); if (current === generation) groups.value = result.filter(group => ['openai','composite'].includes(group.platform)) }
  catch { if (current === generation) groupError.value = t('intelligenceMonitor.form.loadGroupsFailed') }
}, { immediate: true })
async function save() {
  if (saving.value) return
  error.value = ''
  if (!props.oauthOnly && !form.name.trim()) { error.value=t('intelligenceMonitor.form.requiredName'); return }
  if ((props.oauthOnly && !form.account_id) || (form.source_type==='upstream' && !form.upstream_target_id) || (form.source_type==='local_group' && !form.group_id)) { error.value=t('intelligenceMonitor.form.requiredSource'); return }
  if (form.source_type==='external') {
    try { const parsed = new URL(form.endpoint || ''); if (parsed.protocol !== 'https:' || parsed.username || parsed.password) throw new Error() } catch { error.value=t('intelligenceMonitor.form.validEndpoint'); return }
    if (!form.api_key?.trim() && (!props.plan || props.plan.source_type !== 'external')) { error.value=t('intelligenceMonitor.form.requiredKey'); return }
  }
  saving.value=true
  const input: IntelligencePlanInput = { name:props.oauthOnly?'':form.name.trim(),source_type:props.oauthOnly?'openai_oauth':form.source_type,account_id:props.oauthOnly?form.account_id:null,endpoint:form.source_type==='external' ? form.endpoint?.trim() : undefined,api_key:form.source_type==='external' ? form.api_key?.trim() || undefined : undefined,upstream_target_id:form.source_type==='upstream' ? form.upstream_target_id : null,group_id:form.source_type==='local_group' ? form.group_id : null,supplier_note:form.supplier_note.trim(),group_note:form.group_note.trim(),rate_note:form.rate_note.trim(),notes:form.notes.trim(),api_mode:props.oauthOnly?'responses':form.api_mode,enabled:form.enabled,interval_seconds:form.interval_seconds,timeout_seconds:form.timeout_seconds }
  try { if (props.plan) await intelligenceMonitorAPI.update(props.plan.id,input); else await intelligenceMonitorAPI.create(input); emit('saved'); emit('close') }
  catch (err) {
    const detail = extractApiErrorMetadata(err)?.detail
    const detailText = typeof detail === 'string' ? detail.trim() : ''
    error.value = detailText === 'the selected account must support the fixed gpt-6-astra model without remapping'
      ? t('intelligenceMonitor.oauth.fixedModelRequired')
      : detailText || extractApiErrorMessage(err,t('intelligenceMonitor.saveFailed'))
  }
  finally { saving.value=false }
}
onBeforeUnmount(()=>{generation++;accountController?.abort();clearTimeout(accountSearchTimer)})
</script>
