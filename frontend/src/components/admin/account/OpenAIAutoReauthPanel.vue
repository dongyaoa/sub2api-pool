<template>
  <BaseDialog :show="show" :title="t(account ? 'admin.accounts.autoReauth.configure2fa' : 'admin.accounts.autoReauth.importNew')" width="extra-wide" @close="close">
    <div class="space-y-6">
      <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('admin.accounts.autoReauth.description') }}</p>

      <div v-if="loadFailed" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ t(`admin.accounts.autoReauth.loadErrors.${loadError}`) }}
        <button type="button" class="ml-2 underline" @click="refresh">{{ t('common.refresh') }}</button>
      </div>
      <div v-if="overview && !configured" role="alert" class="space-y-1 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        <p v-if="!overview.worker_configured">{{ t('admin.accounts.autoReauth.workerMissing') }}</p>
        <p v-if="!overview.encryption_key_configured">{{ t('admin.accounts.autoReauth.keyMissing') }}</p>
      </div>

      <form v-if="account" data-testid="reauth-bind-form" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700" autocomplete="off" @submit.prevent="submitBinding">
        <div>
          <h4 class="font-medium">{{ account.name }} <span class="text-xs text-gray-500">#{{ account.id }}</span></h4>
          <p class="mt-2 text-sm text-gray-500">{{ t('admin.accounts.autoReauth.bindHint') }}</p>
          <p class="mt-2 text-xs text-gray-500">{{ account.proxy_id ? t('admin.accounts.autoReauth.originalProxy', { id: account.proxy_id }) : t('admin.accounts.autoReauth.bindingProxyMissing') }}</p>
        </div>
        <div>
          <label for="reauth-paste" class="input-label">{{ t('admin.accounts.autoReauth.pasteLine') }}</label>
          <div class="flex gap-2">
            <input id="reauth-paste" v-model="pasteContent" data-testid="reauth-paste" type="password" class="input min-w-0 flex-1" :placeholder="t('admin.accounts.autoReauth.importPlaceholder')" autocomplete="new-password" :disabled="importing" @keydown.enter.prevent="fillPastedLine" />
            <button type="button" class="btn btn-secondary whitespace-nowrap" :disabled="!pasteContent || importing" @click="fillPastedLine">{{ t('admin.accounts.autoReauth.fillFields') }}</button>
          </div>
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <div class="md:col-span-2">
            <label for="reauth-email" class="input-label">{{ t('admin.accounts.autoReauth.loginEmail') }}</label>
            <input id="reauth-email" v-model="loginEmail" data-testid="reauth-email" type="email" class="input w-full" autocomplete="off" :disabled="importing" />
          </div>
          <div>
            <label for="reauth-password" class="input-label">{{ t('admin.accounts.autoReauth.loginPassword') }}</label>
            <input id="reauth-password" v-model="loginPassword" data-testid="reauth-password" type="password" class="input w-full" autocomplete="new-password" :disabled="importing" />
          </div>
          <div>
            <label for="reauth-totp" class="input-label">{{ t('admin.accounts.autoReauth.totpSecret') }}</label>
            <input id="reauth-totp" v-model="totpSecret" data-testid="reauth-totp" type="password" class="input w-full" autocomplete="new-password" :disabled="importing" />
          </div>
        </div>
        <p class="text-xs text-gray-500">{{ selectedStatus ? t('admin.accounts.autoReauth.secretsAlreadySaved') : t('admin.accounts.autoReauth.secretHint') }}</p>
        <label class="flex items-center gap-2 text-sm"><input v-model="bindingEnabled" type="checkbox" :disabled="importing" />{{ t('admin.accounts.autoReauth.autoRecover') }}</label>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500">{{ t('admin.accounts.autoReauth.saveBindingHint') }}</p>
          <button type="submit" data-testid="reauth-save-binding" class="btn btn-primary" :disabled="!canBind">{{ importing ? t('admin.accounts.autoReauth.importing') : t('admin.accounts.autoReauth.saveBinding') }}</button>
        </div>
        <p v-if="bindingSaved" data-testid="reauth-binding-saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-400">{{ t(bindingEnabled ? 'admin.accounts.autoReauth.bindingSaved' : 'admin.accounts.autoReauth.bindingSavedDisabled') }}</p>
      </form>

      <form v-else class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700" autocomplete="off" @submit.prevent="submitImport">
        <div>
          <label for="auto-reauth-content" class="input-label">{{ t('admin.accounts.autoReauth.importLabel') }}</label>
          <p id="auto-reauth-content-hint" class="mb-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.importHint') }}</p>
          <textarea
            id="auto-reauth-content"
            v-model="content"
            data-testid="reauth-content"
            class="input w-full font-mono text-sm"
            rows="5"
            :disabled="importing"
            :placeholder="t('admin.accounts.autoReauth.importPlaceholder')"
            aria-describedby="auto-reauth-content-hint"
            autocomplete="off"
            autocapitalize="none"
            autocorrect="off"
            :spellcheck="false"
            @input="results = []; actionError = ''"
          />
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.secretHint') }}</p>
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label for="auto-reauth-proxy" class="input-label">{{ t('admin.accounts.autoReauth.proxyLabel') }}</label>
            <Select id="auto-reauth-proxy" v-model="proxyID" :options="proxyOptions" :disabled="importing || proxiesLoading" searchable />
            <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.proxyHint') }}</p>
            <p v-if="proxiesFailed" role="alert" class="mt-2 text-xs text-red-600">{{ t('admin.accounts.autoReauth.proxiesFailed') }}</p>
            <p v-else-if="!proxiesLoading && !proxyChoices.length" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.accounts.autoReauth.noAvailableProxies') }}</p>
          </div>
          <div>
            <GroupSelector v-model="groupIDs" :groups="groups" platform="openai" :class="{ 'pointer-events-none opacity-60': importing }" />
            <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.groupsHint') }}</p>
          </div>
        </div>
        <div v-if="validation.length" data-testid="reauth-validation" class="max-h-40 overflow-auto rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800" aria-live="polite">
          <p v-for="row in validation" :key="row.line" :class="row.error ? 'text-red-600 dark:text-red-400' : 'text-gray-600 dark:text-dark-300'">
            {{ t('admin.accounts.autoReauth.line', { line: row.line }) }} · {{ row.email || '—' }} · {{ row.error ? errorLabel(row.error) : t('admin.accounts.autoReauth.validLine') }}
          </p>
        </div>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.preserveHint') }}</p>
          <button type="submit" data-testid="reauth-import" class="btn btn-primary" :disabled="!canImport">
            {{ importing ? t('admin.accounts.autoReauth.importing') : t('admin.accounts.autoReauth.importAction') }}
          </button>
        </div>
      </form>

      <div v-if="actionError" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ actionError }}</div>
      <div v-if="results.length" data-testid="reauth-results" class="space-y-2" aria-live="polite">
        <h4 class="font-medium">{{ t('admin.accounts.autoReauth.importResults') }}</h4>
        <div class="max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">
          <p v-for="row in results" :key="row.line" :class="row.error_code ? 'text-red-600 dark:text-red-400' : 'text-emerald-700 dark:text-emerald-400'">
            {{ t('admin.accounts.autoReauth.line', { line: row.line }) }} · {{ row.email || '—' }} · {{ row.error_code ? errorLabel(row.error_code) : statusLabel(row.status) }}
            <span v-if="row.account_id"> · #{{ row.account_id }}</span>
          </p>
        </div>
      </div>

      <section :aria-label="t('admin.accounts.autoReauth.accountsLabel')" class="space-y-3">
        <div class="flex items-center justify-between gap-3">
          <h4 class="font-medium">{{ t('admin.accounts.autoReauth.accountsLabel') }}</h4>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">{{ t('common.refresh') }}</button>
        </div>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.pollHint') }}</p>
        <p v-if="!overview && loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <p v-else-if="overview && !displayedAccounts.length" class="py-4 text-center text-sm text-gray-500">{{ t(account ? 'admin.accounts.autoReauth.notBound' : 'admin.accounts.autoReauth.empty') }}</p>
        <div v-else-if="displayedAccounts.length" class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
          <table class="w-full text-left text-sm">
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-300">
              <tr>
                <th scope="col" class="px-3 py-2">{{ t('admin.accounts.autoReauth.account') }}</th>
                <th scope="col" class="px-3 py-2">{{ t('admin.accounts.autoReauth.status') }}</th>
                <th scope="col" class="px-3 py-2">{{ t('admin.accounts.autoReauth.lastSuccess') }}</th>
                <th scope="col" class="px-3 py-2">{{ t('admin.accounts.autoReauth.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="account in displayedAccounts" :key="account.account_id" :data-account-id="account.account_id">
                <td class="px-3 py-3"><span class="break-all">{{ account.email }}</span><span class="mt-1 block text-xs text-gray-400">#{{ account.account_id }}</span></td>
                <td class="px-3 py-3">
                  <span class="font-medium" :class="getOpenAIAutoReauthStateClass(account)">{{ t(getOpenAIAutoReauthStateLabelKey(account)) }}</span>
                  <span class="mt-1 block text-xs text-gray-500">{{ t('admin.accounts.autoReauth.attempts', { count: account.attempts }) }}</span>
                  <span v-if="account.last_error_code" class="mt-1 block max-w-xs text-xs text-red-600 dark:text-red-400">{{ errorLabel(account.last_error_code) }}</span>
                  <span v-if="account.next_attempt_at" class="mt-1 block text-xs text-gray-500">{{ t('admin.accounts.autoReauth.nextAttempt', { time: displayDate(account.next_attempt_at) }) }}</span>
                </td>
                <td class="whitespace-nowrap px-3 py-3 text-xs">{{ displayDate(account.last_success_at) }}</td>
                <td class="px-3 py-3">
                  <div class="flex gap-2">
                    <button type="button" data-testid="reauth-toggle" class="btn btn-secondary whitespace-nowrap" :disabled="importing || actionAccountID !== null || (!account.enabled && !configured)" @click="setEnabled(account)">
                      {{ account.enabled ? t('admin.accounts.autoReauth.disable') : t('admin.accounts.autoReauth.enable') }}
                    </button>
                    <button type="button" data-testid="reauth-run" class="btn btn-secondary whitespace-nowrap" :disabled="importing || actionAccountID !== null || !configured || !account.enabled || isRunning(account.status)" @click="run(account)">
                      {{ t('admin.accounts.autoReauth.retry') }}
                    </button>
                    <button type="button" data-testid="reauth-manual" class="btn btn-secondary whitespace-nowrap" :disabled="importing || actionAccountID !== null || isRunning(account.status)" @click="openManual(account.account_id)">
                      {{ t('admin.accounts.autoReauth.manualAction') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="close">{{ t('common.close') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { openaiAutoReauthAPI, type OpenAIAutoReauthAccount, type OpenAIAutoReauthImportResult } from '@/api/admin/openaiAutoReauth'
import { useOpenAIAutoReauth } from '@/composables/useOpenAIAutoReauth'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import type { Account, AdminGroup } from '@/types'
import { validateReauthImport } from './openaiAutoReauthImport'
import { getOpenAIAutoReauthStateClass, getOpenAIAutoReauthStateLabelKey, isOpenAIAutoReauthRunning as isRunning } from '@/utils/openaiAutoReauthStatus'

const props = defineProps<{ show: boolean; groups: AdminGroup[]; account?: Pick<Account, 'id' | 'name' | 'proxy_id' | 'credentials'> | null }>()
const emit = defineEmits<{ close: []; changed: []; manual: [accountID: number] }>()
const { t, te } = useI18n()
const { overview, loading, loadFailed, loadError, refresh } = useOpenAIAutoReauth(toRef(props, 'show'))
const content = ref('')
const pasteContent = ref('')
const loginEmail = ref('')
const loginPassword = ref('')
const totpSecret = ref('')
const bindingEnabled = ref(true)
const bindingSaved = ref(false)
const proxyID = ref<number | null>(null)
const groupIDs = ref<number[]>([])
const proxyChoices = ref<{ value: number; label: string }[]>([])
const proxiesLoading = ref(false)
const proxiesFailed = ref(false)
const importing = ref(false)
const actionAccountID = ref<number | null>(null)
const actionError = ref('')
const results = ref<OpenAIAutoReauthImportResult[]>([])
let panelRevision = 0
let bindingStatusInitialized = false

const configured = computed(() => !!overview.value?.worker_configured && !!overview.value?.encryption_key_configured && !loadFailed.value)
const selectedStatus = computed(() => overview.value?.accounts.find(row => row.account_id === props.account?.id))
const displayedAccounts = computed(() => props.account ? (selectedStatus.value ? [selectedStatus.value] : []) : (overview.value?.accounts ?? []))
const validation = computed(() => validateReauthImport(content.value))
const canImport = computed(() => configured.value && !importing.value && proxyID.value !== null && validation.value.length > 0 && validation.value.every(row => !row.error))
const canBind = computed(() => configured.value && !importing.value && actionAccountID.value === null && !!props.account?.proxy_id && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(loginEmail.value.trim()) && loginPassword.value.length > 0 && /^[A-Z2-7]{16,}={0,6}$/i.test(totpSecret.value.replace(/\s/g, '')))
const proxyOptions = computed(() => [{ value: null, label: t('admin.accounts.autoReauth.noProxy') }, ...proxyChoices.value])
const knownErrors = new Set(['account_exists', 'identity_unknown', 'invalid_account', 'configuration_failed', 'credentials_updated', 'invalid_format', 'invalid_email', 'password_required', 'invalid_totp_secret', 'duplicate_email', 'proxy_required', 'invalid_proxy', 'proxy_not_found', 'proxy_unavailable', 'proxy_pool_not_supported', 'proxy_changed', 'proxy_ip_changed', 'account_not_found', 'account_ambiguous', 'account_conflict', 'account_changed', 'account_mismatch', 'identity_mismatch', 'credentials_missing', 'worker_not_configured', 'encryption_key_not_configured', 'login_failed', 'invalid_credentials', 'invalid_password', 'invalid_totp', 'totp_failed', 'manual_required', 'captcha_required', 'email_otp_required', 'device_verification_required', 'network_error', 'timeout', 'disabled', 'busy', 'cooldown', 'rate_limited', 'invalid_group', 'group_not_found', 'import_failed', 'refresh_failed', 'oauth_configuration_error', 'persistence_failed', 'state_changed', 'worker_unavailable', 'lock_unavailable', 'secret_unavailable', 'invalid_request', 'invalid_callback', 'unauthorized', 'payload_too_large', 'unsupported_proxy', 'unsupported_totp', 'state_mismatch', 'oauth_error', 'untrusted_login_origin', 'login_timeout', 'cancelled', 'browser_unavailable', 'login_navigation_failed', 'login_flow_unsupported', 'workspace_selection_required', 'email_verification_required', 'account_disabled', 'credentials_rejected'])

function statusLabel(status: string) {
  return t(getOpenAIAutoReauthStateLabelKey(status))
}
function errorLabel(code: string) {
  const normalized = code.toLowerCase().replace(/^openai_auto_reauth_/, '')
  const key = `admin.accounts.autoReauth.errors.${knownErrors.has(normalized) ? normalized : 'unknown'}`
  return te(key) ? t(key) : t('admin.accounts.autoReauth.errors.unknown')
}
function displayDate(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString()
}
function clearSensitiveState() {
  content.value = ''
  pasteContent.value = ''
  loginEmail.value = ''
  loginPassword.value = ''
  totpSecret.value = ''
  bindingSaved.value = false
  results.value = []
  actionError.value = ''
}
function close() {
  panelRevision++
  clearSensitiveState()
  bindingStatusInitialized = false
  emit('close')
}
function openManual(accountID: number) {
  close()
  emit('manual', accountID)
}

watch([() => props.show, () => props.account?.id], async ([visible]) => {
  const revision = ++panelRevision
  clearSensitiveState()
  bindingStatusInitialized = false
  importing.value = false
  actionAccountID.value = null
  proxyID.value = null
  groupIDs.value = []
  proxyChoices.value = []
  proxiesFailed.value = false
  if (!visible) return
  prefillAccount()
  if (props.account) return
  proxiesLoading.value = true
  try {
    const proxies = await adminAPI.proxies.getAll()
    if (revision !== panelRevision || !props.show) return
    proxyChoices.value = proxies.filter(proxy => proxy.status === 'active' && (!proxy.expires_at || new Date(proxy.expires_at).getTime() > Date.now()))
      .map(proxy => ({ value: proxy.id, label: `${proxy.name} (#${proxy.id})` }))
  } catch {
    if (revision === panelRevision && props.show) proxiesFailed.value = true
  } finally {
    if (revision === panelRevision) proxiesLoading.value = false
  }
}, { immediate: true })
watch(selectedStatus, () => {
  if (!loginEmail.value) prefillAccount()
  if (selectedStatus.value && !bindingStatusInitialized) {
    bindingEnabled.value = selectedStatus.value.enabled
    bindingStatusInitialized = true
  }
})
onBeforeUnmount(() => { panelRevision++; clearSensitiveState() })

async function submitImport() {
  if (!canImport.value) return
  const revision = panelRevision
  importing.value = true
  actionError.value = ''
  results.value = []
  try {
    const data = await openaiAutoReauthAPI.importAccounts({
      content: content.value,
      mode: 'create',
      ...(proxyID.value !== null ? { proxy_id: proxyID.value } : {}),
      ...(groupIDs.value.length ? { group_ids: [...groupIDs.value] } : {})
    })
    emit('changed')
    if (revision !== panelRevision || !props.show) return
    results.value = data.results
    // Clear the original source after any accepted batch, including partial failures.
    content.value = ''
    await refresh()
  } catch {
    if (revision === panelRevision && props.show) actionError.value = t('admin.accounts.autoReauth.importFailed')
  } finally {
    if (revision === panelRevision) importing.value = false
  }
}

function prefillAccount() {
  if (!props.account) return
  const candidate = selectedStatus.value?.email || props.account.credentials?.email || props.account.name
  loginEmail.value = typeof candidate === 'string' && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(candidate) ? candidate : ''
  bindingEnabled.value = selectedStatus.value?.enabled ?? true
}

function fillPastedLine() {
  const rows = validateReauthImport(pasteContent.value)
  if (rows.length !== 1 || rows[0].error) {
    actionError.value = errorLabel(rows[0]?.error || 'invalid_format')
    return
  }
  const fields = pasteContent.value.split(/\r?\n/).find(line => line.trim())!.split('----')
  loginEmail.value = fields[0].trim()
  loginPassword.value = fields[1]
  totpSecret.value = fields[2]
  pasteContent.value = ''
  actionError.value = ''
  bindingSaved.value = false
}

async function submitBinding() {
  if (!props.account || !canBind.value) return
  const revision = panelRevision
  importing.value = true
  actionError.value = ''
  bindingSaved.value = false
  try {
    await openaiAutoReauthAPI.saveCredentials(props.account.id, {
      email: loginEmail.value.trim(), password: loginPassword.value, totp_secret: totpSecret.value, enabled: bindingEnabled.value
    })
    emit('changed')
    if (revision !== panelRevision || !props.show) return
    loginPassword.value = ''
    totpSecret.value = ''
    pasteContent.value = ''
    bindingSaved.value = true
    await refresh()
  } catch (error) {
    if (revision === panelRevision && props.show) {
      const value = error as { reason?: string; message?: string; code?: string } | null
      const code = value?.reason || value?.message || value?.code
      actionError.value = typeof code === 'string' ? errorLabel(code) : t('admin.accounts.autoReauth.actionFailed')
    }
  } finally {
    if (revision === panelRevision) importing.value = false
  }
}

async function performAction(account: OpenAIAutoReauthAccount, action: () => Promise<void>) {
  if (importing.value || actionAccountID.value !== null) return
  const revision = panelRevision
  actionAccountID.value = account.account_id
  actionError.value = ''
  try {
    await action()
    emit('changed')
    if (revision === panelRevision && props.show) await refresh()
  } catch {
    if (revision === panelRevision && props.show) actionError.value = t('admin.accounts.autoReauth.actionFailed')
  } finally {
    if (revision === panelRevision) actionAccountID.value = null
  }
}
function setEnabled(account: OpenAIAutoReauthAccount) {
  return performAction(account, () => openaiAutoReauthAPI.setEnabled(account.account_id, !account.enabled))
}
function run(account: OpenAIAutoReauthAccount) {
  return performAction(account, () => openaiAutoReauthAPI.run(account.account_id))
}
</script>
