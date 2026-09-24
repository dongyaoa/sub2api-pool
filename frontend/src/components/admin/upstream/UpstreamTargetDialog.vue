<template>
  <BaseDialog :show="show" :title="title" width="wide" @close="!saving && emit('close')">
    <form id="upstream-target-form" class="space-y-5" @submit.prevent="save">
      <div v-if="supplier" class="flex items-center gap-2 rounded-xl bg-primary-50 px-4 py-3 text-sm text-primary-700 dark:bg-primary-500/10 dark:text-primary-300"><Icon name="server" size="sm" />{{ supplier.name }}</div>
      <div><label for="target-name" class="input-label">{{ t('upstreamCenter.form.name') }}</label><input id="target-name" v-model="form.name" required maxlength="100" class="input" :placeholder="t('upstreamCenter.form.targetNamePlaceholder')" /></div>
      <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t(supplier ? 'upstreamCenter.form.bindAccounts' : 'upstreamCenter.import.title') }}<span v-if="supplier && form.account_ids.length" class="ml-2 text-primary-600 dark:text-primary-400">{{ form.account_ids.length }}</span></h3>
        <p class="mb-3 mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t(supplier ? 'upstreamCenter.form.bindHint' : 'upstreamCenter.form.importCredentialHint') }}</p>
        <div class="flex gap-2"><Select v-model="accountPicker" class="min-w-0 flex-1" :options="accountOptions" :placeholder="t('upstreamCenter.form.selectAccount')" remote :loading="accountsLoading" @search="searchAccounts" /><button type="button" class="btn btn-secondary shrink-0" :disabled="!accountPicker || importing" @click="importAccount">{{ t('upstreamCenter.form.importAccount') }}</button></div>
        <p v-if="accountError" class="mt-2 text-xs text-red-500">{{ accountError }}</p>
        <div v-if="supplier" class="mt-3 flex flex-wrap gap-2"><span v-for="id in form.account_ids" :key="id" class="inline-flex items-center gap-2 rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">{{ accountNames[id] || t('upstreamCenter.form.accountId', { id }) }}<button type="button" :aria-label="`${t('upstreamCenter.remove')} ${accountNames[id] || id}`" @click="form.account_ids = form.account_ids.filter(value => value !== id)"><Icon name="x" size="xs" /></button></span></div>
        <div v-else-if="sourceAccount" class="mt-3"><span class="inline-flex items-center gap-2 rounded-lg bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">{{ sourceAccount.name }}<button type="button" :aria-label="`${t('upstreamCenter.remove')} ${sourceAccount.name}`" @click="clearSourceAccount"><Icon name="x" size="xs" /></button></span></div>
      </section>
      <div class="grid gap-4 sm:grid-cols-2">
        <div><label for="target-provider" class="input-label">{{ t('upstreamCenter.form.provider') }}</label><Select id="target-provider" v-model="form.provider" :options="providerOptions" :searchable="false" :aria-label="t('upstreamCenter.form.provider')" /></div>
        <div v-if="form.provider === 'openai'"><label for="target-api-mode" class="input-label">{{ t('upstreamCenter.form.apiMode') }}</label><Select id="target-api-mode" v-model="form.api_mode" :options="apiModeOptions" :searchable="false" :aria-label="t('upstreamCenter.form.apiMode')" /></div>
      </div>
      <div><label for="target-endpoint" class="input-label">{{ t('upstreamCenter.form.endpoint') }}</label><input id="target-endpoint" v-model="form.endpoint" type="url" required class="input" :placeholder="t('upstreamCenter.form.websitePlaceholder')" /><p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.form.endpointHint') }}</p></div>
      <div><label for="target-key" class="input-label">{{ t('upstreamCenter.form.apiKey') }}</label><input id="target-key" v-model="form.api_key" type="password" autocomplete="new-password" class="input" :required="!canKeepSavedKey && !sourceAccount && !(supplier && form.account_ids.length)" :placeholder="t(sourceAccount ? 'upstreamCenter.form.importedKeyPlaceholder' : canKeepSavedKey ? 'upstreamCenter.form.keepKey' : 'upstreamCenter.form.apiKeyPlaceholder')" /><p v-if="target?.api_key_masked && !sourceAccount && canKeepSavedKey" class="mt-1.5 font-mono text-xs text-gray-400">{{ t('upstreamCenter.form.existingKey', { key: target.api_key_masked }) }}</p><p v-if="sourceAccount || (supplier && form.account_ids.length)" class="mt-1.5 text-xs text-primary-600 dark:text-primary-400">{{ t(target && !sourceAccount ? 'upstreamCenter.form.accountEditHint' : 'upstreamCenter.form.useAccountKeyHint') }}</p><p v-else-if="target && !canKeepSavedKey" class="mt-1.5 text-xs text-amber-600 dark:text-amber-400">{{ t('upstreamCenter.form.changedConnectionKeyHint') }}</p></div>
      <div>
        <div class="mb-2 flex items-center justify-between gap-2"><label class="input-label mb-0">{{ t('upstreamCenter.form.models') }}</label><button type="button" class="text-xs font-medium text-primary-600 disabled:opacity-50 dark:text-primary-400" :disabled="modelsLoading || !form.endpoint || (!form.api_key && !canKeepSavedKey && !sourceAccount && !(supplier && form.account_ids.length))" @click="fetchModels">{{ t(modelsLoading ? 'upstreamCenter.form.fetching' : 'upstreamCenter.form.fetchModels') }}</button></div>
        <ModelTagInput :models="form.models" :platform="form.provider" :placeholder="t('upstreamCenter.form.modelsPlaceholder')" @update:models="form.models = $event" />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.form.modelsHint') }}</p>
        <p v-if="modelError" role="status" class="mt-2 text-xs text-amber-600 dark:text-amber-400">{{ modelError }}</p>
        <div v-if="discoveredModels.length" class="mt-3 max-h-40 overflow-y-auto rounded-xl border border-gray-100 p-3 dark:border-dark-700"><p class="mb-2 text-xs text-gray-500">{{ t('upstreamCenter.form.chooseModel') }}</p><div class="flex flex-wrap gap-1.5"><button v-for="model in discoveredModels" :key="model" type="button" :disabled="form.models.includes(model) || form.models.length >= 8" class="rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-700 hover:bg-primary-50 hover:text-primary-700 disabled:opacity-40 dark:bg-dark-700 dark:text-gray-200 dark:hover:bg-primary-500/10" @click="form.models.push(model)">{{ model }}</button></div></div>
      </div>
      <div><label for="target-interval" class="input-label">{{ t('upstreamCenter.form.interval') }}</label><input id="target-interval" v-model.number="form.interval_seconds" type="number" min="30" max="3600" step="1" required class="input" /><p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.form.intervalHint') }}</p></div>
      <div class="flex items-center justify-between"><label for="target-enabled" class="input-label mb-0">{{ t('upstreamCenter.form.enabled') }}</label><Toggle id="target-enabled" v-model="form.enabled" /></div>
      <details class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('upstreamCenter.form.advanced') }}</summary>
        <div class="mt-4 grid gap-4 sm:grid-cols-2"><div><label for="target-timeout" class="input-label">{{ t('upstreamCenter.form.timeout') }}</label><input id="target-timeout" v-model.number="form.timeout_seconds" required type="number" min="5" max="45" class="input" /></div><div><label for="target-threshold" class="input-label">{{ t('upstreamCenter.form.degraded') }}</label><input id="target-threshold" v-model.number="form.degraded_threshold_ms" required type="number" min="100" max="45000" class="input" /></div></div>
        <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.form.degradedHint') }}</p>
        <div v-if="supplier" class="mt-4"><label for="target-wallet" class="input-label">{{ t('upstreamCenter.wallet.ref') }}</label><input id="target-wallet" v-model="form.wallet_ref" required maxlength="100" class="input" /><p class="mt-1.5 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.wallet.refHint') }}</p></div>
        <div class="mt-4"><label for="target-notes" class="input-label">{{ t('upstreamCenter.form.notes') }}</label><textarea id="target-notes" v-model="form.notes" class="input min-h-[70px]" maxlength="2000" :placeholder="t('upstreamCenter.form.optional')"></textarea></div>
      </details>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-600 dark:bg-red-500/10 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button><button type="submit" form="upstream-target-form" class="btn btn-primary" :disabled="saving">{{ t(saving ? 'upstreamCenter.form.saving' : target || !form.enabled ? 'upstreamCenter.form.save' : 'upstreamCenter.form.createStart') }}</button></div></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelTagInput from '@/components/admin/channel/ModelTagInput.vue'
import { upstreamCenterAPI, type UpstreamSupplier, type UpstreamTarget, type UpstreamTargetInput } from '@/api/admin/upstreamCenter'
import * as accountsAPI from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ show: boolean; target: UpstreamTarget | null; supplier: UpstreamSupplier | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const providerOptions = computed(() => [
  { value: 'openai', label: t('upstreamCenter.form.providerOpenAI') },
  { value: 'anthropic', label: t('upstreamCenter.form.providerAnthropic') },
  { value: 'gemini', label: t('upstreamCenter.form.providerGemini') }
])
const apiModeOptions = [{ value: 'chat_completions', label: 'Chat Completions' }, { value: 'responses', label: 'Responses' }]
const title = computed(() => t(props.target ? props.supplier ? 'upstreamCenter.editGroup' : 'upstreamCenter.editMonitor' : props.supplier ? 'upstreamCenter.addGroup' : 'upstreamCenter.addMonitor'))
const defaults = (): UpstreamTargetInput => ({ supplier_id: props.supplier?.id || null, name: '', provider: 'openai', api_mode: 'chat_completions', endpoint: props.supplier?.website || '', api_key: '', models: ['gpt-5.6-sol'], enabled: true, interval_seconds: 30, timeout_seconds: 45, degraded_threshold_ms: 6000, account_ids: [], wallet_ref: 'default', notes: '' })
const form = reactive<UpstreamTargetInput>(defaults())
const saving = ref(false), error = ref(''), modelsLoading = ref(false), modelError = ref(''), importing = ref(false)
const discoveredModels = ref<string[]>([])
const accounts = ref<AccountListItem[]>([]), accountNames = ref<Record<number, string>>({}), accountsLoading = ref(false), accountError = ref(''), accountPicker = ref('')
const sourceAccount = ref<{ id: number; name: string } | null>(null)
const normalizedEndpoint = (value: string) => value.trim().replace(/\/+$/, '')
const canKeepSavedKey = computed(() => Boolean(props.target && normalizedEndpoint(form.endpoint) === normalizedEndpoint(props.target.endpoint) && form.provider === props.target.provider))
const accountOptions = computed(() => accounts.value.filter(account => ['openai', 'anthropic', 'gemini'].includes(account.platform) && !form.account_ids.includes(account.id)).map(account => ({ value: String(account.id), label: `${account.name} · ${account.platform}` })))
function clearSourceAccount() { sourceAccount.value = null }
function invalidateModels() { modelRequest++; modelsLoading.value = false; modelError.value = ''; discoveredModels.value = [] }
watch([() => form.endpoint, () => form.provider, () => form.api_key], () => { clearSourceAccount(); invalidateModels() }, { flush: 'sync' })
watch(sourceAccount, invalidateModels, { flush: 'sync' })
let accountRequest = 0, dialogGeneration = 0, modelRequest = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(() => props.show, show => {
  dialogGeneration++
  accountRequest++; clearTimeout(searchTimer); accountsLoading.value = false; importing.value = false; modelsLoading.value = false
  if (!show) return
  Object.assign(form, defaults(), props.target ? { supplier_id: props.target.supplier_id, name: props.target.name, provider: props.target.provider, api_mode: props.target.api_mode, endpoint: props.target.endpoint, models: [...props.target.models], enabled: props.target.enabled, interval_seconds: props.target.interval_seconds, timeout_seconds: props.target.timeout_seconds, degraded_threshold_ms: props.target.degraded_threshold_ms, account_ids: [...(props.target.account_ids || [])], wallet_ref: props.target.wallet_ref, notes: props.target.notes } : {})
  if (!props.supplier) form.account_ids = []
  sourceAccount.value = null; form.api_key = ''; error.value = ''; modelError.value = ''; discoveredModels.value = []; accountPicker.value = ''; accountError.value = ''
  void loadAccounts('')
}, { immediate: true })
async function loadAccounts(search: string) {
  const request = ++accountRequest
  accountsLoading.value = true
  try { const result = await accountsAPI.list(1, 100, { type: 'apikey', search, lite: '1' }); if (request !== accountRequest) return; accounts.value = result.items; result.items.forEach(account => { accountNames.value[account.id] = account.name }); accountError.value = '' }
  catch (err) { if (request === accountRequest) accountError.value = extractApiErrorMessage(err, t('upstreamCenter.form.accountLoadFailed')) }
  finally { if (request === accountRequest) accountsLoading.value = false }
}
function searchAccounts(search: string) { clearTimeout(searchTimer); searchTimer = setTimeout(() => void loadAccounts(search), 250) }
async function importAccount() {
  if (!accountPicker.value || importing.value) return
  const id = Number(accountPicker.value), generation = dialogGeneration
  importing.value = true
  try {
    const account = await accountsAPI.getById(id)
    if (generation !== dialogGeneration) return
    if (!props.supplier || !form.account_ids.length) {
      const endpoint = account.credentials?.base_url
      const defaultEndpoints = { openai: 'https://api.openai.com', anthropic: 'https://api.anthropic.com', gemini: 'https://generativelanguage.googleapis.com' }
      if (['openai', 'anthropic', 'gemini'].includes(account.platform)) {
        const provider = account.platform as UpstreamTargetInput['provider']
        if (form.provider !== provider) form.api_mode = 'chat_completions'
        form.provider = provider
        form.endpoint = typeof endpoint === 'string' && endpoint.trim() ? endpoint.trim() : defaultEndpoints[provider]
      }
      if (!form.name) form.name = account.name
    }
    if (props.supplier) {
      if (!form.account_ids.includes(id)) form.account_ids.push(id)
    } else {
      form.account_ids = []; form.api_key = ''; sourceAccount.value = { id, name: account.name }
    }
    accountNames.value[id] = account.name; accountPicker.value = ''; accountError.value = ''
  } catch (err) { if (generation === dialogGeneration) accountError.value = extractApiErrorMessage(err, t('upstreamCenter.form.accountLoadFailed')) }
  finally { if (generation === dialogGeneration) importing.value = false }
}
async function fetchModels() {
  const generation = dialogGeneration, request = ++modelRequest
  modelsLoading.value = true; modelError.value = ''
  try {
    const models = await upstreamCenterAPI.models({ target_id: sourceAccount.value || !canKeepSavedKey.value ? undefined : props.target?.id, account_id: sourceAccount.value?.id ?? (!props.target && !form.api_key?.trim() ? form.account_ids[0] : undefined), provider: form.provider, endpoint: form.endpoint.trim(), api_key: form.api_key?.trim() || undefined })
    if (generation !== dialogGeneration || request !== modelRequest) return
    discoveredModels.value = [...new Set(models || [])].sort()
    if (!models?.length) modelError.value = t('upstreamCenter.form.noModels')
  } catch (err) { if (generation === dialogGeneration && request === modelRequest) modelError.value = extractApiErrorMessage(err, t('upstreamCenter.form.fetchFailed')) }
  finally { if (generation === dialogGeneration && request === modelRequest) modelsLoading.value = false }
}
async function save() {
  if (saving.value) return
  error.value = ''
  const models = [...new Set(form.models.map(model => model.trim()).filter(Boolean))]
  if (!models.length) { error.value = t('upstreamCenter.form.requiredModels'); return }
  if (models.length > 8) { error.value = t('upstreamCenter.form.maxModels'); return }
  if (!canKeepSavedKey.value && !form.api_key?.trim() && !sourceAccount.value && !(props.supplier && form.account_ids.length)) { error.value = t('upstreamCenter.form.requiredKey'); return }
  try { const url = new URL(form.endpoint); if (url.protocol !== 'https:') throw new Error() } catch { error.value = t('upstreamCenter.form.validUrl'); return }
  saving.value = true
  try {
    const input = { ...form, name: form.name.trim(), endpoint: form.endpoint.trim(), api_key: form.api_key?.trim() || undefined, source_account_id: !props.supplier ? sourceAccount.value?.id : undefined, models, account_ids: props.supplier ? [...form.account_ids] : [], notes: form.notes.trim(), wallet_ref: form.wallet_ref.trim() || 'default' }
    if (props.target) await upstreamCenterAPI.updateTarget(props.target.id, input)
    else await upstreamCenterAPI.createTarget(input)
    emit('saved'); emit('close')
  } catch (err) { error.value = extractApiErrorMessage(err, t('upstreamCenter.saveFailed')) }
  finally { saving.value = false }
}
onBeforeUnmount(() => { clearTimeout(searchTimer); accountRequest++; dialogGeneration++ })
</script>
