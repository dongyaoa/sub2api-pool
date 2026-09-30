<template>
  <BaseDialog :show="show" :title="t(plan ? 'intelligenceMonitor.edit' : oauthOnly ? 'intelligenceMonitor.oauth.add' : localOnly ? 'intelligenceMonitor.local.add' : 'intelligenceMonitor.add')" width="wide" :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <form id="intelligence-plan-form" class="space-y-5" @submit.prevent="save">
      <div class="flex items-center gap-3 rounded-xl border border-gray-100 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/50"><Icon name="lightbulb" size="md" class="text-primary-500"/><div class="min-w-0 flex-1"><p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ PELICAN_MODEL }} <span class="ml-2 rounded border border-primary-200 px-1.5 py-0.5 text-[10px] font-medium text-primary-600 dark:border-primary-800">{{ PELICAN_REASONING }}</span></p><p class="mt-1 text-xs text-gray-500">{{ t('intelligenceMonitor.form.fixedPrompt') }}</p></div></div>
      <div v-if="!oauthOnly"><label for="intelligence-name" class="input-label">{{ t('intelligenceMonitor.form.name') }}</label><input id="intelligence-name" v-model="form.name" class="input" required maxlength="100" :placeholder="t('intelligenceMonitor.form.namePlaceholder')"/></div>
      <div v-if="!oauthOnly && !lockedUpstream && !localOnly"><label class="input-label">{{ t('intelligenceMonitor.form.source') }}</label><div class="grid gap-2" :class="sources.length > 2 ? 'grid-cols-3' : 'grid-cols-2'"><button v-for="source in sources" :key="source.value" type="button" class="flex items-center justify-center gap-2 rounded-xl border px-2 py-3 text-xs font-medium transition-colors" :class="form.source_type === source.value ? 'border-primary-400 bg-primary-50 text-primary-700 dark:border-primary-600 dark:bg-primary-500/10 dark:text-primary-300' : 'border-gray-200 text-gray-500 hover:border-gray-300 dark:border-dark-600 dark:text-dark-300'" :aria-pressed="form.source_type === source.value" @click="form.source_type = source.value"><Icon :name="source.icon" size="sm"/>{{ t(`intelligenceMonitor.source.${source.value}`) }}</button></div></div>
      <div v-if="oauthOnly" class="space-y-3">
        <div class="flex items-center gap-2"><span class="rounded-md bg-violet-50 px-2 py-1 text-xs font-semibold text-violet-600 dark:bg-violet-500/10 dark:text-violet-300">OpenAI OAuth</span><span class="text-xs text-gray-500">{{ t('intelligenceMonitor.oauth.nameHint') }}</span></div>
        <label class="input-label" for="intelligence-oauth-account">{{ t('intelligenceMonitor.oauth.select') }}</label>
        <div class="flex gap-2">
          <Select id="intelligence-oauth-account" :model-value="form.account_id" class="oauth-account-select min-w-0 flex-1" :class="selectedAccount && 'oauth-account-selected'" :options="accountOptions" :searchable="false" :loading="accountsLoading" :disabled="accountsLoading || saving" :error="Boolean(accountError || accountSelectionError)" :placeholder="t(accountsLoading ? 'common.loading' : 'intelligenceMonitor.oauth.select')" :empty-text="t(accountError ? 'intelligenceMonitor.oauth.loadFailed' : 'intelligenceMonitor.oauth.noAccounts')" :aria-label="t('intelligenceMonitor.oauth.select')" aria-describedby="intelligence-oauth-hint" @update:model-value="selectAccount">
            <template #selected="{ option }"><span class="flex min-w-0 items-center gap-2"><span class="truncate">{{ option?.label || t(accountsLoading ? 'common.loading' : 'intelligenceMonitor.oauth.select') }}</span><span v-if="option" class="shrink-0 text-[10px] font-medium text-emerald-600 dark:text-emerald-300">OAuth</span></span></template>
            <template #option="{ option, selected }"><span class="intelligence-oauth-option -mx-4 -my-2.5 flex min-w-0 flex-1 items-center justify-between gap-3 px-4 py-2.5" :class="selected && 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'"><span class="truncate">{{ option.label }}</span><Icon v-if="selected" name="check" size="sm" class="shrink-0 text-emerald-600 dark:text-emerald-300"/><span v-else class="shrink-0 text-[10px] text-violet-500">OAuth</span></span></template>
          </Select>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="accountsLoading || saving" :aria-label="t('intelligenceMonitor.refresh')" @click="loadAccounts"><Icon name="refresh" size="sm" :class="accountsLoading && 'animate-spin'"/></button>
        </div>
        <p v-if="accountsLoading" role="status" class="text-xs text-gray-500">{{ t('intelligenceMonitor.oauth.loading') }}</p>
        <p v-else-if="accountsReady && !availableAccounts.length" class="text-xs text-gray-500">{{ t('intelligenceMonitor.oauth.noAccounts') }}</p>
        <p id="intelligence-oauth-hint" class="text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.oauth.hint') }}</p><p v-if="accountError || accountSelectionError" role="alert" class="text-xs text-rose-500">{{ accountError || accountSelectionError }}</p>
      </div>
      <div v-else-if="lockedUpstream" data-testid="intelligence-locked-upstream" class="rounded-xl border border-primary-100 bg-primary-50/50 p-4 dark:border-primary-900/50 dark:bg-primary-500/5">
        <div class="mb-3 flex items-center gap-2 text-xs font-medium text-primary-600 dark:text-primary-300"><Icon name="server" size="sm"/>{{ t('intelligenceMonitor.source.upstream') }}</div>
        <div v-if="selectedTarget" class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0 flex-1"><p class="truncate text-xs text-gray-500 dark:text-dark-300">{{ selectedSupplier?.name }}</p><p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-gray-100">{{ selectedTarget.name }}</p><p class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">{{ domain(selectedTarget.endpoint) }}</p></div>
          <div class="shrink-0 text-right"><p class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.rate') }}</p><p class="mt-1 text-sm font-semibold tabular-nums" :class="selectedTarget.balance?.billing?.stale ? 'text-amber-600 dark:text-amber-400' : 'text-primary-600 dark:text-primary-300'">{{ upstreamRate(selectedTarget.id) }}</p><p class="mt-1 text-[10px] text-gray-500 dark:text-dark-400">{{ t('intelligenceMonitor.autoRate') }}</p></div>
        </div>
        <p v-else role="status" class="text-sm text-amber-600 dark:text-amber-400">{{ t('intelligenceMonitor.sourceMissing') }}</p>
      </div>
      <div v-else-if="form.source_type === 'upstream'">
        <label for="intelligence-upstream" class="input-label">{{ t('intelligenceMonitor.form.selectUpstream') }}</label>
        <Select id="intelligence-upstream" :model-value="form.upstream_target_id" :options="upstreamOptions" :searchable="false" :placeholder="t('intelligenceMonitor.form.selectUpstream')" :aria-label="t('intelligenceMonitor.form.selectUpstream')" @update:model-value="selectUpstream">
          <template #selected="{ option }">
            <span v-if="option" class="flex min-w-0 items-center gap-2"><span class="max-w-[35%] truncate text-xs text-gray-500 dark:text-dark-300">{{ option.supplierName }}</span><span class="text-gray-300 dark:text-dark-500">/</span><span class="min-w-0 truncate font-medium">{{ option.groupName }}</span><span class="ml-auto shrink-0 rounded-md bg-primary-50 px-2 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">{{ option.rate }}</span></span>
            <span v-else>{{ t('intelligenceMonitor.form.selectUpstream') }}</span>
          </template>
          <template #option="{ option, selected }">
            <span v-if="option.kind === 'group'" class="intelligence-supplier-heading -mx-4 -my-2.5 flex min-w-0 flex-1 items-center gap-2.5 border-y border-primary-100/70 bg-primary-50/70 px-4 py-3 normal-case tracking-normal dark:border-primary-900/50 dark:bg-primary-950/40">
              <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-white text-primary-600 shadow-sm dark:bg-primary-900/60 dark:text-primary-300"><Icon name="server" size="sm"/></span><span class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{{ option.label }}</span><span v-if="option.website" class="ml-auto max-w-[40%] truncate text-[11px] font-normal text-gray-500 dark:text-dark-400">{{ option.website }}</span>
            </span>
            <span v-else class="intelligence-upstream-option -mx-4 -my-2.5 flex min-w-0 flex-1 items-center gap-3 py-3 pl-7 pr-4" :class="selected && 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'">
              <span aria-hidden="true" class="h-3.5 w-3 shrink-0 rounded-bl border-b border-l border-gray-300 dark:border-dark-500"/><span class="min-w-0 flex-1 truncate font-medium">{{ option.groupName }}</span><span class="shrink-0 rounded-md bg-gray-100 px-2 py-0.5 text-xs font-semibold tabular-nums text-primary-700 dark:bg-dark-700 dark:text-primary-300">{{ option.rate }}</span><Icon v-if="selected" name="check" size="sm" class="shrink-0 text-emerald-600 dark:text-emerald-300"/>
            </span>
          </template>
        </Select>
        <div v-if="selectedTarget" class="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500"><span class="truncate">{{ domain(selectedTarget.endpoint) }}</span><span :class="selectedTarget.balance?.billing?.stale ? 'text-amber-600' : 'text-primary-600'">{{ t('intelligenceMonitor.rate') }} {{ upstreamRate(selectedTarget.id) }} · {{ t('intelligenceMonitor.autoRate') }}</span></div>
      </div>
      <div v-else-if="form.source_type === 'local_group'"><IntelligenceLocalSource v-if="show" ref="localSource" :groups="groups" :groups-loading="groupsLoading" :group-id="form.group_id" :local-key-id="form.local_api_key_id" :saved-key-name="plan?.local_api_key_name" :saved-key-mask="plan?.api_key_masked" :managed-key-ids="managedKeyIds" @select="selectLocalSource"/><p v-if="groupError" role="alert" class="mt-1 text-xs text-rose-500">{{ groupError }}</p></div>
      <template v-else>
        <div class="grid gap-4 sm:grid-cols-2"><div><label for="intelligence-endpoint" class="input-label">{{ t('intelligenceMonitor.form.endpoint') }}</label><input id="intelligence-endpoint" v-model="form.endpoint" class="input" type="url" placeholder="https://api.example.com" required/></div><div><label for="intelligence-key" class="input-label">{{ t('intelligenceMonitor.form.key') }}</label><input id="intelligence-key" v-model="form.api_key" class="input" type="password" autocomplete="new-password" :required="!plan || plan.source_type !== 'external'" :placeholder="plan?.source_type === 'external' ? t('intelligenceMonitor.form.keepKey') : 'sk-…'"/></div></div>
        <div class="grid gap-3 sm:grid-cols-3"><div><label for="intelligence-supplier-note" class="input-label">{{ t('intelligenceMonitor.form.supplierNote') }}</label><input id="intelligence-supplier-note" v-model="form.supplier_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.supplierPlaceholder')"/></div><div><label for="intelligence-group-note" class="input-label">{{ t('intelligenceMonitor.form.groupNote') }}</label><input id="intelligence-group-note" v-model="form.group_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.groupPlaceholder')"/></div><div><label for="intelligence-rate-note" class="input-label">{{ t('intelligenceMonitor.form.rateNote') }}</label><input id="intelligence-rate-note" v-model="form.rate_note" class="input" maxlength="200" :placeholder="t('intelligenceMonitor.form.ratePlaceholder')"/></div></div>
      </template>
      <div class="grid gap-4" :class="!oauthOnly && 'sm:grid-cols-2'">
        <div v-if="!oauthOnly"><label for="intelligence-api-mode" class="input-label">{{ t('intelligenceMonitor.form.protocol') }}</label><Select id="intelligence-api-mode" v-model="form.api_mode" :options="protocolOptions" :searchable="false" :aria-label="t('intelligenceMonitor.form.protocol')"/></div>
        <fieldset><legend class="input-label">{{ t('intelligenceMonitor.form.timeout') }}</legend><div class="grid gap-2" :class="timeoutOptions.length > 3 ? 'grid-cols-2' : 'grid-cols-3'"><button v-for="seconds in timeoutOptions" :key="seconds" type="button" class="duration-choice" :class="form.timeout_seconds === seconds && 'duration-choice-selected'" :data-timeout="seconds" :aria-pressed="form.timeout_seconds === seconds" @click="form.timeout_seconds = seconds">{{ durationLabel(seconds) }}</button></div><p class="mt-2 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.form.timeoutHint') }}</p></fieldset>
      </div>
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-center justify-between gap-4"><div><label for="intelligence-enabled" class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('intelligenceMonitor.form.enabled') }}</label><p class="mt-1 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.form.scheduleHint') }}</p></div><Toggle id="intelligence-enabled" v-model="form.enabled"/></div>
        <fieldset v-if="form.enabled" class="mt-4"><legend class="input-label">{{ t('intelligenceMonitor.form.interval') }}</legend><div class="grid grid-cols-2 gap-2 sm:grid-cols-4"><button v-for="seconds in intervals" :key="seconds" type="button" class="duration-choice" :class="intervalChoice === seconds && 'duration-choice-selected'" :data-interval="seconds" :aria-pressed="intervalChoice === seconds" @click="chooseInterval(seconds)">{{ durationLabel(seconds) }}</button><button type="button" class="duration-choice" :class="intervalChoice === 'custom' && 'duration-choice-selected'" data-interval="custom" :aria-pressed="intervalChoice === 'custom'" @click="chooseInterval('custom')">{{ t('intelligenceMonitor.form.customInterval') }}</button></div>
          <div v-if="intervalChoice === 'custom'" class="mt-3"><label for="intelligence-interval-custom" class="input-label">{{ t('intelligenceMonitor.form.customSeconds') }}</label><input id="intelligence-interval-custom" v-model="customInterval" type="text" inputmode="numeric" class="input" :aria-invalid="Boolean(intervalError)" aria-describedby="intelligence-interval-hint intelligence-interval-error" :placeholder="t('intelligenceMonitor.form.intervalPlaceholder')" @input="intervalError = ''"/></div>
          <p id="intelligence-interval-hint" class="mt-2 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.form.intervalHint') }}</p>
        </fieldset><p v-if="intervalError" id="intelligence-interval-error" role="alert" class="mt-2 text-xs text-rose-500">{{ intervalError }}</p>
      </div>
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700" data-testid="candy-option"><div class="flex items-center justify-between gap-4"><div><label for="intelligence-candy-enabled" class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('intelligenceMonitor.candy.enabled') }}</label><p class="mt-1 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.candy.enableHint') }}</p></div><Toggle id="intelligence-candy-enabled" v-model="form.candy_enabled"/></div><fieldset v-if="form.candy_enabled" class="mt-4"><legend class="input-label">{{ t('intelligenceMonitor.candy.interval') }}</legend><div class="grid grid-cols-4 gap-2"><button v-for="seconds in candyIntervals" :key="seconds" type="button" class="duration-choice" :class="form.candy_interval_seconds === seconds && 'duration-choice-selected'" :data-candy-interval="seconds" :aria-pressed="form.candy_interval_seconds === seconds" @click="form.candy_interval_seconds = seconds">{{ durationLabel(seconds) }}</button></div><p class="mt-2 text-xs text-gray-500">{{ t('intelligenceMonitor.candy.intervalHint') }}</p></fieldset></div>
      <div><label for="intelligence-notes" class="input-label">{{ t('intelligenceMonitor.notes') }}</label><textarea id="intelligence-notes" v-model="form.notes" class="input min-h-[76px]" maxlength="2000" :placeholder="t('intelligenceMonitor.form.notesPlaceholder')"/></div>
      <details class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900/50"><summary class="cursor-pointer text-xs font-medium text-gray-500">{{ t('intelligenceMonitor.prompt') }}</summary><p class="mt-2 text-xs leading-6 text-gray-600 dark:text-dark-300">{{ PELICAN_PROMPT }}</p></details>
      <p v-if="error" role="alert" class="rounded-lg bg-rose-50 p-3 text-sm text-rose-600 dark:bg-rose-500/10 dark:text-rose-400">{{ error }}</p>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button><button type="submit" form="intelligence-plan-form" class="btn btn-primary" :disabled="saving || (oauthOnly && (accountsLoading || !accountsReady))">{{ t(saving ? 'intelligenceMonitor.form.saving' : 'intelligenceMonitor.form.save') }}</button></div></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { intelligenceMonitorAPI, PELICAN_MODEL, PELICAN_REASONING, PELICAN_PROMPT, type IntelligencePlan, type IntelligencePlanInput, type IntelligenceSource } from '@/api/admin/intelligenceMonitor'
import type { UpstreamOverview } from '@/api/admin/upstreamCenter'
import { getAll } from '@/api/admin/groups'
import type { AdminGroup, AccountListItem } from '@/types'
import { list as listAccounts } from '@/api/admin/accounts'
import { extractApiErrorCode, extractApiErrorMessage, extractApiErrorMetadata } from '@/utils/apiError'
import { intelligenceRateLabel } from './intelligencePreview'
import { domain } from './format'
import IntelligenceLocalSource from './IntelligenceLocalSource.vue'
const props = defineProps<{ show: boolean; plan: IntelligencePlan | null; overview: UpstreamOverview | null; oauthOnly?: boolean; localOnly?: boolean; managedKeyIds?: number[]; monitoredAccountIds?: number[]; upstreamTargetId?: number }>()
const emit = defineEmits<{ close: []; saved: [plan?: IntelligencePlan] }>()
const { t } = useI18n()
const lockedUpstream = computed(() => props.upstreamTargetId !== undefined)
const oauthOnly = computed(() => Boolean(props.oauthOnly && !lockedUpstream.value))
const localOnly = computed(() => Boolean(props.localOnly && !lockedUpstream.value && !oauthOnly.value))
const localSource = ref<InstanceType<typeof IntelligenceLocalSource> | null>(null)
const candyIntervals = [180, 300, 600, 900]
const sources = computed<{value:IntelligenceSource;icon:'server'|'link'|'grid'}[]>(() => [{ value:'upstream',icon:'server' }, ...(props.plan?.source_type === 'local_group' ? [{ value:'local_group' as const,icon:'grid' as const }] : []),{ value:'external',icon:'link' }])
const intervals = [300, 600, 900]
const timeoutOptions = computed(() => {
  const presets = [300, 600, 900]
  const saved = props.plan?.timeout_seconds
  return saved !== undefined && Number.isInteger(saved) && saved >= 180 && saved <= 900 && !presets.includes(saved)
    ? [...presets, saved].sort((a, b) => a - b)
    : presets
})
const protocolOptions = [{ value: 'responses', label: 'Responses' }, { value: 'chat_completions', label: 'Chat Completions' }]
const defaults = (): IntelligencePlanInput & { candy_enabled: boolean; candy_interval_seconds: number } => ({ name:'',source_type:oauthOnly.value?'openai_oauth':localOnly.value?'local_group':'upstream',account_id:null,endpoint:'',api_key:'',upstream_target_id:null,group_id:null,local_api_key_id:null,supplier_note:'',group_note:'',rate_note:'',notes:'',api_mode:'responses',enabled:true,candy_enabled:false,candy_interval_seconds:180,interval_seconds:300,timeout_seconds:600 })
const form = reactive(defaults()), groups = ref<AdminGroup[]>([]), saving = ref(false), error = ref(''), groupError = ref(''), groupsLoading = ref(false)
const intervalChoice = ref<number | 'custom'>(300), customInterval = ref('300'), intervalError = ref('')
const eligibleSuppliers = computed(() => (props.overview?.suppliers || []).map(supplier => ({ ...supplier, targets: supplier.targets.filter(target => target.provider === 'openai') })).filter(supplier => supplier.targets.length))
const selectedTarget = computed(() => eligibleSuppliers.value.flatMap(supplier => supplier.targets).find(target => target.id === form.upstream_target_id))
const selectedSupplier = computed(() => eligibleSuppliers.value.find(supplier => supplier.targets.some(target => target.id === form.upstream_target_id)))
function upstreamRate(id:number) { const billing = props.overview?.suppliers.flatMap(supplier => supplier.targets).find(target=>target.id===id)?.balance?.billing; return intelligenceRateLabel(billing as unknown as Record<string,unknown>) || t('intelligenceMonitor.rateUnknown') }
const upstreamOptions = computed<SelectOption[]>(() => eligibleSuppliers.value.flatMap(supplier => [
  { value: `supplier-${supplier.id}`, label: supplier.name, kind: 'group', disabled: true, website: supplier.website ? domain(supplier.website) : '' },
  ...supplier.targets.map(target => ({ value: target.id, label: `${target.name} · ${upstreamRate(target.id)}`, groupName: target.name, supplierName: supplier.name, rate: upstreamRate(target.id) }))
]))
function selectUpstream(value: string | number | boolean | null) {
  const target = eligibleSuppliers.value.flatMap(supplier => supplier.targets).find(item => item.id === value)
  if (!target || target.id === form.upstream_target_id) return
  form.upstream_target_id = target.id
  form.name = target.name
  error.value = ''
}
function selectLocalSource(value: { groupId: number | null; keyId: number | null; name?: string }) {
  const changedGroup = form.group_id !== value.groupId
  form.group_id = value.groupId
  form.local_api_key_id = value.keyId
  if (value.name && (changedGroup || !form.name.trim())) form.name = value.name
  error.value = ''
}
function durationLabel(seconds: number) {
  return seconds % 3600 === 0 ? t('intelligenceMonitor.hours', { count: seconds / 3600 })
    : seconds % 60 === 0 ? t('intelligenceMonitor.minutes', { count: seconds / 60 })
      : t('intelligenceMonitor.seconds', { count: seconds })
}
function chooseInterval(value: number | 'custom') {
  intervalChoice.value = value
  intervalError.value = ''
  if (typeof value === 'number') form.interval_seconds = value
}
watch(() => form.enabled, () => { intervalError.value = '' })
const oauthAccounts = ref<AccountListItem[]>([]), accountsLoading = ref(false), accountsReady = ref(false), accountError = ref(''), accountSelectionError = ref('')
const monitoredAccounts = computed(() => new Set(props.monitoredAccountIds || []))
const availableAccounts = computed(() => oauthAccounts.value.filter(account => account.id === props.plan?.account_id || !monitoredAccounts.value.has(account.id)))
const accountOptions = computed(() => availableAccounts.value.map(account => ({ value: account.id, label: account.name })))
const selectedAccount = computed(() => availableAccounts.value.find(account => account.id === form.account_id))
watch(monitoredAccounts, occupied => {
  if (!props.show || !oauthOnly.value || !form.account_id || form.account_id === props.plan?.account_id || !occupied.has(form.account_id)) return
  form.account_id = null
  form.name = ''
  accountSelectionError.value = t('intelligenceMonitor.oauth.alreadyAdded')
})
let accountController: AbortController | undefined
function eligibleAccount(account: AccountListItem): boolean {
  const now = Date.now()
  return account.platform === 'openai' && account.type === 'oauth' && account.status === 'active' && account.schedulable === true
    && account.parent_account_id == null && account.extra?.synthetic_ui_test !== true
    && ![account.rate_limit_reset_at, account.temp_unschedulable_until, account.overload_until].some(value => value && new Date(value).getTime() > now)
    && !(account.auto_pause_on_expired && account.expires_at != null && account.expires_at * 1000 <= now)
}
function selectAccount(value: string | number | boolean | null) {
  const account = availableAccounts.value.find(item => item.id === value && eligibleAccount(item))
  form.account_id = account?.id ?? null
  form.name = account?.name ?? ''
  accountSelectionError.value = account ? '' : t('intelligenceMonitor.oauth.unavailable')
  error.value = ''
}
async function loadAccounts() {
  accountController?.abort()
  const current = new AbortController(); accountController = current; accountsLoading.value = true; accountsReady.value = false; accountError.value = ''
  try {
    const accounts = new Map<number, AccountListItem>()
    let page = 1
    while (!current.signal.aborted) {
      const result = await listAccounts(page, 100, { platform: 'openai', type: 'oauth', status: 'active', lite: '1' }, { signal: current.signal })
      if (current.signal.aborted) return
      for (const account of result.items) accounts.set(account.id, account)
      const pages = result.pages || Math.ceil(result.total / (result.page_size || 100))
      if (page >= pages) break
      if (!result.items.length) throw new Error('Incomplete account list')
      page++
    }
    if (current.signal.aborted) return
    oauthAccounts.value = [...accounts.values()].filter(eligibleAccount)
    accountsReady.value = true
    if (form.account_id) {
      const account = availableAccounts.value.find(item => item.id === form.account_id)
      if (account) { form.name = account.name; accountSelectionError.value = '' }
      else { form.account_id = null; form.name = ''; accountSelectionError.value = t('intelligenceMonitor.oauth.unavailable') }
    }
  } catch { if (!current.signal.aborted) { oauthAccounts.value = []; accountError.value = t('intelligenceMonitor.oauth.loadFailed') } }
  finally { if (!current.signal.aborted) accountsLoading.value = false }
}
let generation = 0
function cancelLoading() { generation++; accountController?.abort(); accountsLoading.value = false; groupsLoading.value = false }
function close() { if (!saving.value) { cancelLoading(); emit('close') } }
watch([() => props.show, () => props.upstreamTargetId, () => props.plan?.id, () => props.localOnly, () => props.oauthOnly], async ([show]) => {
  cancelLoading()
  if (!show) return
  const current = generation
  Object.assign(form, defaults(), props.plan ? { ...props.plan, api_key:'' } : {})
  form.candy_enabled = Boolean(props.plan?.candy_enabled)
  form.candy_interval_seconds = candyIntervals.includes(props.plan?.candy_interval_seconds ?? 180) ? props.plan?.candy_interval_seconds ?? 180 : 180
  // Existing managed keys remain automatic; explicit administrator keys retain their binding.
  form.local_api_key_id = props.plan?.local_api_key_managed ? null : props.plan?.local_api_key_id ?? null
  if (localOnly.value) { form.source_type = 'local_group'; form.account_id = null; form.upstream_target_id = null }
  if (lockedUpstream.value) {
    form.source_type = 'upstream'
    form.upstream_target_id = props.upstreamTargetId
    form.account_id = null
    form.group_id = null
    if (!props.plan) form.name = selectedTarget.value?.name || ''
  }
  intervalChoice.value = intervals.includes(form.interval_seconds) ? form.interval_seconds : 'custom'
  customInterval.value = String(form.interval_seconds)
  error.value = ''; groupError.value = ''; accountError.value = ''; accountSelectionError.value = ''; intervalError.value = ''
  oauthAccounts.value = []; accountsReady.value = false; groups.value = []
  if (lockedUpstream.value) return
  if (oauthOnly.value) { await loadAccounts(); return }
  groupsLoading.value = true
  try { const result = await getAll(); if (current === generation) groups.value = result.filter(group => ['openai','composite'].includes(group.platform)) }
  catch { if (current === generation) groupError.value = t('intelligenceMonitor.form.loadGroupsFailed') }
  finally { if (current === generation) groupsLoading.value = false }
}, { immediate: true })
async function save() {
  if (saving.value) return
  error.value = ''
  intervalError.value = ''
  if (lockedUpstream.value) {
    form.source_type = 'upstream'
    form.upstream_target_id = props.upstreamTargetId
    if (!selectedTarget.value) { error.value = t('intelligenceMonitor.sourceMissing'); return }
  }
  if (oauthOnly.value && (accountsLoading.value || !accountsReady.value)) return
  if (localOnly.value) form.source_type = 'local_group'
  if (form.source_type === 'local_group' && !localSource.value?.validate()) return
  if (!oauthOnly.value && !form.name.trim()) { error.value=t('intelligenceMonitor.form.requiredName'); return }
  if ((oauthOnly.value && !form.account_id) || (form.source_type==='upstream' && !form.upstream_target_id) || (form.source_type==='local_group' && !form.group_id)) { error.value=t('intelligenceMonitor.form.requiredSource'); return }
  if (oauthOnly.value && (!selectedAccount.value || !eligibleAccount(selectedAccount.value))) { form.account_id = null; form.name = ''; accountSelectionError.value = t('intelligenceMonitor.oauth.unavailable'); return }
  if (intervalChoice.value === 'custom') {
    const text = customInterval.value.trim()
    const seconds = Number(text)
    const valid = /^\d+$/.test(text) && Number.isSafeInteger(seconds) && seconds >= 30 && seconds <= 86400
    if (form.enabled && !valid) { intervalError.value = t('intelligenceMonitor.form.validInterval'); return }
    if (valid) form.interval_seconds = seconds
  }
  if (!Number.isInteger(form.interval_seconds) || form.interval_seconds < 30 || form.interval_seconds > 86400) { intervalError.value = t('intelligenceMonitor.form.validInterval'); return }
  if (form.source_type==='external') {
    try { const parsed = new URL(form.endpoint || ''); if (parsed.protocol !== 'https:' || parsed.username || parsed.password) throw new Error() } catch { error.value=t('intelligenceMonitor.form.validEndpoint'); return }
    if (!form.api_key?.trim() && (!props.plan || props.plan.source_type !== 'external')) { error.value=t('intelligenceMonitor.form.requiredKey'); return }
  }
  saving.value=true
  const input: IntelligencePlanInput = { name:oauthOnly.value?'':form.name.trim(),source_type:oauthOnly.value?'openai_oauth':form.source_type,account_id:oauthOnly.value?form.account_id:null,endpoint:form.source_type==='external' ? form.endpoint?.trim() : undefined,api_key:form.source_type==='external' ? form.api_key?.trim() || undefined : undefined,upstream_target_id:form.source_type==='upstream' ? form.upstream_target_id : null,group_id:form.source_type==='local_group' ? form.group_id : null,local_api_key_id:form.source_type==='local_group' ? form.local_api_key_id ?? null : undefined,supplier_note:form.supplier_note.trim(),group_note:form.group_note.trim(),rate_note:form.rate_note.trim(),notes:form.notes.trim(),api_mode:oauthOnly.value?'responses':form.api_mode,enabled:form.enabled,candy_enabled:Boolean(form.candy_enabled),candy_interval_seconds:form.candy_interval_seconds,interval_seconds:form.interval_seconds,timeout_seconds:form.timeout_seconds }
  try { const saved = props.plan ? await intelligenceMonitorAPI.update(props.plan.id,input) : await intelligenceMonitorAPI.create(input); emit('saved', saved); emit('close') }
  catch (err) {
    const detail = extractApiErrorMetadata(err)?.detail
    const detailText = typeof detail === 'string' ? detail.trim() : ''
    const code = extractApiErrorCode(err)
    error.value = code === 'INTELLIGENCE_OAUTH_PLAN_EXISTS'
      ? t('intelligenceMonitor.oauth.alreadyAdded')
      : code === 'INTELLIGENCE_OAUTH_COOLING_DOWN'
      ? t('intelligenceMonitor.oauth.cooldownHint')
      : code === 'INTELLIGENCE_OAUTH_UNAVAILABLE'
      ? t('intelligenceMonitor.oauth.unavailable')
      : code === 'INTELLIGENCE_UPSTREAM_PLAN_EXISTS'
      ? t('intelligenceMonitor.groupMonitor.alreadyExists')
      : detailText === 'the selected account must support the fixed gpt-6-astra model without remapping'
      ? t('intelligenceMonitor.oauth.fixedModelRequired')
      : detailText === 'the selected OAuth account is disabled, paused, expired, rate limited or cooling down'
        ? t('intelligenceMonitor.oauth.unavailable')
      : detailText || extractApiErrorMessage(err,t('intelligenceMonitor.saveFailed'))
    if (oauthOnly.value && (extractApiErrorMetadata(err)?.field === 'account_id' || code === 'INTELLIGENCE_OAUTH_UNAVAILABLE' || code === 'INTELLIGENCE_OAUTH_COOLING_DOWN')) void loadAccounts()
  }
  finally { saving.value=false }
}
onBeforeUnmount(cancelLoading)
</script>
<style scoped>
.duration-choice { @apply rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-xs font-medium text-gray-600 transition-colors hover:border-gray-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/30 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-300 dark:hover:border-dark-500; }
.duration-choice-selected { @apply border-emerald-300 bg-emerald-50 text-emerald-800 hover:border-emerald-400 dark:border-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300; }
.oauth-account-selected :deep(.select-trigger) { @apply border-emerald-300 bg-emerald-50 text-emerald-800 hover:border-emerald-400 focus:border-emerald-500 focus:ring-emerald-500/30 dark:border-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200; }
.oauth-account-selected :deep(.select-trigger-open) { @apply border-emerald-500 ring-emerald-500/30; }
</style>
