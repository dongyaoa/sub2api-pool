<template>
  <article class="card supplier-card grid min-w-0 overflow-hidden 2xl:grid-cols-[232px_minmax(0,1fr)]">
    <aside class="supplier-summary min-w-0 border-b border-gray-100 p-4 2xl:border-b-0 2xl:border-r dark:border-dark-700">
      <div class="supplier-identity min-w-0">
        <div class="flex items-center gap-2.5">
          <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-sm font-semibold text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">{{ supplier.name.slice(0, 1).toUpperCase() }}</div>
          <h2 class="min-w-0 flex-1 truncate text-sm font-semibold text-gray-900 dark:text-gray-100" :title="supplier.name">{{ supplier.name }}</h2>
          <button type="button" class="action" :title="t('upstreamCenter.editSupplier')" :aria-label="t('upstreamCenter.editSupplier')" @click="emit('edit', supplier)"><Icon name="edit" size="xs" /></button>
        </div>
        <div class="mt-2 flex min-w-0 items-center justify-between gap-2">
          <p class="min-w-0 truncate text-[11px] text-gray-400 dark:text-dark-400" :title="website || undefined">{{ website ? domain(website) : '—' }}</p>
          <a v-if="website" :href="website" target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer" class="inline-flex shrink-0 items-center gap-1 rounded-md bg-primary-50 px-1.5 py-1 text-[10px] font-medium text-primary-600 transition-colors hover:bg-primary-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:bg-primary-500/10 dark:text-primary-400 dark:hover:bg-primary-500/20" :aria-label="t('upstreamCenter.visitWebsite')" data-testid="supplier-website">{{ t('upstreamCenter.visitWebsite') }}<Icon name="externalLink" size="xs" /></a>
        </div>
      </div>
      <UpstreamWallet class="supplier-wallet !rounded-lg !p-3" :wallets="supplier.wallets || []" :target-id="supplier.targets[0]?.id" :syncable="!!supplier.targets.length" :busy="supplier.targets.some(target => busyIds.has(target.id))" @sync="id => emit('sync', id)" />
      <div class="supplier-finance min-w-0">
      <dl class="space-y-3 text-xs"><div class="flex items-center justify-between gap-2"><dt class="text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.finance.todayCost') }}</dt><dd class="font-semibold tabular-nums text-gray-800 dark:text-gray-100">{{ money(supplier.finance?.business_cost, supplier.finance?.currency) }}</dd></div><div class="flex items-center justify-between gap-2"><dt class="text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.finance.todayRevenue') }}</dt><dd class="font-semibold tabular-nums text-gray-800 dark:text-gray-100">{{ money(supplier.finance?.revenue, supplier.finance?.currency) }}</dd></div><div class="flex items-center justify-between gap-2"><dt class="text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.finance.todayProfit') }}</dt><dd class="font-semibold tabular-nums" :class="supplier.finance?.profit == null ? 'text-[11px] text-amber-600 dark:text-amber-400' : supplier.finance.profit < 0 ? 'text-rose-600 dark:text-rose-400' : 'text-primary-700 dark:text-primary-300'">{{ supplier.finance?.profit == null ? t('upstreamCenter.finance.pending') : money(supplier.finance.profit, supplier.finance.currency) }}</dd></div></dl>
      <div class="mt-3 flex items-center justify-between border-t border-gray-100 pt-2.5 dark:border-dark-700"><button type="button" class="inline-flex items-center gap-1 text-[11px] text-primary-600 dark:text-primary-400" @click="emit('finance', supplier)">{{ t('upstreamCenter.financeDetails') }}<Icon name="chevronRight" size="xs" /></button><button type="button" class="action hover:!text-rose-500" :title="t('upstreamCenter.remove')" :aria-label="t('upstreamCenter.remove')" @click="emit('delete', supplier)"><Icon name="trash" size="xs" /></button></div>
      </div>
    </aside>
    <div class="min-w-0">
      <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-4 py-2.5 sm:px-5 dark:border-dark-700"><div class="flex items-center gap-3"><span class="text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('upstreamCenter.groupCount', { count: supplier.targets.length }) }}</span><div class="hidden items-center gap-2.5 text-[10px] text-gray-400 sm:flex"><span v-for="status in ['operational', 'degraded', 'error']" :key="status" class="inline-flex items-center gap-1"><i class="h-2 w-1 rounded-full" :class="status === 'operational' ? 'bg-emerald-500' : status === 'degraded' ? 'bg-amber-400' : 'bg-rose-500'"></i>{{ t(`upstreamCenter.status.${status}`) }}</span></div></div><button type="button" class="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-500/10" @click="emit('add-target', supplier)"><Icon name="plus" size="xs" />{{ t('upstreamCenter.addGroup') }}</button></div>
      <div class="divide-y divide-gray-100 dark:divide-dark-700"><UpstreamGroupRow v-for="target in supplier.targets" :key="target.id" :target="target" :busy="busyIds.has(target.id)" :running="runningIds.has(target.id)" @details="(item, model, record) => emit('target-details', item, model, record)" @run="item => emit('run', item)" @toggle="item => emit('toggle', item)" @edit="item => emit('edit-target', item)" @delete="item => emit('delete-target', item)" /></div>
      <div v-if="!supplier.targets.length" class="flex min-h-[190px] flex-col items-center justify-center px-5 py-6 text-center"><Icon name="key" size="lg" class="mb-3 text-gray-300 dark:text-dark-500" /><p class="text-sm text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.noGroups') }}</p><p class="mt-1 text-xs text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.noGroupsHint') }}</p><button type="button" class="mt-3 text-xs font-medium text-primary-600 dark:text-primary-400" @click="emit('add-target', supplier)">{{ t('upstreamCenter.addGroup') }}</button></div>
    </div>
  </article>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UpstreamHistoryRecord, UpstreamSupplier, UpstreamTarget } from '@/api/admin/upstreamCenter'
import UpstreamWallet from './UpstreamWallet.vue'
import UpstreamGroupRow from './UpstreamGroupRow.vue'
import { domain, money } from './format'
import { safeWebsite } from './safeWebsite'
const props = defineProps<{ supplier: UpstreamSupplier; busyIds: Set<number>; runningIds: Set<number> }>()
const website = computed(() => safeWebsite(props.supplier.website))
const emit = defineEmits<{ edit: [supplier: UpstreamSupplier]; delete: [supplier: UpstreamSupplier]; finance: [supplier: UpstreamSupplier]; 'add-target': [supplier: UpstreamSupplier]; 'target-details': [target: UpstreamTarget, model: string, record?: UpstreamHistoryRecord]; run: [target: UpstreamTarget]; toggle: [target: UpstreamTarget]; 'edit-target': [target: UpstreamTarget]; 'delete-target': [target: UpstreamTarget]; sync: [targetId: number] }>()
const { t } = useI18n()
</script>
<style scoped>
.supplier-summary { display: grid; gap: 16px; align-content: start; }
.supplier-wallet { min-width: 0; }
.supplier-finance { align-self: end; }
.action { @apply flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-primary-600 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-primary-400; }
@media (min-width: 768px) and (max-width: 1535px) {
  .supplier-summary { grid-template-columns: minmax(0, .85fr) minmax(0, 1fr) minmax(0, 1fr); align-items: start; gap: 24px; }
}
@media (min-width: 1536px) {
  .supplier-summary { grid-template-rows: auto minmax(min-content, 1fr) auto; }
  .supplier-wallet { align-self: center; }
}
</style>
