<template>
  <Select
    :model-value="modelValue"
    :options="options"
    :disabled="disabled"
    :placeholder="placeholder || t('admin.accounts.proxyPool.selectProxy')"
    :aria-label="ariaLabel || t('admin.accounts.proxy')"
    :searchable="true"
    :search-placeholder="t('admin.proxies.searchProxies')"
    @click.capture="refreshAvailability"
    @keydown.capture="refreshAvailability"
    @update:model-value="selectProxy"
  >
    <template #selected>
      <span class="block truncate" :title="selectedLabel">{{ selectedLabel }}</span>
    </template>
    <template #after-search="{ searchQuery }">
      <div class="flex items-center justify-between gap-3 border-b border-gray-100 px-3 py-2 text-xs dark:border-dark-700">
        <span class="text-gray-500 dark:text-gray-400">
          {{ t('admin.proxies.selectorAvailable', { count: testableProxies(searchQuery).length }) }}
        </span>
        <button
          type="button"
          class="batch-test-btn flex items-center gap-1 rounded px-2 py-1 text-primary-600 hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
          :disabled="disabled || batchTesting || !testableProxies(searchQuery).length"
          :title="t('admin.proxies.testVisible')"
          @click.stop="handleBatchTest(searchQuery)"
          @keydown.enter.stop
          @keydown.space.stop
        >
          <Icon :name="batchTesting ? 'refresh' : 'play'" size="sm" :class="{ 'animate-spin': batchTesting }" />
          {{ t('admin.proxies.testVisible') }}
        </button>
      </div>
    </template>
    <template #option="{ option, selected }">
      <template v-if="option.proxy">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="truncate font-medium">{{ option.proxy.name }}</span>
            <span v-if="option.reason" class="rounded bg-amber-50 px-1.5 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
              {{ t('admin.accounts.testProxyOptions.' + option.reason) }}
            </span>
            <span
              v-else-if="proxyTestResult(option.proxy)"
              class="rounded px-1.5 py-0.5 text-xs"
              :class="proxyTestResult(option.proxy)?.success ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'"
              :title="proxyTestResult(option.proxy)?.message"
            >
              <template v-if="proxyTestResult(option.proxy)?.success">
                {{ proxyTestResult(option.proxy)?.country }}
                <span v-if="proxyTestResult(option.proxy)?.latency_ms != null">{{ proxyTestResult(option.proxy)?.latency_ms }}ms</span>
                <span v-else>{{ t('common.success') }}</span>
              </template>
              <template v-else>{{ t('admin.proxies.testFailed') }}</template>
            </span>
          </div>
          <div class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400">
            {{ option.proxy.protocol }}://{{ option.proxy.host }}:{{ option.proxy.port }}
          </div>
          <div class="mt-0.5 flex flex-wrap gap-x-3 text-xs text-gray-500 dark:text-gray-400">
            <span v-if="proxyTestResult(option.proxy)?.ip_address || option.proxy.ip_address">
              {{ proxyTestResult(option.proxy)?.ip_address || option.proxy.ip_address }}
            </span>
            <span v-if="option.proxy.country || option.proxy.city">{{ [option.proxy.country, option.proxy.city].filter(Boolean).join(' · ') }}</span>
            <span v-if="option.proxy.account_count !== undefined">{{ t('admin.proxies.usedByAccounts', { count: option.proxy.account_count }) }}</span>
          </div>
        </div>
        <button
          type="button"
          class="test-btn flex-shrink-0 rounded p-1.5 text-gray-400 hover:bg-emerald-50 hover:text-emerald-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-emerald-900/20"
          :disabled="disabled || !!option.reason || testingProxyIds.has(option.proxy.id)"
          :title="t('admin.proxies.testConnection')"
          :aria-label="t('admin.proxies.testConnection') + ' ' + option.proxy.name"
          @click.stop="handleTestProxy(option.proxy)"
          @keydown.enter.stop
          @keydown.space.stop
        >
          <Icon :name="testingProxyIds.has(option.proxy.id) ? 'refresh' : 'play'" size="sm" :class="{ 'animate-spin': testingProxyIds.has(option.proxy.id) }" />
        </button>
      </template>
      <span v-else class="truncate">{{ option.label }}</span>
      <Icon v-if="selected" name="check" size="sm" class="flex-shrink-0 text-primary-500" />
    </template>
  </Select>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Icon from '@/components/icons/Icon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { isProxySelectable, proxyUnavailableReason } from '@/utils/proxySelection'
import type { Proxy } from '@/types'

interface Props {
  modelValue: number | null
  proxies: Proxy[]
  disabled?: boolean
  allowNone?: boolean
  excludedIds?: number[]
  placeholder?: string
  ariaLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  allowNone: true,
  excludedIds: () => []
})
const emit = defineEmits<{ 'update:modelValue': [value: number | null] }>()
const { t } = useI18n()
const availabilityTime = ref(Date.now())
const refreshAvailability = () => { availabilityTime.value = Date.now() }
type ProxyTestResult = Awaited<ReturnType<typeof adminAPI.proxies.testProxy>>
const testResults = reactive<Record<number, ProxyTestResult>>({})
const testingProxyIds = reactive(new Set<number>())
const pendingTests = new Map<number, Promise<void>>()
const batchTesting = ref(false)

const candidateProxies = computed(() => {
  const excluded = new Set(props.excludedIds)
  return props.proxies.filter(proxy => !excluded.has(proxy.id) || proxy.id === props.modelValue)
})
const selectedLabel = computed(() => {
  if (props.modelValue === null) {
    return props.allowNone ? t('admin.accounts.noProxy') : props.placeholder || t('admin.accounts.proxyPool.selectProxy')
  }
  const proxy = props.proxies.find(item => item.id === props.modelValue)
  if (!proxy) return t('admin.accounts.proxyPool.missingProxy', { id: props.modelValue })
  const reason = proxyUnavailableReason(proxy, availabilityTime.value)
  return proxy.name + ' (' + proxy.host + ':' + proxy.port + ')' + (reason ? ' · ' + t('admin.accounts.testProxyOptions.' + reason) : '')
})

const proxyDescription = (proxy: Proxy) => [proxy.protocol, proxy.host + ':' + proxy.port, proxy.ip_address, proxy.country, proxy.country_code, proxy.city].filter(Boolean).join(' ')
const options = computed<SelectOption[]>(() => {
  const items: SelectOption[] = candidateProxies.value.map(proxy => {
    const reason = proxyUnavailableReason(proxy, availabilityTime.value)
    return { value: proxy.id, label: proxy.name, description: proxyDescription(proxy), proxy, reason,
      disabled: !!reason || props.excludedIds.includes(proxy.id) }
  })
  // Keep usable proxies first; preserve the server order within each group.
  items.sort((a, b) => Number(!!a.disabled) - Number(!!b.disabled))
  if (props.modelValue !== null && !items.some(item => item.value === props.modelValue)) {
    items.push({ value: props.modelValue, label: selectedLabel.value, disabled: true })
  }
  if (props.allowNone) items.unshift({ value: null, label: t('admin.accounts.noProxy') })
  return items
})

const selectProxy = (value: SelectOption['value']) => {
  if (props.disabled) return
  if (value === null && props.allowNone) emit('update:modelValue', null)
  if (typeof value !== 'number' || props.excludedIds.includes(value)) return
  if (isProxySelectable(props.proxies.find(proxy => proxy.id === value))) emit('update:modelValue', value)
}

const proxyTestResult = (proxy: Proxy): ProxyTestResult | undefined => testResults[proxy.id] || (proxy.latency_status ? {
  success: proxy.latency_status === 'success', message: proxy.latency_message || '',
  latency_ms: proxy.latency_ms, country: proxy.country, ip_address: proxy.ip_address
} : undefined)

const testableProxies = (query = '') => {
  const search = query.trim().toLowerCase()
  return candidateProxies.value.filter(proxy => isProxySelectable(proxy, availabilityTime.value)
    && !props.excludedIds.includes(proxy.id)
    && (!search || (proxy.name + ' ' + proxyDescription(proxy)).toLowerCase().includes(search)))
}

const handleTestProxy = (proxy: Proxy): Promise<void> => {
  const pending = pendingTests.get(proxy.id)
  if (pending) return pending
  if (props.disabled || !isProxySelectable(proxy) || props.excludedIds.includes(proxy.id)) return Promise.resolve()
  testingProxyIds.add(proxy.id)
  const task = Promise.resolve().then(async () => {
    try {
      testResults[proxy.id] = await adminAPI.proxies.testProxy(proxy.id)
    } catch {
      testResults[proxy.id] = { success: false, message: t('admin.proxies.testFailed') }
    } finally {
      testingProxyIds.delete(proxy.id)
      pendingTests.delete(proxy.id)
    }
  })
  pendingTests.set(proxy.id, task)
  return task
}

const handleBatchTest = async (query: string) => {
  if (props.disabled || batchTesting.value) return
  const proxies = testableProxies(query)
  if (!proxies.length) return
  batchTesting.value = true
  let next = 0
  try {
    await Promise.all(Array.from({ length: Math.min(5, proxies.length) }, async () => {
      while (next < proxies.length) await handleTestProxy(proxies[next++])
    }))
  } finally {
    batchTesting.value = false
  }
}
</script>
