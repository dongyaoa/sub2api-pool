<template>
  <header class="glass sticky top-0 z-30 border-b border-gray-200/50 dark:border-dark-700/50">
    <div class="flex h-16 items-center justify-between gap-2 px-2 sm:px-4 md:px-6">
      <!-- Left: Mobile Menu Toggle + Page Title -->
      <div class="flex shrink-0 items-center gap-2 sm:gap-4">
        <button
          @click="toggleMobileSidebar"
          class="btn-ghost btn-icon lg:hidden"
          :aria-label="t('common.toggleMenu')"
        >
          <Icon name="menu" size="md" />
        </button>

        <div class="hidden lg:block">
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ pageTitle }}
          </h1>
          <p v-if="pageDescription" class="text-xs text-gray-500 dark:text-dark-400">
            {{ pageDescription }}
          </p>
        </div>
      </div>

      <!-- Right: Announcements + Docs + Language + Subscriptions + Balance + User Dropdown -->
      <div class="flex min-w-0 items-center gap-1 sm:gap-3">
        <!-- Customer support and group chat -->
        <button
          v-if="user"
          type="button"
          class="flex items-center gap-2 rounded-xl bg-sky-100 px-3 py-1.5 text-sm font-medium text-sky-700 transition-colors hover:bg-sky-200 dark:bg-sky-950/40 dark:text-sky-300 dark:hover:bg-sky-950/60"
          aria-label="客服群聊"
          title="客服群聊"
          @click="openSupportModal"
        >
          <Icon name="chat" size="sm" />
          <span class="hidden md:inline">客服群聊</span>
        </button>
        <!-- Announcement Bell -->
        <AnnouncementBell v-if="user" />

        <!-- Docs Link -->
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="hidden items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white sm:flex"
        >
          <Icon name="book" size="sm" />
          <span class="hidden sm:inline">{{ t('nav.docs') }}</span>
        </a>


        <!-- Language Switcher -->
        <LocaleSwitcher />

        <!-- Subscription Progress (for users with active subscriptions; not mounted at all when the feature is off) -->
        <SubscriptionProgressMini v-if="user && subscriptionFeatureEnabled" />

        <!-- Balance Display -->
        <div
          v-if="user"
          class="group relative hidden items-center gap-2 rounded-xl bg-primary-50 px-3 py-1.5 dark:bg-primary-900/20 sm:flex"
        >
          <svg
            class="h-4 w-4 text-primary-600 dark:text-primary-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
            />
          </svg>
          <span class="text-sm font-semibold text-primary-700 dark:text-primary-300">
            {{ formatHeaderMoney(availableBalance) }}
          </span>
          <span
            v-if="frozenBalance > 0"
            class="rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-200"
          >
            {{ balanceFrozenLabel }}
          </span>
          <div
            class="pointer-events-none absolute right-0 top-full mt-2 hidden w-56 rounded-lg border border-gray-200 bg-white p-3 text-xs shadow-lg group-hover:block dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex items-center justify-between">
              <span class="text-gray-500 dark:text-dark-400">{{ balanceAvailableText }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatHeaderMoney(availableBalance) }}</span>
            </div>
            <div class="mt-2 flex items-center justify-between">
              <span class="text-gray-500 dark:text-dark-400">{{ balanceFrozenText }}</span>
              <span class="font-medium text-amber-700 dark:text-amber-200">{{ formatHeaderMoney(frozenBalance) }}</span>
            </div>
            <div class="mt-2 border-t border-gray-100 pt-2 dark:border-dark-700">
              <div class="flex items-center justify-between">
                <span class="text-gray-500 dark:text-dark-400">{{ balanceTotalText }}</span>
                <span class="font-semibold text-gray-900 dark:text-white">{{ formatHeaderMoney(totalBalance) }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Recharge shortcut -->
        <button
          v-if="user && paymentEnabled"
          type="button"
          class="flex items-center gap-2 rounded-xl bg-emerald-50 px-3 py-1.5 text-sm font-medium text-emerald-700 transition-colors hover:bg-emerald-100 dark:bg-emerald-950/30 dark:text-emerald-300 dark:hover:bg-emerald-950/60"
          aria-label="充值"
          title="充值"
          @click="goToRecharge"
        >
          <Icon name="creditCard" size="sm" />
          <span class="hidden md:inline">充值</span>
        </button>
        <!-- User Dropdown -->
        <div v-if="user" class="relative" ref="dropdownRef">
          <button
            @click="toggleDropdown"
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
            :aria-label="t('common.userMenu')"
          >
            <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="hidden text-left md:block">
              <div class="text-sm font-medium text-gray-900 dark:text-white">
                {{ displayName }}
              </div>
              <div class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.users.roles.' + user.role) }}
              </div>
            </div>
            <Icon name="chevronDown" size="sm" class="hidden text-gray-400 md:block" />
          </button>

          <!-- Dropdown Menu -->
          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56">
              <!-- User Info -->
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ displayName }}
                </div>
                <div class="text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
              </div>

              <!-- Balance (mobile only) -->
              <div class="border-b border-gray-100 px-4 py-2 dark:border-dark-700 sm:hidden">
                <div class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('common.balance') }}
                </div>
                <div class="text-sm font-semibold text-primary-600 dark:text-primary-400">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-1 text-xs text-amber-600 dark:text-amber-300">
                  {{ balanceFrozenText }} {{ formatHeaderMoney(frozenBalance) }}
                </div>
              </div>

              <div class="py-1">
                <router-link to="/profile" @click="closeDropdown" class="dropdown-item">
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>

                <router-link to="/keys" @click="closeDropdown" class="dropdown-item">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>

                <a
                  v-if="authStore.isAdmin"
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  @click="closeDropdown"
                  class="dropdown-item"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('nav.github') }}
                </a>

              </div>

              <!-- Contact Support (only show if configured) -->
              <div
                v-if="contactInfo"
                class="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700"
              >
                <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <svg
                    class="h-3.5 w-3.5 flex-shrink-0"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 01-.825-.242m9.345-8.334a2.126 2.126 0 00-.476-.095 48.64 48.64 0 00-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0011.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
                    />
                  </svg>
                  <span>{{ t('common.contactSupport') }}:</span>
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{
                    contactInfo
                  }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 14a1 1 0 110 2 1 1 0 010-2zm1.07-7.75c0-.6-.49-1.25-1.32-1.25-.7 0-1.22.4-1.43 1.02a1 1 0 11-1.9-.62A3.41 3.41 0 0111.8 5c2.02 0 3.25 1.4 3.25 2.9 0 2-1.83 2.55-2.43 3.12-.43.4-.47.75-.47 1.23a1 1 0 01-2 0c0-1 .16-1.82 1.1-2.7.69-.64 1.82-1.05 1.82-2.06z"
                    />
                  </svg>
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"
                    />
                  </svg>
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
  <!-- Support modal -->
  <Teleport to="body">
    <Transition name="support-modal">
      <div
        v-if="supportModalOpen && user"
        class="fixed inset-0 z-[110] flex items-start justify-center overflow-y-auto bg-slate-950/65 p-4 pt-[8vh] backdrop-blur-sm sm:items-center sm:pt-4"
        role="presentation"
        @click.self="closeSupportModal"
      >
        <section
          class="w-full max-w-3xl overflow-hidden rounded-3xl border border-white/60 bg-white shadow-2xl shadow-slate-950/20 dark:border-dark-700 dark:bg-dark-800"
          role="dialog"
          aria-modal="true"
          aria-labelledby="support-modal-title"
          @click.stop
        >
          <div class="flex items-start justify-between gap-4 border-b border-gray-100 bg-gray-50/80 px-5 py-5 dark:border-dark-700 dark:bg-dark-900/50 sm:px-7">
            <div class="flex min-w-0 items-center gap-3">
              <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl border border-sky-100 bg-sky-50 text-sky-600 dark:border-sky-900/50 dark:bg-sky-950/40 dark:text-sky-300">
                <Icon name="chat" size="lg" />
              </div>
              <div class="min-w-0">
                <p class="text-[11px] font-bold uppercase tracking-[0.18em] text-sky-600 dark:text-sky-300">Support Center</p>
                <h2 id="support-modal-title" class="mt-1 truncate text-xl font-bold text-gray-950 dark:text-white">客服群聊</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">售前售后都可以联系我们</p>
              </div>
            </div>
            <button
              type="button"
              class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl text-gray-400 transition-colors hover:bg-gray-200/80 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              aria-label="关闭客服群聊弹窗"
              title="关闭"
              @click="closeSupportModal"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
          <div class="grid gap-4 p-5 sm:grid-cols-2 sm:gap-5 sm:p-7">
            <article class="rounded-2xl border border-sky-100 bg-sky-50/60 p-4 dark:border-sky-900/50 dark:bg-sky-950/20 sm:p-5">
              <div class="flex items-start gap-3">
                <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-sky-600 text-white shadow-lg shadow-sky-600/20">
                  <Icon name="chatBubble" size="sm" />
                </div>
                <div class="min-w-0">
                  <h3 class="text-base font-bold text-gray-950 dark:text-white">客服 QQ</h3>
                  <p class="mt-1 text-xs leading-relaxed text-sky-800/80 dark:text-sky-200/80">售前售后客服，有问题联系</p>
                </div>
              </div>
              <div class="mt-4 flex items-center justify-between rounded-xl border border-sky-100 bg-white/80 px-3 py-2.5 dark:border-sky-900/40 dark:bg-dark-800/70">
                <span class="text-xs font-medium text-gray-500 dark:text-gray-400">QQ 号</span>
                <span class="font-mono text-lg font-bold tracking-wide text-sky-700 dark:text-sky-300">{{ supportContact.qq }}</span>
              </div>
              <a class="group mt-4 flex justify-center rounded-2xl border border-sky-100 bg-white p-3 transition-colors hover:border-sky-300 hover:bg-sky-50 dark:border-sky-900/50 dark:bg-dark-900/50 dark:hover:border-sky-700 dark:hover:bg-sky-950/30" :href="supportContact.qqQr" target="_blank" rel="noopener noreferrer" aria-label="打开客服 QQ 二维码原图">
                <img :src="supportContact.qqQr" alt="客服 QQ 二维码" class="aspect-square w-full max-w-[220px] rounded-xl object-contain" loading="lazy" referrerpolicy="no-referrer" />
              </a>
              <p class="mt-2 text-center text-xs text-gray-400 dark:text-gray-500">点击二维码查看大图</p>
            </article>
            <article class="rounded-2xl border border-violet-100 bg-violet-50/60 p-4 dark:border-violet-900/50 dark:bg-violet-950/20 sm:p-5">
              <div class="flex items-start gap-3">
                <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-violet-600 text-white shadow-lg shadow-violet-600/20">
                  <Icon name="users" size="sm" />
                </div>
                <div class="min-w-0">
                  <h3 class="text-base font-bold text-gray-950 dark:text-white">QQ群聊</h3>
                  <p class="mt-1 text-xs leading-relaxed text-violet-800/80 dark:text-violet-200/80">加群及时获取活动倍率通知，不定时抽奖</p>
                </div>
              </div>
              <div class="mt-4 flex items-center justify-between rounded-xl border border-violet-100 bg-white/80 px-3 py-2.5 dark:border-violet-900/40 dark:bg-dark-800/70">
                <span class="text-xs font-medium text-gray-500 dark:text-gray-400">群号</span>
                <span class="font-mono text-lg font-bold tracking-wide text-violet-700 dark:text-violet-300">{{ supportContact.group }}</span>
              </div>
              <a class="group mt-4 flex justify-center rounded-2xl border border-violet-100 bg-white p-3 transition-colors hover:border-violet-300 hover:bg-violet-50 dark:border-violet-900/50 dark:bg-dark-900/50 dark:hover:border-violet-700 dark:hover:bg-violet-950/30" :href="supportContact.groupQr" target="_blank" rel="noopener noreferrer" aria-label="打开 QQ 群二维码原图">
                <img :src="supportContact.groupQr" alt="QQ群二维码" class="aspect-square w-full max-w-[220px] rounded-xl object-contain" loading="lazy" referrerpolicy="no-referrer" />
              </a>
              <p class="mt-2 text-center text-xs text-gray-400 dark:text-gray-500">点击二维码查看大图</p>
            </article>
          </div>
          <div class="border-t border-gray-100 bg-gray-50/70 px-5 py-4 dark:border-dark-700 dark:bg-dark-900/35 sm:px-7">
            <p class="flex items-center justify-center gap-2 text-center text-xs text-gray-500 dark:text-gray-400">
              <Icon name="infoCircle" size="xs" class="shrink-0 text-gray-400" />
              请认准官方客服与群聊信息，谨防冒充和诈骗
            </p>
          </div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const onboardingStore = useOnboardingStore()

const user = computed(() => authStore.user)
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const contactInfo = computed(() => appStore.contactInfo)
const supportModalOpen = ref(false)
const paymentEnabled = computed(() => appStore.cachedPublicSettings?.payment_enabled === true)
const supportContact = {
  qq: '1009700115',
  qqQr: 'https://pan.qzyun.net/f/rQ5Pt5/QQ20260709-112354.jpg',
  group: '383233702',
  groupQr: 'https://pan.qzyun.net/f/Pvv6s3/QQ20260709-112108.jpg',
} as const
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const totalBalance = computed(() => availableBalance.value + frozenBalance.value)
const balanceAvailableText = computed(() => t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance'))
const balanceFrozenText = computed(() => t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance'))
const balanceTotalText = computed(() => t('common.totalBalance') === 'common.totalBalance' ? '总余额' : t('common.totalBalance'))
const balanceFrozenLabel = computed(() => `${balanceFrozenText.value} ${formatHeaderMoney(frozenBalance.value)}`)

// 只在标准模式的管理员下显示新手引导按钮
const showOnboardingButton = computed(() => {
  return !authStore.isSimpleMode && user.value?.role === 'admin'
})

const userInitials = computed(() => {
  if (!user.value) return ''
  // Prefer username, fallback to email
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    // Get the part before @ and take first 2 chars
    const localPart = user.value.email.split('@')[0]
    return localPart.substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

// 订阅功能关闭时不挂载顶栏订阅徽章（组件 onMounted 会拉取订阅接口）。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

// /purchase 的标题/描述随站点计费模式切换，与 document.title 共用同一解析。
const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
  billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
}))

const pageTitle = computed(() => {
  // For custom pages, use the menu item's label instead of generic "自定义页面"
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
    if (menuItem?.label) return menuItem.label
  }
  const titleKey = routeMetaKeys.value.titleKey
  if (titleKey) {
    return t(titleKey)
  }
  return (route.meta.title as string) || ''
})

const pageDescription = computed(() => {
  const descKey = routeMetaKeys.value.descriptionKey
  if (descKey) {
    return t(descKey)
  }
  return (route.meta.description as string) || ''
})

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function openSupportModal() {
  closeDropdown()
  supportModalOpen.value = true
}
function closeSupportModal() {
  supportModalOpen.value = false
}
function goToRecharge() {
  closeDropdown()
  router.push('/purchase')
}
function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } catch (error) {
    // Ignore logout errors - still redirect to login
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
}

function formatHeaderMoney(value: number) {
  if (!Number.isFinite(value)) return '$0.00'
  return `$${value.toFixed(2)}`
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    closeDropdown()
  }
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && supportModalOpen.value) {
    closeSupportModal()
  }
}
onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleGlobalKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
.support-modal-enter-active,
.support-modal-leave-active {
  transition: opacity 0.2s ease;
}
.support-modal-enter-from,
.support-modal-leave-to {
  opacity: 0;
}
.support-modal-enter-from section,
.support-modal-leave-to section {
  transform: translateY(-8px) scale(0.98);
}
</style>
