<template>
  <AppLayout>
    <div class="mx-auto w-full min-w-0 max-w-[1600px] space-y-4 pb-6">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 dark:border-dark-700">
        <div class="flex min-w-0 gap-3 overflow-x-auto sm:gap-5" role="tablist" :aria-label="t('upstreamCenter.title')"><button v-for="item in tabs" :id="`upstream-tab-${item}`" :key="item" type="button" role="tab" :aria-selected="tab === item" aria-controls="upstream-panel" class="relative flex shrink-0 items-center gap-1.5 border-b-2 pb-3 pt-1 text-sm transition-colors" :class="tab === item ? 'border-primary-600 font-semibold text-primary-700 dark:border-primary-400 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'" @click="tab = item"><Icon :name="item === 'suppliers' ? 'server' : item === 'monitors' ? 'chart' : item === 'oauth' ? 'shield' : 'lightbulb'" size="sm" />{{ item === 'oauth' ? t('intelligenceMonitor.oauth.title') : t(`upstreamCenter.tabs.${item}`) }}<span v-if="item === 'suppliers' || item === 'monitors'" class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-normal text-gray-500 dark:bg-dark-800 dark:text-dark-400">{{ item === 'suppliers' ? overview?.suppliers.length || 0 : overview?.monitors.length || 0 }}</span></button></div>
        <button v-if="tab === 'suppliers' || tab === 'monitors'" type="button" class="btn btn-primary btn-sm mb-2" @click="tab === 'suppliers' ? openSupplier() : openTarget()"><Icon name="plus" size="sm" class="mr-1.5" />{{ t(tab === 'suppliers' ? 'upstreamCenter.addSupplier' : 'upstreamCenter.addMonitor') }}</button>
      </div>
      <div v-if="error" role="alert" class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-sm text-rose-600 dark:border-rose-900 dark:bg-rose-500/10 dark:text-rose-400"><span>{{ error }}</span><button type="button" class="font-medium underline" @click="reload()">{{ t('upstreamCenter.retry') }}</button></div>
      <div id="upstream-panel" role="tabpanel" :aria-labelledby="`upstream-tab-${tab}`" :aria-busy="loading" class="space-y-4">
        <IntelligenceMonitorPanel v-if="tab === 'intelligence' || tab === 'oauth'" :key="tab" :oauth-only="tab === 'oauth'" :overview="overview" />
        <template v-else>
          <section v-if="overview" class="grid grid-cols-2 gap-3 lg:grid-cols-4" :aria-label="t(tab === 'suppliers' ? 'upstreamCenter.finance.title' : 'upstreamCenter.tabs.monitors')">
            <div v-for="metric in tab === 'suppliers' ? supplierMetrics : monitorMetrics" :key="metric.key" class="card flex min-w-0 items-center gap-3 px-3 py-3 sm:px-4"><div class="shrink-0 rounded-lg bg-primary-50 p-2 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400"><Icon :name="metric.icon" size="md" :stroke-width="2" /></div><div class="min-w-0"><p class="text-[11px] font-medium text-gray-500 dark:text-dark-400">{{ t(metric.key) }}</p><p class="mt-0.5 truncate text-xl font-bold tabular-nums text-gray-900 dark:text-white" :class="metric.color">{{ metric.value }}</p><p v-if="metric.note" class="mt-0.5 truncate text-[10px] text-gray-400 dark:text-dark-400">{{ metric.note }}</p></div></div>
          </section>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="relative w-full sm:max-w-[280px]"><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" /><input v-model="search" class="input !py-2 !pl-9 !text-xs" :aria-label="t('upstreamCenter.search')" :placeholder="t('upstreamCenter.search')" /></div>
            <div class="flex w-full flex-wrap items-center justify-between gap-3 sm:w-auto">
              <div class="flex items-center gap-0.5 rounded-lg bg-gray-100 p-0.5 dark:bg-dark-800" role="group" :aria-label="t('upstreamCenter.range')"><button v-for="value in windows" :key="value" type="button" class="rounded-md px-2.5 py-1.5 text-[11px] transition-colors" :class="window === value ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-700 dark:text-gray-100' : 'text-gray-500 dark:text-dark-400'" :aria-pressed="window === value" @click="window = value">{{ t(`upstreamCenter.ranges.${value}`) }}</button></div>
              <div class="flex items-center gap-2 text-[10px] text-gray-400 dark:text-dark-400">
                <span class="hidden xl:inline" :title="t('upstreamCenter.refreshHint')">{{ updatedAt ? t('upstreamCenter.updated', { time: shortTime(updatedAt) }) : t('upstreamCenter.refreshHint') }}</span>
                <button type="button" class="btn btn-secondary btn-sm" data-testid="upstream-order" :disabled="!canOrder" @click="openOrder()"><Icon name="menu" size="sm" class="mr-1.5" />{{ t('upstreamCenter.order.open') }}</button>
                <button type="button" class="flex items-center gap-1.5 rounded-lg px-2 py-2 text-xs text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:opacity-50 dark:text-dark-400 dark:hover:bg-dark-800" :disabled="loading" @click="reload()"><Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />{{ t('upstreamCenter.refresh') }}</button>
              </div>
            </div>
          </div>
          <div v-if="!overview && loading" class="space-y-3"><div v-for="n in 2" :key="n" class="card grid animate-pulse gap-6 p-5 lg:grid-cols-[220px_1fr]"><div class="h-28 rounded-lg bg-gray-100 dark:bg-dark-700"></div><div class="space-y-3"><div class="h-12 rounded-lg bg-gray-100 dark:bg-dark-700"></div><div class="h-12 rounded-lg bg-gray-100 dark:bg-dark-700"></div></div></div></div>
          <template v-else-if="overview">
            <div v-if="tab === 'suppliers' && filteredSuppliers.length" class="space-y-4"><UpstreamSupplierCard v-for="supplier in filteredSuppliers" :key="supplier.id" :supplier="supplier" :busy-ids="busyIds" :running-ids="runningIds" @edit="openSupplier" @delete="confirmSupplierDelete" @add-target="supplier => openTarget(null, supplier)" @order-groups="openGroupOrder" @edit-target="item => openTarget(item)" @delete-target="confirmTargetDelete" @target-details="showTargetDetails" @finance="showSupplierDetails" @run="runTarget" @toggle="toggleTarget" @sync="syncBalance" /></div>
            <div v-else-if="tab === 'monitors' && filteredMonitors.length" class="grid items-start gap-4 lg:grid-cols-2 2xl:grid-cols-3"><UpstreamTargetCard v-for="target in filteredMonitors" :key="target.id" :target="target" :busy="busyIds.has(target.id)" :running="runningIds.has(target.id)" @edit="item => openTarget(item)" @delete="confirmTargetDelete" @details="showTargetDetails" @run="runTarget" @toggle="toggleTarget" /></div>
            <EmptyState v-else-if="search" class="card py-12" :title="t('upstreamCenter.noMatches')" />
            <EmptyState v-else class="card py-12" :title="t(tab === 'suppliers' ? 'upstreamCenter.emptySuppliers' : 'upstreamCenter.emptyMonitors')" :description="t(tab === 'suppliers' ? 'upstreamCenter.emptySuppliersHint' : 'upstreamCenter.emptyMonitorsHint')" :action-text="t(tab === 'suppliers' ? 'upstreamCenter.addSupplier' : 'upstreamCenter.addMonitor')" @action="tab === 'suppliers' ? openSupplier() : openTarget()"><template #icon><Icon :name="tab === 'suppliers' ? 'server' : 'chart'" size="xl" class="text-primary-500" /></template></EmptyState>
          </template>
          <p v-if="overview && tab === 'suppliers'" class="text-[10px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.finance.note') }}<span class="ml-1">{{ t('upstreamCenter.finance.accountingDate', { from: dateTime(overview.summary.from), to: dateTime(overview.summary.to) }) }}</span></p>
          <p v-else-if="overview" class="text-[10px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.latencyHint') }}</p>
        </template>
      </div>
    </div>
    <UpstreamOrderDialog :show="!!ordering" :scope="ordering?.scope || 'suppliers'" :supplier-id="ordering?.supplierId" :supplier-name="ordering?.supplierName" @close="ordering = null" @saved="orderSaved" />
    <UpstreamSupplierDialog :show="supplierDialog" :supplier="editingSupplier" @close="supplierDialog = false" @saved="saved" @changed="reload()" />
    <UpstreamTargetDialog :show="targetDialog" :target="editingTarget" :supplier="targetSupplier" @close="targetDialog = false" @saved="saved" />
    <UpstreamDetailDialog :show="detailDialog" :target="detailTarget" :supplier="detailSupplier" :model="detailModel" :record="detailRecord" :window="window" :busy="!!detailTarget && busyIds.has(detailTarget.id)" @close="detailDialog = false" @run="runTarget" @sync="syncBalance" @window-change="window = $event" />
    <BaseDialog :show="!!deleteItem" :title="deleteItem?.type === 'supplier' ? t('upstreamCenter.deleteSupplierTitle') : t('upstreamCenter.deleteTargetTitle')" width="narrow" :show-close-button="!deleting" :close-on-escape="!deleting" @close="!deleting && (deleteItem = null)"><p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ deleteItem ? t(deleteItem.type === 'supplier' ? 'upstreamCenter.deleteSupplierMessage' : 'upstreamCenter.deleteTargetMessage', { name: deleteItem.name }) : '' }}</p><template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="deleting" @click="deleteItem = null">{{ t('common.cancel') }}</button><button type="button" class="btn bg-rose-600 text-white hover:bg-rose-700 disabled:opacity-50" :disabled="deleting" @click="deleteConfirmed"><Icon v-if="deleting" name="refresh" size="sm" class="mr-2 animate-spin" />{{ t('upstreamCenter.remove') }}</button></div></template></BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import IntelligenceMonitorPanel from '@/components/admin/upstream/IntelligenceMonitorPanel.vue'
import UpstreamSupplierCard from '@/components/admin/upstream/UpstreamSupplierCard.vue'
import UpstreamTargetCard from '@/components/admin/upstream/UpstreamTargetCard.vue'
import UpstreamSupplierDialog from '@/components/admin/upstream/UpstreamSupplierDialog.vue'
import UpstreamTargetDialog from '@/components/admin/upstream/UpstreamTargetDialog.vue'
import UpstreamDetailDialog from '@/components/admin/upstream/UpstreamDetailDialog.vue'
import UpstreamOrderDialog from '@/components/admin/upstream/UpstreamOrderDialog.vue'
import { upstreamCenterAPI, type UpstreamHistoryRecord, type UpstreamOverview, type UpstreamSupplier, type UpstreamTarget, type UpstreamWindow } from '@/api/admin/upstreamCenter'
import { dateTime, money, shortTime, overallTargetStatus } from '@/components/admin/upstream/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'
const { t } = useI18n()
const app = useAppStore()
const tabs = ['suppliers', 'monitors', 'intelligence', 'oauth'] as const
const windows: UpstreamWindow[] = ['24h', '7d', '30d']
const tab = ref<typeof tabs[number]>('suppliers'), window = ref<UpstreamWindow>('24h'), search = ref('')
const overview = ref<UpstreamOverview | null>(null), loading = ref(false), error = ref(''), updatedAt = ref('')
const busyIds = ref(new Set<number>()), runningIds = ref(new Set<number>())
const allGroups = computed(() => overview.value?.suppliers.flatMap(supplier => supplier.targets) || [])
const allTargets = computed(() => [...allGroups.value, ...(overview.value?.monitors || [])])
const match = (value: string) => value.toLowerCase().includes(search.value.toLowerCase().trim())
const matchesTarget = (target: UpstreamTarget) => match(`${target.name} ${target.endpoint} ${target.models.join(' ')}`)
const filteredSuppliers = computed(() => overview.value?.suppliers.filter(supplier => match(`${supplier.name} ${supplier.website}`) || supplier.targets.some(matchesTarget)) || [])
const filteredMonitors = computed(() => overview.value?.monitors.filter(matchesTarget) || [])
const ordering = ref<{ scope: 'suppliers' | 'monitors' | 'groups'; supplierId?: number; supplierName?: string } | null>(null)
const canOrder = computed(() => ((tab.value === 'suppliers' ? overview.value?.suppliers.length : overview.value?.monitors.length) ?? 0) > 1)
function openOrder() {
  if (canOrder.value && (tab.value === 'suppliers' || tab.value === 'monitors')) ordering.value = { scope: tab.value }
}
function openGroupOrder(supplier: UpstreamSupplier) { ordering.value = { scope: 'groups', supplierId: supplier.id, supplierName: supplier.name } }
function orderSaved() { ordering.value = null; void reload() }
const supplierMetrics = computed(() => {
  const summary = overview.value?.summary
  return [
    { key: 'upstreamCenter.supplierCount', value: overview.value?.suppliers.length || 0, icon: 'server' as const, color: '', note: t('upstreamCenter.groupCount', { count: allGroups.value.length }) },
    { key: 'upstreamCenter.finance.todayCost', value: money(summary?.business_cost, summary?.currency), icon: 'creditCard' as const, color: '', note: t(`upstreamCenter.finance.${summary?.cost_source || 'unknown'}`) },
    { key: 'upstreamCenter.finance.todayRevenue', value: money(summary?.revenue, summary?.currency), icon: 'chart' as const, color: '', note: t('upstreamCenter.finance.requests', { count: summary?.request_count || 0 }) },
    { key: 'upstreamCenter.finance.todayProfit', value: summary?.profit == null ? t('upstreamCenter.finance.pending') : money(summary.profit, summary.currency), icon: 'chart' as const, color: summary?.profit == null ? '!text-sm !text-amber-600 dark:!text-amber-400' : summary.profit < 0 ? '!text-rose-600 dark:!text-rose-400' : '!text-primary-700 dark:!text-primary-300', note: `${t('upstreamCenter.finance.monitorCost')} ${money(summary?.monitor_cost, summary?.currency)}` },
  ]
})
const monitorMetrics = computed(() => {
  const monitors = overview.value?.monitors || []
  return [
    { key: 'upstreamCenter.monitorCount', value: monitors.length, icon: 'chart' as const, note: '', color: '' },
    { key: 'upstreamCenter.status.operational', value: monitors.filter(item => overallTargetStatus(item) === 'operational').length, icon: 'checkCircle' as const, note: '', color: '!text-emerald-600 dark:!text-emerald-400' },
    { key: 'upstreamCenter.status.degraded', value: monitors.filter(item => ['degraded', 'stale'].includes(overallTargetStatus(item))).length, icon: 'exclamationCircle' as const, note: '', color: '!text-amber-600 dark:!text-amber-400' },
    { key: 'upstreamCenter.status.error', value: monitors.filter(item => ['failed', 'error'].includes(overallTargetStatus(item))).length, icon: 'xCircle' as const, note: '', color: '!text-rose-600 dark:!text-rose-400' },
  ]
})
let controller: AbortController | undefined
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false
async function reload(silent = false) {
  if (silent && (loading.value || ordering.value)) return
  controller?.abort(); const current = new AbortController(); controller = current
  loading.value = true
  try {
    const result = await upstreamCenterAPI.overview(window.value, current.signal)
    if (!current.signal.aborted) { overview.value = result; updatedAt.value = new Date().toISOString(); error.value = '' }
  } catch (err) { if (!current.signal.aborted && !silent) error.value = extractApiErrorMessage(err, t('upstreamCenter.loadFailed')) }
  finally { if (!current.signal.aborted) loading.value = false }
}
watch(window, () => void reload())
watch(tab, () => { search.value = '' })
const supplierDialog = ref(false), targetDialog = ref(false), editingSupplier = ref<UpstreamSupplier | null>(null), editingTarget = ref<UpstreamTarget | null>(null), targetSupplier = ref<UpstreamSupplier | null>(null)
function openSupplier(supplier: UpstreamSupplier | null = null) { editingSupplier.value = supplier; supplierDialog.value = true }
function openTarget(target: UpstreamTarget | null = null, supplier: UpstreamSupplier | null = null) { editingTarget.value = target; targetSupplier.value = supplier || overview.value?.suppliers.find(item => item.id === target?.supplier_id) || null; targetDialog.value = true }
function saved() { app.showSuccess(t('upstreamCenter.saved')); void reload() }
const detailDialog = ref(false), detailTargetId = ref<number | null>(null), detailSupplierId = ref<number | null>(null), detailModel = ref(''), detailRecord = ref<UpstreamHistoryRecord | null>(null)
const detailTarget = computed(() => allTargets.value.find(item => item.id === detailTargetId.value) || null)
const detailSupplier = computed(() => overview.value?.suppliers.find(item => item.id === detailSupplierId.value) || null)
function showTargetDetails(target: UpstreamTarget, model: string, record?: UpstreamHistoryRecord) { detailTargetId.value = target.id; detailSupplierId.value = target.supplier_id; detailModel.value = model; detailRecord.value = record || null; detailDialog.value = true }
function showSupplierDetails(supplier: UpstreamSupplier) { detailTargetId.value = null; detailSupplierId.value = supplier.id; detailModel.value = ''; detailRecord.value = null; detailDialog.value = true }
async function targetAction(id: number, action: () => Promise<void>) {
  if (busyIds.value.has(id)) return
  busyIds.value = new Set([...busyIds.value, id])
  try { await action(); if (!disposed) await reload() }
  catch (err) { app.showError(extractApiErrorMessage(err, t('upstreamCenter.saveFailed'))) }
  finally { const ids = new Set(busyIds.value); ids.delete(id); busyIds.value = ids }
}
function runTarget(target: UpstreamTarget) {
  if (busyIds.value.has(target.id)) return
  void targetAction(target.id, async () => {
    runningIds.value = new Set([...runningIds.value, target.id])
    try { await upstreamCenterAPI.run(target.id); app.showSuccess(t('upstreamCenter.runComplete')) }
    finally { const ids = new Set(runningIds.value); ids.delete(target.id); runningIds.value = ids }
  })
}
function toggleTarget(target: UpstreamTarget) { void targetAction(target.id, async () => { await upstreamCenterAPI.updateTarget(target.id, { enabled: !target.enabled }) }) }
function syncBalance(id: number) { void targetAction(id, async () => { const balance = await upstreamCenterAPI.syncBalance(id); if (balance.status === 'ok') app.showSuccess(t('upstreamCenter.balanceSynced')); else if (balance.status === 'unsupported') app.showInfo(t('upstreamCenter.wallet.unsupported')); else app.showError(balance.error || t('upstreamCenter.balanceFailed')) }) }
const deleteItem = ref<{ type: 'supplier' | 'target'; id: number; name: string } | null>(null)
const deleting = ref(false)
function confirmSupplierDelete(supplier: UpstreamSupplier) { deleteItem.value = { type: 'supplier', id: supplier.id, name: supplier.name } }
function confirmTargetDelete(target: UpstreamTarget) { deleteItem.value = { type: 'target', id: target.id, name: target.name } }
async function deleteConfirmed() {
  if (!deleteItem.value || deleting.value) return
  const item = deleteItem.value; deleting.value = true
  try {
    if (item.type === 'supplier') await upstreamCenterAPI.deleteSupplier(item.id)
    else await upstreamCenterAPI.deleteTarget(item.id)
    deleteItem.value = null; app.showSuccess(t('upstreamCenter.deleted')); await reload()
  } catch (err) { app.showError(extractApiErrorMessage(err, t('upstreamCenter.saveFailed'))) }
  finally { deleting.value = false }
}
function visibilityChanged() { if (!document.hidden) void reload(true) }
onMounted(() => { void reload(); timer = setInterval(() => { if (!document.hidden) void reload(true) }, 30000); document.addEventListener('visibilitychange', visibilityChanged) })
onBeforeUnmount(() => { disposed = true; controller?.abort(); clearInterval(timer); document.removeEventListener('visibilitychange', visibilityChanged) })
</script>
