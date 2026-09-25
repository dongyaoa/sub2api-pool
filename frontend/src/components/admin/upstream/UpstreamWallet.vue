<template>
  <div class="rounded-xl bg-gray-50 p-3.5 dark:bg-dark-900/60">
    <template v-if="wallets.length">
      <div v-for="(wallet, index) in wallets" :key="`${wallet.target_id}-${wallet.wallet_ref}`" :class="index && 'mt-3 border-t border-gray-200/60 pt-3 dark:border-dark-700'">
        <div class="flex items-center justify-between gap-3"><div class="min-w-0"><p class="text-[11px] text-gray-500 dark:text-dark-400">{{ t(wallet.kind === 'key_quota' ? 'upstreamCenter.wallet.quota' : wallet.kind === 'subscription' ? 'upstreamCenter.wallet.subscription' : 'upstreamCenter.wallet.title') }}<span v-if="wallets.length > 1" class="ml-1">· {{ wallet.wallet_ref }}</span></p><strong class="mt-1 block text-xl font-semibold tabular-nums tracking-tight" :class="isLowBalance(wallet) ? 'text-rose-600 dark:text-rose-400' : 'text-gray-900 dark:text-gray-100'" data-testid="wallet-balance">{{ money(walletAmount(wallet), wallet.currency) }}</strong></div><button v-if="syncable" type="button" class="rounded-lg p-2 text-gray-400 hover:bg-white hover:text-primary-600 disabled:opacity-50 dark:hover:bg-dark-800" :disabled="busy" :aria-label="t('upstreamCenter.syncBalance')" :title="t('upstreamCenter.syncBalance')" @click="emit('sync', wallet.target_id)"><Icon name="refresh" size="sm" :class="busy && 'animate-spin'" /></button></div>
        <p v-if="wallet.status !== 'ok'" class="mt-1.5 text-[11px] leading-4" :class="wallet.status === 'error' ? 'text-amber-600 dark:text-amber-400' : 'text-gray-400 dark:text-dark-400'" :title="wallet.error">{{ t(wallet.status === 'unsupported' ? 'upstreamCenter.wallet.unsupported' : wallet.status === 'error' ? 'upstreamCenter.wallet.error' : 'upstreamCenter.wallet.pending') }}</p>
        <p v-if="wallet.status === 'error' && wallet.error" class="mt-1 break-words text-[11px] leading-4 text-amber-600/90 dark:text-amber-400">{{ wallet.error }}</p>
        <p v-if="wallet.synced_at" class="mt-1 text-[10px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.wallet.syncedAt', { time: dateTime(wallet.synced_at) }) }}</p>
        <p v-if="wallet.status === 'error' && wallet.last_attempt_at" class="mt-1 text-[10px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.wallet.attemptAt', { time: dateTime(wallet.last_attempt_at) }) }}</p>
      </div>
    </template>
    <div v-else class="flex items-center justify-between gap-2"><div><p class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.wallet.title') }}</p><p class="mt-1 text-sm text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.wallet.unknown') }}</p></div><button v-if="syncable && targetId" type="button" class="rounded-lg p-2 text-gray-400 hover:text-primary-600 disabled:opacity-50" :disabled="busy" :aria-label="t('upstreamCenter.syncBalance')" :title="t('upstreamCenter.syncBalance')" @click="emit('sync', targetId)"><Icon name="refresh" size="sm" :class="busy && 'animate-spin'" /></button></div>
    <div v-if="showKeyUsage" class="mt-3 border-t border-gray-200/60 pt-3 dark:border-dark-700" data-testid="key-upstream-usage">
      <dl class="grid grid-cols-2 gap-3"><div><dt class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.wallet.todayUsed') }}</dt><dd class="mt-1 font-semibold tabular-nums text-gray-900 dark:text-gray-100" data-testid="key-upstream-today">{{ money(keyWallet?.today_used, keyWallet?.currency) }}</dd></div><div><dt class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('upstreamCenter.wallet.totalUsed') }}</dt><dd class="mt-1 font-semibold tabular-nums text-gray-900 dark:text-gray-100" data-testid="key-upstream-total">{{ money(keyWallet?.total_used, keyWallet?.currency) }}</dd></div></dl>
      <p class="mt-2 text-[10px] leading-5 text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.wallet.usageHint') }}</p>
      <div class="mt-2 flex flex-wrap items-center gap-2"><UpstreamRateBadge :billing="keyWallet?.billing" /><span class="text-[10px] text-gray-400 dark:text-dark-400">{{ t('upstreamCenter.billing.scope') }}</span></div>
      <details v-if="keyWallet?.billing" class="mt-2 text-[11px] text-gray-500 dark:text-dark-400"><summary class="cursor-pointer">{{ t('upstreamCenter.billing.effective') }}</summary><dl class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4"><div><dt>{{ t('upstreamCenter.billing.group') }}</dt><dd class="mt-1 text-gray-800 dark:text-gray-200">{{ multiplier(keyWallet.billing.group_rate_multiplier) }}</dd></div><div><dt>{{ t('upstreamCenter.billing.user') }}</dt><dd class="mt-1 text-gray-800 dark:text-gray-200">{{ multiplier(keyWallet.billing.user_rate_multiplier) }}</dd></div><div><dt>{{ t('upstreamCenter.billing.base') }}</dt><dd class="mt-1 text-gray-800 dark:text-gray-200">{{ multiplier(keyWallet.billing.resolved_rate_multiplier) }}</dd></div><div><dt>{{ t('upstreamCenter.billing.updated') }}</dt><dd class="mt-1 text-gray-800 dark:text-gray-200">{{ dateTime(keyWallet.billing.synced_at) }}</dd></div></dl><p v-if="keyWallet.billing.error" class="mt-2 text-amber-600 dark:text-amber-400">{{ keyWallet.billing.error }}</p></details>
    </div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import UpstreamRateBadge from './UpstreamRateBadge.vue'
import type { UpstreamBalanceSnapshot } from '@/api/admin/upstreamCenter'
import { dateTime, money } from './format'
const props = withDefaults(defineProps<{ wallets: UpstreamBalanceSnapshot[]; syncable?: boolean; busy?: boolean; targetId?: number; showKeyUsage?: boolean }>(), { syncable: true, busy: false, showKeyUsage: false })
const keyWallet = computed(() => props.targetId ? props.wallets.find(wallet => wallet.target_id === props.targetId) : props.wallets.length === 1 ? props.wallets[0] : undefined)
const emit = defineEmits<{ sync: [id: number] }>()
const { t } = useI18n()
const multiplier = (value: number | null) => value == null ? '—' : `${value}×`
const walletAmount = (wallet: UpstreamBalanceSnapshot) => wallet.kind === 'wallet' ? wallet.balance : wallet.quota_remaining ?? wallet.balance
const isLowBalance = (wallet: UpstreamBalanceSnapshot) => {
  const amount = walletAmount(wallet)
  return typeof amount === 'number' && Number.isFinite(amount) && amount < 5
}
</script>
