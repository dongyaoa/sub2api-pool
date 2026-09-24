<template>
  <div class="space-y-4 rounded-xl border border-gray-200 p-3 dark:border-dark-600 sm:p-4">
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0 flex-1">
        <label class="input-label mb-0">{{ t('admin.accounts.proxyPool.title') }}</label>
        <p class="input-hint mt-1">{{ t('admin.accounts.proxyPool.hint') }}</p>
      </div>
      <span class="rounded-md bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">
        {{ t('admin.accounts.proxyPool.selectedCount', { count: entries.length }) }}
      </span>
    </div>

    <div class="space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
      <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_8rem_auto] sm:items-end">
        <div class="min-w-0">
          <label class="input-label">{{ t('admin.accounts.proxyPool.selectProxy') }}</label>
          <ProxySelector
            v-model="pendingProxyId"
            data-testid="pool-add-selector"
            :proxies="proxies"
            :excluded-ids="selectedIds"
            :allow-none="false"
            :placeholder="t('admin.accounts.proxyPool.selectProxy')"
            :disabled="availableProxies.length === 0"
          />
        </div>
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.proxyPool.defaultConcurrency') }}</span>
          <input
            v-model.number="newConcurrency"
            data-testid="pool-new-concurrency"
            type="number"
            min="1"
            :max="MAX_POOL_CONCURRENCY"
            step="1"
            inputmode="numeric"
            class="input"
            @change="normalizeNewConcurrency"
          />
        </label>
        <button
          type="button"
          data-testid="pool-add"
          class="btn btn-primary justify-center whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="!pendingProxy || !canAddEntries(1)"
          @click="addEntry"
        >
          <Icon name="plus" size="sm" />
          <span>{{ t('admin.accounts.proxyPool.add') }}</span>
        </button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
        <p class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.proxyPool.defaultConcurrencyHint') }}</p>
        <button
          v-if="availableProxies.length > 0"
          type="button"
          data-testid="pool-add-all"
          class="rounded px-1 py-1 font-medium text-primary-600 hover:bg-primary-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
          :disabled="!canAddEntries(availableProxies.length)"
          @click="addAllEntries"
        >
          {{ t('admin.accounts.proxyPool.addAll', { count: availableProxies.length }) }}
        </button>
        <span v-else class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.proxyPool.allAdded') }}</span>
      </div>
      <p v-if="showLimitWarning" data-testid="pool-limits" class="text-xs text-amber-700 dark:text-amber-400">
        {{ t('admin.accounts.proxyPool.limitsHint', { maxEntries: MAX_POOL_ENTRIES, maxConcurrency: MAX_POOL_CONCURRENCY }) }}
      </p>
    </div>

    <div v-if="entries.length" class="space-y-3">
      <div
        v-for="(entry, index) in entries"
        :key="`${entry.proxy_id}-${index}`"
        data-testid="pool-entry"
        class="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-2 rounded-lg border border-gray-100 p-3 dark:border-dark-700 sm:grid-cols-[minmax(0,1fr)_8rem_auto]"
      >
        <div class="col-span-2 min-w-0 sm:col-span-1">
          <label class="input-label">{{ t('admin.accounts.proxyPool.proxy') }} {{ index + 1 }}</label>
          <ProxySelector
            :model-value="entry.proxy_id"
            :proxies="displayProxies"
            :excluded-ids="selectedIds.filter(id => id !== entry.proxy_id)"
            :allow-none="false"
            :placeholder="t('admin.accounts.proxyPool.missingProxy', { id: entry.proxy_id })"
            @update:model-value="replaceProxy(index, $event)"
          />
        </div>
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.proxyPool.concurrency') }}</span>
          <input
            v-model.number="entry.concurrency"
            data-testid="pool-entry-concurrency"
            type="number"
            min="1"
            :max="maxEntryConcurrency(index)"
            step="1"
            inputmode="numeric"
            class="input"
            @change="normalizeConcurrency(entry, index)"
          />
        </label>
        <button
          type="button"
          data-testid="pool-remove"
          class="btn btn-icon mb-0.5 text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20"
          :title="t('admin.accounts.proxyPool.remove')"
          :aria-label="t('admin.accounts.proxyPool.remove')"
          @click="removeEntry(index)"
        >
          <Icon name="trash" size="sm" />
        </button>
        <p
          v-if="entryUnavailable(entry)"
          data-testid="pool-unavailable"
          class="col-span-full text-xs text-amber-700 dark:text-amber-400"
        >
          {{ t('admin.accounts.proxyPool.unavailable') }}
        </p>
      </div>
    </div>
    <p v-else class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.proxyPool.empty') }}
    </p>

    <div class="flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 pt-3 text-xs dark:border-dark-600">
      <span class="text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.proxyPool.availableCount', { count: availableProxies.length }) }}
      </span>
      <span v-if="entries.length" class="text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.proxyPool.total') }}
        <strong data-testid="pool-total" class="ml-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ totalConcurrency }}</strong>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { isProxySelectable } from '@/utils/proxySelection'
import type { AccountProxyPoolEntry, Proxy } from '@/types'

const DEFAULT_PROXY_CONCURRENCY = 20
const MAX_POOL_ENTRIES = 256
const MAX_POOL_CONCURRENCY = 100000
const { t } = useI18n()

const props = defineProps<{
  modelValue: AccountProxyPoolEntry[]
  proxies: Proxy[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: AccountProxyPoolEntry[]]
}>()

const entries = ref<AccountProxyPoolEntry[]>([])
const pendingProxyId = ref<number | null>(null)
const newConcurrency = ref(DEFAULT_PROXY_CONCURRENCY)
const concurrencyLimitWarning = ref(false)

watch(
  () => props.modelValue,
  value => { entries.value = value.map(entry => ({ ...entry })) },
  { immediate: true, deep: true }
)

const selectedIds = computed(() => entries.value.map(entry => entry.proxy_id))
const availableProxies = computed(() => {
  const used = new Set(selectedIds.value)
  return props.proxies.filter(proxy => !used.has(proxy.id) && isProxySelectable(proxy))
})
const pendingProxy = computed(() => availableProxies.value.find(proxy => proxy.id === pendingProxyId.value))

// Keep hydrated details for existing entries even when the available-proxy API
// omits disabled or deleted records. The editor must not silently drop a binding.
const displayProxies = computed(() => {
  const proxies = new Map(props.proxies.map(proxy => [proxy.id, proxy]))
  entries.value.forEach(entry => {
    if (entry.proxy && !proxies.has(entry.proxy_id)) proxies.set(entry.proxy_id, entry.proxy)
  })
  return [...proxies.values()]
})

const totalConcurrency = computed(() =>
  entries.value.reduce((total, entry) => total + positiveInteger(entry.concurrency, 1), 0)
)
const showLimitWarning = computed(() => concurrencyLimitWarning.value
  || entries.value.length > MAX_POOL_ENTRIES
  || totalConcurrency.value > MAX_POOL_CONCURRENCY
  || (availableProxies.value.length > 0 && !canAddEntries(availableProxies.value.length)))

function positiveInteger(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? Math.max(1, Math.trunc(number)) : fallback
}

function updateEntries(value: AccountProxyPoolEntry[]) {
  entries.value = value.map(entry => ({ ...entry }))
  emit('update:modelValue', entries.value.map(entry => ({ ...entry })))
}

function normalizeNewConcurrency() {
  newConcurrency.value = positiveInteger(newConcurrency.value, DEFAULT_PROXY_CONCURRENCY)
}

function canAddEntries(count: number): boolean {
  return entries.value.length + count <= MAX_POOL_ENTRIES
    && totalConcurrency.value + count * positiveInteger(newConcurrency.value, DEFAULT_PROXY_CONCURRENCY) <= MAX_POOL_CONCURRENCY
}

function addEntry() {
  // Resolve again on click: proxy availability or selected entries can change
  // while the picker is open, so a stale selection must not be added.
  const proxy = availableProxies.value.find(candidate => candidate.id === pendingProxyId.value)
  if (!proxy || !isProxySelectable(proxy) || !canAddEntries(1)) return
  normalizeNewConcurrency()
  updateEntries([...entries.value, { proxy_id: proxy.id, concurrency: newConcurrency.value }])
  pendingProxyId.value = null
}

function addAllEntries() {
  const proxies = availableProxies.value.filter(proxy => isProxySelectable(proxy))
  if (!proxies.length || !canAddEntries(proxies.length)) return
  normalizeNewConcurrency()
  updateEntries([
    ...entries.value,
    ...proxies.map(proxy => ({ proxy_id: proxy.id, concurrency: newConcurrency.value }))
  ])
  pendingProxyId.value = null
}

function replaceProxy(index: number, proxyId: number | null) {
  const entry = entries.value[index]
  const proxy = props.proxies.find(candidate => candidate.id === proxyId)
  if (!entry || !proxy || !isProxySelectable(proxy)) return
  if (entries.value.some((candidate, current) => current !== index && candidate.proxy_id === proxyId)) return
  updateEntries(entries.value.map((candidate, current) => current === index
    ? { proxy_id: proxy.id, concurrency: positiveInteger(entry.concurrency, 1), proxy }
    : candidate))
}

function removeEntry(index: number) {
  updateEntries(entries.value.filter((_, current) => current !== index))
}

function maxEntryConcurrency(index: number): number {
  const others = entries.value.reduce((total, entry, current) =>
    current === index ? total : total + positiveInteger(entry.concurrency, 1), 0)
  return Math.max(1, MAX_POOL_CONCURRENCY - others)
}

function normalizeConcurrency(entry: AccountProxyPoolEntry, index: number) {
  const normalized = positiveInteger(entry.concurrency, 1)
  const maximum = maxEntryConcurrency(index)
  concurrencyLimitWarning.value = normalized > maximum
  entry.concurrency = Math.min(normalized, maximum)
  updateEntries(entries.value)
}

function entryUnavailable(entry: AccountProxyPoolEntry): boolean {
  const proxy = displayProxies.value.find(candidate => candidate.id === entry.proxy_id)
  return !proxy || !isProxySelectable(proxy)
}
</script>
