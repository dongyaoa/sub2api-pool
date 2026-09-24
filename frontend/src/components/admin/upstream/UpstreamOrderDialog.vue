<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="normal"
    :close-on-escape="!saving"
    :show-close-button="!saving"
    @close="close"
  >
    <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('upstreamCenter.order.description') }}</p>
    <p class="mt-1.5 text-xs leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.order.newItemsHint') }}</p>

    <div v-if="loading" role="status" class="flex items-center justify-center gap-2 py-12 text-sm text-gray-500 dark:text-gray-400">
      <Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}
    </div>
    <div v-else-if="loadError" role="alert" class="mt-4 rounded-xl bg-red-50 p-4 dark:bg-red-500/10">
      <p class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
      <button type="button" data-reload-order class="btn btn-secondary btn-sm mt-3" @click="loadOrder">{{ t('upstreamCenter.order.reload') }}</button>
    </div>
    <template v-else-if="loaded">
      <div class="mb-2 mt-5 flex items-center justify-between gap-3 text-xs">
        <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('upstreamCenter.order.allItems', { count: items.length }) }}</span>
        <span class="text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.order.firstOnTop') }}</span>
      </div>
      <VueDraggable
        v-model="items"
        class="max-h-[min(52vh,480px)] space-y-2 overflow-y-auto rounded-xl p-0.5"
        handle=".upstream-order-handle"
        draggable=".upstream-order-item"
        :animation="180"
        :disabled="saving"
        ghost-class="upstream-order-ghost"
        :group="{ name: 'upstream-center-order', pull: false, put: false }"
        :aria-label="title"
      >
        <div
          v-for="(item, index) in items"
          :key="item.id"
          :data-order-item="item.id"
          class="upstream-order-item flex items-center gap-2 rounded-xl border border-gray-200/80 bg-white px-2 py-2.5 dark:border-dark-700 dark:bg-dark-800/70"
        >
          <span class="upstream-order-handle drag-handle" :title="t('upstreamCenter.order.drag')" aria-hidden="true"><Icon name="menu" size="sm" /></span>
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-xs font-semibold tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ index + 1 }}</span>
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <span class="truncate text-sm font-medium text-gray-800 dark:text-gray-100" :title="item.name">{{ item.name }}</span>
              <span v-if="item.paused" class="shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ t('upstreamCenter.status.paused') }}</span>
            </div>
            <p v-if="item.description" class="mt-0.5 truncate text-xs text-gray-400 dark:text-dark-400" :title="item.description">{{ item.description }}</p>
          </div>
          <div class="flex shrink-0 items-center gap-0.5">
            <button type="button" class="order-button" data-move-order="up" :disabled="saving || index === 0" :aria-label="`${t('upstreamCenter.order.moveUp')}: ${item.name}`" :title="t('upstreamCenter.order.moveUp')" @click="move(index, -1)"><Icon name="chevronUp" size="sm" /></button>
            <button type="button" class="order-button" data-move-order="down" :disabled="saving || index === items.length - 1" :aria-label="`${t('upstreamCenter.order.moveDown')}: ${item.name}`" :title="t('upstreamCenter.order.moveDown')" @click="move(index, 1)"><Icon name="chevronDown" size="sm" /></button>
          </div>
        </div>
      </VueDraggable>
      <p v-if="!items.length" class="py-10 text-center text-sm text-gray-400">{{ t('upstreamCenter.order.empty') }}</p>
    </template>
    <div v-if="saveError" role="alert" class="mt-4 rounded-xl bg-red-50 p-3 dark:bg-red-500/10">
      <p class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
      <button v-if="stale" type="button" data-reload-order class="btn btn-secondary btn-sm mt-3" :disabled="saving" @click="loadOrder">{{ t('upstreamCenter.order.reload') }}</button>
    </div>
    <template #footer>
      <button type="button" data-cancel-order class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
      <button type="button" data-save-order class="btn btn-primary" :disabled="!loaded || loading || saving || stale || items.length < 2" @click="save">{{ t(saving ? 'upstreamCenter.order.saving' : 'upstreamCenter.order.save') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { upstreamCenterAPI } from '@/api/admin/upstreamCenter'
import { intelligenceMonitorAPI } from '@/api/admin/intelligenceMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

type OrderScope = 'suppliers' | 'monitors' | 'groups' | 'intelligence' | 'oauth'
interface OrderItem { id: number; name: string; description: string; paused?: boolean }
const props = defineProps<{ show: boolean; scope: OrderScope; supplierId?: number; supplierName?: string }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const title = computed(() => props.scope === 'groups' && props.supplierName
  ? t('upstreamCenter.order.groupTitle', { name: props.supplierName })
  : t(`upstreamCenter.order.titles.${props.scope}`))
const items = ref<OrderItem[]>([])
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const stale = ref(false)
const loadError = ref('')
const saveError = ref('')
let controller: AbortController | undefined
let session = 0
let disposed = false

function reset() {
  session++
  controller?.abort()
  controller = undefined
  items.value = []
  loaded.value = false
  loading.value = false
  saving.value = false
  loadError.value = ''
  saveError.value = ''
  stale.value = false
}

async function loadOrder() {
  if (saving.value || !props.show) return
  reset()
  const current = new AbortController()
  const currentSession = session
  controller = current
  loading.value = true
  const scope = props.scope
  const supplierId = props.supplierId
  try {
    let result: OrderItem[]
    if (scope === 'intelligence' || scope === 'oauth') {
      const plans = await intelligenceMonitorAPI.plans(current.signal)
      if (!Array.isArray(plans.items)) throw new Error(t('upstreamCenter.order.loadError'))
      result = plans.items.filter(plan => (plan.source_type === 'openai_oauth') === (scope === 'oauth')).map(plan => ({
        id: plan.id, name: plan.name, paused: !plan.enabled,
        description: [t(`intelligenceMonitor.source.${plan.source_type}`), plan.endpoint || plan.source_name].filter(Boolean).join(' · '),
      }))
    } else {
      const overview = await upstreamCenterAPI.overview('24h', current.signal)
      if (scope === 'suppliers') {
        if (!Array.isArray(overview.suppliers)) throw new Error(t('upstreamCenter.order.loadError'))
        result = overview.suppliers.map(supplier => ({ id: supplier.id, name: supplier.name, description: supplier.website }))
      } else {
        const targets = scope === 'monitors' ? overview.monitors : overview.suppliers?.find(supplier => supplier.id === supplierId)?.targets
        if (!Array.isArray(targets)) throw new Error(t(scope === 'groups' ? 'upstreamCenter.order.supplierMissing' : 'upstreamCenter.order.loadError'))
        result = targets.map(target => ({ id: target.id, name: target.name, description: target.endpoint, paused: !target.enabled }))
      }
    }
    if (current.signal.aborted || disposed || currentSession !== session || !props.show) return
    if (result.some(item => !Number.isSafeInteger(item.id) || item.id <= 0) || new Set(result.map(item => item.id)).size !== result.length) throw new Error(t('upstreamCenter.order.loadError'))
    items.value = result
    loaded.value = true
  } catch (error) {
    if (!current.signal.aborted && !disposed && currentSession === session) loadError.value = extractApiErrorMessage(error, t('upstreamCenter.order.loadError'))
  } finally {
    if (controller === current) {
      controller = undefined
      loading.value = false
    }
  }
}

function move(index: number, offset: number) {
  const target = index + offset
  if (saving.value || index < 0 || index >= items.value.length || target < 0 || target >= items.value.length) return
  const [item] = items.value.splice(index, 1)
  items.value.splice(target, 0, item)
}

function isConflict(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const value = error as { status?: number; response?: { status?: number } }
  return value.status === 409 || value.response?.status === 409
}

async function save() {
  if (!loaded.value || loading.value || saving.value || stale.value || items.value.length < 2) return
  const scope = props.scope
  const supplierId = props.supplierId
  const currentSession = session
  const ids = items.value.map(item => item.id)
  saving.value = true
  saveError.value = ''
  try {
    if (scope === 'intelligence' || scope === 'oauth') await intelligenceMonitorAPI.reorder({ scope, ids })
    else if (scope === 'groups') {
      if (!Number.isSafeInteger(supplierId) || !supplierId || supplierId <= 0) throw new Error(t('upstreamCenter.order.supplierMissing'))
      await upstreamCenterAPI.reorder({ scope, supplier_id: supplierId, ids })
    } else await upstreamCenterAPI.reorder({ scope, ids })
    if (disposed || currentSession !== session || !props.show) return
    app.showSuccess(t('upstreamCenter.order.saved'))
    emit('saved')
    emit('close')
  } catch (error) {
    if (disposed || currentSession !== session || !props.show) return
    stale.value = isConflict(error)
    saveError.value = stale.value ? t('upstreamCenter.order.stale') : extractApiErrorMessage(error, t('upstreamCenter.order.saveError'))
  } finally {
    if (currentSession === session) saving.value = false
  }
}

function close() {
  if (saving.value) return
  reset()
  emit('close')
}

watch(() => [props.show, props.scope, props.supplierId] as const, () => {
  reset()
  if (props.show) void loadOrder()
}, { immediate: true })
onUnmounted(() => { disposed = true; reset() })
</script>

<style scoped>
.order-button {
  @apply flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400 disabled:cursor-not-allowed disabled:opacity-25 dark:text-gray-400 dark:hover:bg-dark-600;
}
.drag-handle {
  @apply flex h-8 w-4 shrink-0 cursor-grab touch-none items-center justify-center text-gray-400 active:cursor-grabbing;
}
.upstream-order-ghost { @apply opacity-30; }
</style>
