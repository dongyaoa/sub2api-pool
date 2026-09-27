<template>
  <section v-if="run.source_type === 'local_group'" class="mt-4 rounded-xl border border-gray-100 bg-gray-50/50 p-3 dark:border-dark-700 dark:bg-dark-900/30" data-testid="execution-source">
    <div class="mb-2 flex items-center gap-2"><Icon name="server" size="sm" class="text-primary-500"/><h4 class="text-[11px] font-semibold text-gray-700 dark:text-gray-200">{{ t('intelligenceMonitor.execution.title') }}</h4><span v-if="recorded" class="ml-auto text-[10px]" :class="snapshot.execution_source_status === 'completed' ? 'text-emerald-600' : 'text-amber-600'">{{ t(`intelligenceMonitor.execution.${snapshot.execution_source_status === 'completed' ? 'completed' : 'attempted'}`) }}</span></div>
    <p v-if="!recorded" class="text-xs text-gray-400">{{ t('intelligenceMonitor.execution.notRecorded') }}</p>
    <dl v-else class="grid gap-x-5 gap-y-2 text-xs sm:grid-cols-2"><div v-for="item in details" :key="item.label" class="min-w-0"><dt class="text-[10px] text-gray-400">{{ t(`intelligenceMonitor.execution.${item.label}`) }}</dt><dd class="mt-0.5 break-words font-medium text-gray-700 dark:text-gray-200">{{ item.value }}</dd></div></dl>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntelligenceRun } from '@/api/admin/intelligenceMonitor'
const props = defineProps<{ run: IntelligenceRun }>()
const { t } = useI18n()
const snapshot = computed(() => props.run.source_snapshot || {})
const recorded = computed(() => Boolean(snapshot.value.execution_account_id))
const text = (value: unknown) => typeof value === 'string' || typeof value === 'number' ? String(value) : ''
const details = computed(() => {
  const data = snapshot.value
  const items = [{ label: data.execution_source_status === 'completed' ? 'account' : 'attemptedAccount', value: [text(data.execution_account_name), `#${text(data.execution_account_id)}`].filter(Boolean).join(' · ') }, { label: 'type', value: [text(data.execution_account_platform), text(data.execution_account_type)].filter(Boolean).join(' · ') || '—' }]
  if (data.execution_account_base_origin) items.push({ label: 'origin', value: text(data.execution_account_base_origin) })
  if (data.execution_attempt_count) items.push({ label: 'attempts', value: text(data.execution_attempt_count) })
  if (data.execution_binding_status === 'matched') {
    items.push({ label: 'supplier', value: text(data.execution_supplier_name) || '—' }, { label: 'target', value: text(data.execution_upstream_target_name) || '—' })
  } else {
    const status = ['unbound', 'ambiguous', 'unavailable'].includes(text(data.execution_binding_status)) ? text(data.execution_binding_status) : 'unknown'
    items.push({ label: 'supplier', value: t(`intelligenceMonitor.execution.${status}`) })
  }
  return items
})
</script>
