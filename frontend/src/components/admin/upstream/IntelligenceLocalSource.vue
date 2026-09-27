<template>
  <div class="space-y-3" data-testid="local-source">
    <div class="rounded-xl border border-primary-100 bg-primary-50/40 p-4 dark:border-primary-900/40 dark:bg-primary-500/5">
      <label for="intelligence-local-key" class="input-label">{{ t('intelligenceMonitor.local.key') }}</label>
      <div class="flex gap-2"><Select id="intelligence-local-key" :model-value="localKeyId ?? null" :options="keyOptions" :searchable="false" :loading="loading" :disabled="loading" :error="Boolean(error)" class="min-w-0 flex-1" :aria-label="t('intelligenceMonitor.local.key')" @update:model-value="selectKey"/><button type="button" class="btn btn-secondary btn-sm" :disabled="loading" :aria-label="t('intelligenceMonitor.refresh')" @click="load"><Icon name="refresh" size="sm" :class="loading && 'animate-spin'"/></button></div>
      <p class="mt-2 text-xs leading-5 text-gray-500">{{ t('intelligenceMonitor.local.keyHint') }}</p>
      <p v-if="selected" class="mt-2 flex flex-wrap items-center gap-2 text-[11px]"><span class="rounded-md bg-emerald-50 px-2 py-1 font-medium text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300">{{ selected.name }}</span><span class="font-mono text-gray-500">{{ selected.masked }}</span><span class="text-gray-400">{{ t('intelligenceMonitor.local.bindingFixed') }}</span></p>
      <p v-if="error" role="alert" class="mt-2 text-xs text-rose-500">{{ error }}</p>
    </div>
    <div><label for="intelligence-group" class="input-label">{{ t('intelligenceMonitor.form.selectGroup') }}</label><Select id="intelligence-group" :model-value="groupId" :options="groupOptions" :searchable="false" :loading="groupsLoading" :disabled="groupsLoading || Boolean(localKeyId)" :placeholder="t(groupsLoading ? 'common.loading' : 'intelligenceMonitor.form.selectGroup')" :empty-text="t('intelligenceMonitor.form.noGroups')" :aria-label="t('intelligenceMonitor.form.selectGroup')" @update:model-value="selectGroup"/><p class="mt-2 text-xs leading-5 text-gray-500">{{ t(localKeyId ? 'intelligenceMonitor.local.existingHint' : 'intelligenceMonitor.form.sourceHint') }}</p></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { list as listKeys } from '@/api/keys'
import type { AdminGroup, ApiKey } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ groups: AdminGroup[]; groupsLoading: boolean; groupId?: number | null; localKeyId?: number | null; savedKeyName?: string; savedKeyMask?: string; managedKeyIds?: number[] }>()
const emit = defineEmits<{ select: [value: { groupId: number | null; keyId: number | null; name?: string }] }>()
const { t } = useI18n()
type KeyChoice = { id: number; name: string; masked: string; groupId: number }
const keys = ref<KeyChoice[]>([]), loading = ref(false), error = ref(''), ready = ref(false)
let controller: AbortController | undefined
const selected = computed(() => keys.value.find(key => key.id === props.localKeyId))
const eligible = computed(() => keys.value.filter(key => !props.managedKeyIds?.includes(key.id) && props.groups.some(group => group.id === key.groupId)))
const groupOptions = computed(() => props.groups.map(group => ({ value: group.id, label: `${group.name} · ${group.rate_multiplier}×` })))
const keyOptions = computed(() => {
  const options = [{ value: null as number | null, label: t('intelligenceMonitor.local.managedKey') }, ...eligible.value.map(key => ({ value: key.id, label: `${key.name} · ${props.groups.find(group => group.id === key.groupId)?.name || ''} · ${key.masked}` }))]
  if (props.localKeyId && !eligible.value.some(key => key.id === props.localKeyId)) options.push({ value: props.localKeyId, label: `${props.savedKeyName || t('intelligenceMonitor.local.keyUnavailable')} · ${props.savedKeyMask || '••••'}` })
  return options
})
function activeKey(key: ApiKey) {
  return key.status === 'active' && key.group_id !== null && !(key.expires_at && Date.parse(key.expires_at) <= Date.now()) && !(key.quota > 0 && key.quota_used >= key.quota)
}
function mask(key: string) { return key.length > 10 ? `${key.slice(0, 3)}••••${key.slice(-4)}` : '••••' }
async function load() {
  controller?.abort(); const current = new AbortController(); controller = current; loading.value = true; ready.value = false; error.value = ''
  try {
    const found = new Map<number, KeyChoice>()
    for (let page = 1; !current.signal.aborted; page++) {
      const response = await listKeys(page, 100, { status: 'active' }, { signal: current.signal })
      if (current.signal.aborted) return
      for (const key of response.items) if (activeKey(key)) found.set(key.id, { id: key.id, name: key.name, masked: mask(key.key || ''), groupId: key.group_id! })
      const pages = response.pages || Math.ceil(response.total / (response.page_size || 100))
      if (page >= pages) break
      if (!response.items.length) throw new Error(t('intelligenceMonitor.local.keysFailed'))
    }
    keys.value = [...found.values()]; ready.value = true
  } catch (cause) { if (!current.signal.aborted) error.value = extractApiErrorMessage(cause, t('intelligenceMonitor.local.keysFailed')) }
  finally { if (!current.signal.aborted) loading.value = false }
}
function selectKey(value: string | number | boolean | null) {
  error.value = ''
  if (value === null) { emit('select', { groupId: props.groupId ?? null, keyId: null }); return }
  const key = eligible.value.find(item => item.id === value)
  if (!key) return
  const group = props.groups.find(item => item.id === key.groupId)
  emit('select', { groupId: key.groupId, keyId: key.id, name: group?.name })
}
function selectGroup(value: string | number | boolean | null) {
  const group = props.groups.find(item => item.id === value)
  if (props.localKeyId || !group) return
  emit('select', { groupId: group.id, keyId: null, name: group.name })
}
function validate() {
  if (!props.localKeyId) return true
  if (loading.value || !ready.value) { error.value = t('intelligenceMonitor.local.keysFailed'); return false }
  const key = eligible.value.find(item => item.id === props.localKeyId && item.groupId === props.groupId)
  if (!key) { error.value = t('intelligenceMonitor.local.keyUnavailable'); return false }
  return true
}
defineExpose({ validate })
onMounted(() => { void load() })
onBeforeUnmount(() => controller?.abort())
</script>
