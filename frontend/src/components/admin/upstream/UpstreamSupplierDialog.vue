<template>
  <BaseDialog :show="show" :title="t(supplier ? 'upstreamCenter.editSupplier' : 'upstreamCenter.addSupplier')" width="wide" :close-on-escape="!saving" :show-close-button="!saving" @close="close">
    <form id="upstream-supplier-form" class="space-y-4" @submit.prevent="save">
      <div v-if="!supplier" class="rounded-xl border border-primary-100 bg-primary-50/30 p-3 dark:border-primary-900/60 dark:bg-primary-500/5">
        <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="flex items-center gap-2 text-sm font-medium text-gray-800 dark:text-gray-200"><Icon name="server" size="sm" class="text-primary-600" />{{ t('upstreamCenter.import.title') }}</h3><span class="text-xs text-primary-600 dark:text-primary-400">{{ t('upstreamCenter.import.selected', { count: selected.length }) }}</span></div>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.import.hint') }}</p>
        <div class="mt-3 flex items-center gap-2"><input v-model="accountSearch" class="input !py-2 !text-xs" :placeholder="t('upstreamCenter.form.accountSearch')" :aria-label="t('upstreamCenter.form.accountSearch')" :disabled="saving" @input="searchAccounts" /><button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="accountsLoading" @click="loadAccounts(1)"><Icon name="refresh" size="xs" :class="accountsLoading && 'animate-spin'" /></button></div>
        <div class="mt-2 max-h-48 overflow-y-auto rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-900/40">
          <label v-for="account in accounts" :key="account.id" class="flex cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2.5 last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-800"><input type="checkbox" class="h-3.5 w-3.5 rounded border-gray-300 accent-teal-600" :checked="selected.some(item => item.id === account.id)" :disabled="saving || preparingIds.has(account.id) || selected.some(item => item.id === account.id && item.status === 'done')" :aria-label="account.name" @change="toggleAccount(account)" /><span class="min-w-0 flex-1"><span class="block truncate text-xs font-medium text-gray-800 dark:text-gray-200">{{ account.name }}</span><span class="mt-0.5 block truncate text-[10px] text-gray-400">{{ account.platform }} · #{{ account.id }}</span></span><span v-if="preparingIds.has(account.id)" class="text-[10px] text-gray-400">{{ t('upstreamCenter.import.loading') }}</span></label>
          <p v-if="!accounts.length" class="px-3 py-5 text-center text-xs text-gray-400">{{ t(accountsLoading ? 'upstreamCenter.import.loading' : 'upstreamCenter.form.noAccounts') }}</p>
        </div>
        <div v-if="accountTotal > 50" class="mt-2 flex items-center justify-end gap-2 text-[11px] text-gray-500"><button type="button" class="rounded px-2 py-1 disabled:opacity-40" :disabled="accountPage === 1 || accountsLoading" @click="loadAccounts(accountPage - 1)">{{ t('pagination.previous') }}</button><span>{{ accountPage }} / {{ Math.ceil(accountTotal / 50) }}</span><button type="button" class="rounded px-2 py-1 disabled:opacity-40" :disabled="accountPage * 50 >= accountTotal || accountsLoading" @click="loadAccounts(accountPage + 1)">{{ t('pagination.next') }}</button></div>
        <p v-if="accountError" role="alert" class="mt-2 text-xs text-rose-600 dark:text-rose-400">{{ accountError }}</p>
      </div>
      <div class="grid gap-4 sm:grid-cols-2"><div><label for="supplier-name" class="input-label">{{ t('upstreamCenter.form.name') }}</label><input id="supplier-name" v-model="form.name" required maxlength="100" class="input" :disabled="saving || identityLocked" :placeholder="t('upstreamCenter.form.supplierNamePlaceholder')" /></div><div><label for="supplier-website" class="input-label">{{ t('upstreamCenter.form.website') }}</label><input id="supplier-website" v-model="form.website" type="url" required class="input" :disabled="saving || identityLocked" :placeholder="t('upstreamCenter.form.websitePlaceholder')" /></div></div>
      <div v-if="selected.length" class="space-y-3">
        <div class="flex items-center justify-between gap-2"><h3 class="text-xs font-medium text-gray-700 dark:text-gray-200">{{ t('upstreamCenter.import.groups') }}</h3><label class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400"><input v-model="enabled" type="checkbox" class="accent-teal-600" :disabled="saving" />{{ t('upstreamCenter.form.enabled') }}</label></div>
        <p class="text-[11px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.import.modelHint') }}</p>
        <div v-for="item in selected" :key="item.id" class="rounded-lg border p-3" :class="item.status === 'error' ? 'border-rose-200 dark:border-rose-900' : item.status === 'done' ? 'border-primary-200 dark:border-primary-900' : 'border-gray-200 dark:border-dark-700'">
          <div class="flex items-center gap-2"><span class="min-w-0 flex-1 truncate text-xs font-medium text-gray-800 dark:text-gray-200">{{ item.name }}<span class="ml-2 text-[10px] font-normal text-gray-400">{{ item.provider }} · #{{ item.id }}</span></span><span v-if="item.status === 'done'" class="text-[10px] text-primary-600 dark:text-primary-400">{{ t('upstreamCenter.import.done') }}</span><Icon v-else-if="item.status === 'saving'" name="refresh" size="xs" class="animate-spin text-primary-500" /><button v-else type="button" :disabled="saving" :aria-label="`${t('upstreamCenter.remove')} ${item.name}`" class="text-gray-400 hover:text-rose-500" @click="selected = selected.filter(value => value.id !== item.id)"><Icon name="x" size="xs" /></button></div>
          <div class="mt-2 grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_100px]"><div><label :for="`import-name-${item.id}`" class="mb-1 block text-[10px] text-gray-500">{{ t('upstreamCenter.finance.group') }}</label><input :id="`import-name-${item.id}`" v-model="item.targetName" maxlength="100" required class="input !py-1.5 !text-xs" :disabled="saving || item.status === 'done'" /></div><div><label :for="`import-model-${item.id}`" class="mb-1 block text-[10px] text-gray-500">{{ t('upstreamCenter.form.models') }}</label><input :id="`import-model-${item.id}`" v-model="item.modelsText" required class="input !py-1.5 !text-xs" :disabled="saving || item.status === 'done'" /></div><div><label :for="`import-interval-${item.id}`" class="mb-1 block text-[10px] text-gray-500">{{ t('upstreamCenter.form.interval') }}</label><input :id="`import-interval-${item.id}`" v-model.number="item.intervalSeconds" type="number" min="30" max="3600" step="1" required class="input !py-1.5 !text-xs" :disabled="saving || item.status === 'done'" /></div></div>
          <p v-if="item.error" class="mt-2 break-words text-xs text-rose-600 dark:text-rose-400">{{ item.error }}</p>
        </div>
      </div>
      <div><label for="supplier-notes" class="input-label">{{ t('upstreamCenter.form.notes') }}</label><textarea id="supplier-notes" v-model="form.notes" class="input min-h-[64px] resize-y" maxlength="2000" :disabled="saving || identityLocked" :placeholder="t('upstreamCenter.form.optional')"></textarea></div>
      <p v-if="createdSupplierId && selected.some(item => item.status !== 'done')" role="status" class="rounded-lg bg-amber-50 p-3 text-xs leading-5 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400">{{ t('upstreamCenter.import.retryHint') }}</p>
      <div v-if="recoveryChoices.length" class="rounded-lg border border-amber-200 p-3 dark:border-amber-900"><label for="supplier-recovery" class="mb-2 block text-xs text-amber-700 dark:text-amber-400">{{ t('upstreamCenter.import.chooseExisting') }}</label><Select id="supplier-recovery" v-model="recoveryId" :options="recoveryOptions" :searchable="false" :aria-label="t('upstreamCenter.import.chooseExisting')" /></div>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-600 dark:bg-red-500/10 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t(createdSupplierId ? 'common.close' : 'common.cancel') }}</button><button type="submit" form="upstream-supplier-form" class="btn btn-primary" :disabled="saving || preparingIds.size > 0"><Icon v-if="saving" name="refresh" size="sm" class="mr-2 animate-spin" />{{ t(saving ? 'upstreamCenter.form.saving' : createdSupplierId ? 'upstreamCenter.import.retry' : selected.length ? 'upstreamCenter.import.save' : 'upstreamCenter.form.save') }}</button></div></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { upstreamCenterAPI, type UpstreamProvider, type UpstreamSupplier } from '@/api/admin/upstreamCenter'
import * as accountsAPI from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ show: boolean; supplier: UpstreamSupplier | null }>()
const emit = defineEmits<{ close: []; saved: []; changed: [] }>()
const { t } = useI18n()
const form = reactive({ name: '', website: '', notes: '' })
interface ImportAccount { id: number; name: string; targetName: string; provider: UpstreamProvider; endpoint: string; modelsText: string; intervalSeconds: number; status: 'pending' | 'saving' | 'done' | 'error'; error: string }
const selected = ref<ImportAccount[]>([]), enabled = ref(true), saving = ref(false), error = ref(''), createdSupplierId = ref<number | null>(null)
const identityLocked = ref(false)
const accounts = ref<AccountListItem[]>([]), accountsLoading = ref(false), accountSearch = ref(''), accountPage = ref(1), accountTotal = ref(0), accountError = ref(''), preparingIds = ref(new Set<number>())
const recoveryChoices = ref<UpstreamSupplier[]>([]), recoveryId = ref('')
const recoveryOptions = computed(() => [
  { value: '', label: t('upstreamCenter.import.choosePlaceholder') },
  ...recoveryChoices.value.map(item => ({ value: String(item.id), label: `#${item.id} · ${item.name} · ${item.website}` }))
])
let accountRequest = 0, generation = 0, supplierAttempted = false, draftPending = false
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(() => props.show, show => {
  generation++
  accountRequest++; clearTimeout(searchTimer); accountsLoading.value = false; preparingIds.value = new Set(); accountError.value = ''
  if (!show) return
  if (!props.supplier && draftPending) { void loadAccounts(1); return }
  Object.assign(form, { name: props.supplier?.name || '', website: props.supplier?.website || '', notes: props.supplier?.notes || '' })
  selected.value = []; createdSupplierId.value = null; identityLocked.value = false; enabled.value = true; error.value = ''; recoveryChoices.value = []; recoveryId.value = ''; supplierAttempted = false; draftPending = false; accountSearch.value = ''
  if (!props.supplier) void loadAccounts(1)
}, { immediate: true })
function close() { if (!saving.value) { if (createdSupplierId.value) emit('changed'); emit('close') } }
async function loadAccounts(page: number) {
  const request = ++accountRequest; accountsLoading.value = true; accountError.value = ''
  try { const result = await accountsAPI.list(page, 50, { type: 'apikey', search: accountSearch.value.trim(), lite: '1' }); if (request !== accountRequest) return; accounts.value = result.items.filter(item => ['openai', 'anthropic', 'gemini'].includes(item.platform)); accountTotal.value = result.total; accountPage.value = page }
  catch (err) { if (request === accountRequest) accountError.value = extractApiErrorMessage(err, t('upstreamCenter.form.accountLoadFailed')) }
  finally { if (request === accountRequest) accountsLoading.value = false }
}
function searchAccounts() { clearTimeout(searchTimer); searchTimer = setTimeout(() => void loadAccounts(1), 250) }
async function toggleAccount(account: AccountListItem) {
  if (saving.value || preparingIds.value.has(account.id)) return
  if (selected.value.some(item => item.id === account.id)) { selected.value = selected.value.filter(item => item.id !== account.id); return }
  const currentGeneration = generation
  preparingIds.value = new Set([...preparingIds.value, account.id])
  try {
    const detail = await accountsAPI.getById(account.id)
    if (currentGeneration !== generation) return
    const endpoint = typeof detail.credentials?.base_url === 'string' ? detail.credentials.base_url : form.website.trim()
    const provider = detail.platform as UpstreamProvider
    selected.value.push({ id: account.id, name: detail.name, targetName: detail.name, provider, endpoint, modelsText: 'gpt-5.6-sol', intervalSeconds: 30, status: 'pending', error: '' })
    if (!form.name) form.name = detail.name
    if (!form.website && endpoint) { try { form.website = new URL(endpoint).origin } catch { form.website = endpoint } }
    accountError.value = ''
  } catch (err) { if (currentGeneration === generation) accountError.value = extractApiErrorMessage(err, t('upstreamCenter.form.accountLoadFailed')) }
  finally { if (currentGeneration === generation) { const ids = new Set(preparingIds.value); ids.delete(account.id); preparingIds.value = ids } }
}
const parsedModels = (value: string) => [...new Set(value.split(/[,，;；\n]/).map(model => model.trim()).filter(Boolean))]
const normalizedURL = (value: string) => value.trim().replace(/\/+$/, '')
async function resolveSupplier(): Promise<number | null> {
  if (createdSupplierId.value) return createdSupplierId.value
  if (supplierAttempted) {
    const overview = await upstreamCenterAPI.overview()
    const matches = overview.suppliers.filter(item => item.name === form.name.trim() && normalizedURL(item.website) === normalizedURL(form.website))
    if (matches.length > 1) {
      const choice = matches.find(item => String(item.id) === recoveryId.value)
      if (!choice) { recoveryChoices.value = matches; error.value = t('upstreamCenter.import.chooseExisting'); return null }
      createdSupplierId.value = choice.id
    } else if (matches.length === 1) createdSupplierId.value = matches[0]!.id
    if (createdSupplierId.value) return createdSupplierId.value
  }
  supplierAttempted = true; draftPending = true; identityLocked.value = true
  try {
    const result = await upstreamCenterAPI.createSupplier({ name: form.name.trim(), website: form.website.trim(), notes: form.notes.trim() })
    createdSupplierId.value = result.id; emit('changed')
    return result.id
  } catch (err) {
    const status = (err as { status?: number; response?: { status?: number } })?.status ?? (err as { response?: { status?: number } })?.response?.status
    if (status && status >= 400 && status < 500) { supplierAttempted = false; draftPending = false; identityLocked.value = false }
    throw err
  }
}
async function save() {
  if (saving.value) return
  error.value = ''
  for (const item of selected.value.filter(item => item.status !== 'done')) {
    const models = parsedModels(item.modelsText)
    if (!models.length || models.length > 8 || !item.targetName.trim()) { error.value = `${item.name}: ${t(models.length > 8 ? 'upstreamCenter.form.maxModels' : 'upstreamCenter.form.requiredModels')}`; return }
  }
  saving.value = true
  try {
    if (props.supplier) { await upstreamCenterAPI.updateSupplier(props.supplier.id, { name: form.name.trim(), website: form.website.trim(), notes: form.notes.trim() }); emit('saved'); emit('close'); return }
    const id = await resolveSupplier()
    if (!id) return
    const existing = selected.value.length ? (await upstreamCenterAPI.overview()).suppliers.find(item => item.id === id) : undefined
    for (const item of selected.value) {
      if (item.status === 'done') continue
      if (existing?.targets.some(target => target.account_ids.includes(item.id))) { item.status = 'done'; item.error = ''; continue }
      item.status = 'saving'; item.error = ''
      try {
        await upstreamCenterAPI.createTarget({ supplier_id: id, name: item.targetName.trim(), provider: item.provider, api_mode: 'chat_completions', endpoint: item.endpoint || form.website.trim(), models: parsedModels(item.modelsText), enabled: enabled.value, interval_seconds: item.intervalSeconds, timeout_seconds: 45, degraded_threshold_ms: 6000, account_ids: [item.id], wallet_ref: 'default', notes: '' })
        item.status = 'done'; emit('changed')
      } catch (err) { item.status = 'error'; item.error = extractApiErrorMessage(err, t('upstreamCenter.saveFailed')) }
    }
    if (selected.value.some(item => item.status !== 'done')) { error.value = t('upstreamCenter.import.partial'); return }
    draftPending = false; emit('saved'); emit('close')
  } catch (err) { error.value = extractApiErrorMessage(err, t('upstreamCenter.saveFailed')) }
  finally { saving.value = false }
}
onBeforeUnmount(() => { clearTimeout(searchTimer); accountRequest++; generation++ })
</script>
