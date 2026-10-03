<template>
  <div class="space-y-5">
    <div v-if="filteredAmounts.length > 0">
      <label class="mb-2 block text-sm font-semibold text-gray-800 dark:text-gray-200">
        {{ t('payment.quickAmounts') }}
      </label>
      <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 xl:grid-cols-5" :class="showSecondLine && 'pt-2'">
        <button
          v-for="amt in filteredAmounts"
          :key="amt"
          type="button"
          :class="[
            'group relative flex min-h-[58px] flex-col items-center justify-center rounded-xl border px-2.5 py-2 text-center',
            modelValue === amt
              ? 'border-amber-300 bg-amber-50/30 text-gray-950 shadow-md shadow-amber-500/10 ring-1 ring-amber-200/70 dark:border-amber-300/70 dark:bg-amber-950/15 dark:text-white'
              : 'border-gray-200 bg-white text-gray-800 shadow-sm dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100',
          ]"
          :data-testid="`quick-amount-${amt}`"
          @click="selectAmount(amt)"
        >
          <span class="inline-flex items-baseline justify-center leading-none">
            <span
              :class="[
                'mr-1 text-xs font-black sm:text-sm',
                modelValue === amt ? 'text-primary-600 dark:text-primary-200' : 'text-primary-500 dark:text-primary-300',
              ]"
            >{{ amountSymbol }}</span>
            <span class="text-base font-black tracking-tight sm:text-lg">{{ formatQuickAmountNumber(amt) }}</span>
          </span>
          <!-- 促销价签（单行）：仅命中档位的金额显示；红底白字、内圈点线、右侧圆孔、整体旋转 -->
          <span
            v-if="quoteFor(amt).percent > 0"
            class="pointer-events-none absolute -right-2 -top-3 z-10 rotate-12"
            data-testid="quick-amount-bonus-badge"
          >
            <span
              class="relative flex items-center gap-1 whitespace-nowrap rounded bg-red-600 py-0.5 pl-1.5 pr-1 text-[11px] font-extrabold leading-tight tracking-tight text-white shadow-md ring-2 ring-white before:pointer-events-none before:absolute before:inset-[2px] before:rounded-sm before:border before:border-dotted before:border-white/70 dark:bg-red-500 dark:ring-dark-800"
            >
              <span>{{ badgeText(amt) }}</span>
              <span class="h-1 w-1 shrink-0 rounded-full bg-white"></span>
            </span>
          </span>
          <!-- 配置了优惠阶梯时，所有按钮都显示第二行，保持高度一致：赠金显示到账 USD，折扣显示折后实付 -->
          <span
            v-if="showSecondLine"
            :class="[
              'mt-0.5 block text-[11px] font-normal leading-tight',
              quoteFor(amt).percent > 0 ? 'text-red-600 dark:text-red-300' : 'text-gray-400 dark:text-gray-500',
            ]"
            data-testid="quick-amount-credited"
          >{{ secondLine(amt) }}</span>
        </button>
      </div>
    </div>

    <div>
      <label class="mb-2 block text-sm font-semibold text-gray-800 dark:text-gray-200">
        {{ t('payment.customAmount') }}
      </label>
      <div class="relative rounded-xl border border-gray-200 bg-white shadow-sm transition-all focus-within:border-primary-500 focus-within:shadow-lg focus-within:shadow-primary-500/10 focus-within:ring-2 focus-within:ring-primary-500/15 dark:border-dark-600 dark:bg-dark-800">
        <span class="absolute left-4 top-1/2 inline-flex -translate-y-1/2 items-baseline gap-2 text-xs font-bold uppercase tracking-wide text-gray-400 dark:text-dark-400">
          <span class="text-base font-black text-primary-500 dark:text-primary-300">{{ amountSymbol }}</span>
          <span>{{ normalizedCurrency }}</span>
        </span>
        <input
          type="text"
          inputmode="decimal"
          :value="customText"
          :placeholder="placeholderText"
          class="w-full bg-transparent py-3.5 pl-24 pr-4 text-lg font-semibold text-gray-950 outline-none placeholder:text-sm placeholder:font-normal placeholder:text-gray-400 dark:text-white dark:placeholder:text-dark-400"
          @input="handleInput"
        />
      </div>
      <p v-if="helpText" class="mt-3 rounded-xl border border-primary-100 bg-primary-50/70 px-3 py-2 text-xs font-medium leading-relaxed text-primary-700 dark:border-primary-900/40 dark:bg-primary-950/30 dark:text-primary-200">
        {{ helpText }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { currencySymbol, formatPaymentAmount, normalizePaymentCurrency } from './currency'
import type { RechargeBonusTier } from '@/types/payment'
import { formatRechargeBonusNumber, quoteRechargeBonus, type RechargeBonusMode } from '@/utils/rechargeBonus'

const props = withDefaults(defineProps<{
  amounts?: number[]
  modelValue: number | null
  min?: number
  max?: number
  helpText?: string
  /** 充值优惠阶梯（按 min_amount 升序）；为空时不显示价签与第二行 */
  bonusTiers?: RechargeBonusTier[]
  /** 阶梯模式：bonus 赠金 / discount 折扣 */
  bonusMode?: RechargeBonusMode
  /** 充值倍率（1 支付币种 = multiplier USD），用于计算到账金额 */
  multiplier?: number
  /** 支付币种（折扣模式第二行实付金额的币种与精度） */
  currency?: string
}>(), {
  amounts: () => [10, 20, 50, 100, 200, 500, 1000, 2000, 5000],
  min: 0,
  max: 0,
  helpText: '',
  bonusTiers: () => [],
  bonusMode: 'bonus',
  multiplier: 1,
  currency: undefined,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t } = useI18n()

const customText = ref('')
const normalizedCurrency = computed(() => normalizePaymentCurrency(props.currency))
const amountSymbol = computed(() => currencySymbol(normalizedCurrency.value))

// 0 = no limit
const filteredAmounts = computed(() =>
  props.amounts.filter((a) => (props.min <= 0 || a >= props.min) && (props.max <= 0 || a <= props.max))
)

const showSecondLine = computed(() => props.bonusTiers.length > 0)

function currencyDigits(): number {
  if (!props.currency) return 2
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency: props.currency }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function quoteFor(amt: number) {
  return quoteRechargeBonus(props.bonusTiers, amt, {
    multiplier: props.multiplier,
    mode: props.bonusMode,
    currencyDigits: currencyDigits(),
  })
}

// 价签文案：赠金「+20%」，折扣「20% OFF」
function badgeText(amt: number): string {
  const percent = formatRechargeBonusNumber(quoteFor(amt).percent)
  return props.bonusMode === 'discount' ? `${percent}% OFF` : `+${percent}%`
}

function secondLine(amt: number): string {
  const quote = quoteFor(amt)
  if (props.bonusMode === 'discount') {
    return t('payment.rechargeBonus.payShort', { amount: formatPaymentAmount(quote.payBase, props.currency) })
  }
  return t('payment.rechargeBonus.creditedShort', { amount: '$' + quote.credited.toFixed(2) })
}

const placeholderText = computed(() => {
  if (props.min > 0 && props.max > 0) return `${formatAmount(props.min)} - ${formatAmount(props.max)}`
  if (props.min > 0) return `>= ${formatAmount(props.min)}`
  if (props.max > 0) return `<= ${formatAmount(props.max)}`
  return t('payment.enterAmount')
})

const AMOUNT_PATTERN = /^\d*(\.\d{0,2})?$/

function formatAmount(value: number): string {
  return formatPaymentAmount(value, normalizedCurrency.value)
}

function formatQuickAmountNumber(value: number): string {
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 0,
  }).format(Number.isFinite(value) ? Math.round(value) : 0)
}


function selectAmount(amt: number) {
  customText.value = String(amt)
  emit('update:modelValue', amt)
}

function handleInput(e: Event) {
  const input = e.target as HTMLInputElement
  const val = input.value
  if (!AMOUNT_PATTERN.test(val)) {
    input.value = customText.value
    return
  }
  customText.value = val
  if (val === '') {
    emit('update:modelValue', null)
    return
  }
  const num = parseFloat(val)
  if (!isNaN(num) && num > 0) {
    emit('update:modelValue', num)
  } else {
    emit('update:modelValue', null)
  }
}

watch(() => props.modelValue, (v) => {
  if (v !== null && String(v) !== customText.value) {
    customText.value = String(v)
  }
}, { immediate: true })
</script>
