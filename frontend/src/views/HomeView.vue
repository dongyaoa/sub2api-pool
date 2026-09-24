<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img
          :src="siteLogo || '/logo.svg'"
          alt="Logo"
          class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
        />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 [overflow-wrap:anywhere] sm:px-6 dark:border-dark-800 dark:text-dark-400">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    class="home-shell flex min-h-screen flex-col text-slate-950 transition-colors duration-300 dark:text-white"
    :class="{ 'is-dark': isDark }"
  >
    <header class="home-header">
      <nav class="home-nav mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4 sm:h-[72px] sm:px-6">
        <router-link to="/home" class="home-brand flex min-w-0 items-center gap-3" :aria-label="siteName">
          <span class="brand-mark">
            <img
              v-if="siteLogo"
              :src="siteLogo"
              :alt="siteName"
              class="h-full w-full object-contain"
            />
            <span v-else>{{ siteInitial }}</span>
          </span>
          <span class="min-w-0">
            <span class="block truncate text-sm font-semibold sm:text-base">{{ siteName }}</span>
            <span class="hidden text-[11px] text-slate-500 dark:text-zinc-500 sm:block">
              AI API Gateway
            </span>
          </span>
        </router-link>

        <div class="flex shrink-0 items-center gap-1.5 sm:gap-2">
          <router-link
            to="/status"
            class="nav-link nav-status-link"
            :aria-label="copy.statusNavLabel"
            :title="copy.statusNavLabel"
          >
            <Icon name="server" size="sm" />
            <span class="hidden lg:inline">{{ copy.statusNavLabel }}</span>
          </router-link>

          <a href="#pricing" class="nav-link hidden md:inline-flex">
            {{ copy.pricingNavLabel }}
          </a>

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="nav-link hidden md:inline-flex"
          >
            {{ copy.docsLabel }}
            <Icon name="externalLink" size="xs" />
          </a>

          <LocaleSwitcher />


          <!-- Theme Toggle -->
          <button
            class="icon-button"
            :title="copy.announcementLabel"
            :aria-label="copy.announcementLabel"
            @click="openAnnouncementModal"
          >
            <span v-if="showAnnouncementDot" class="notification-dot"></span>
            <Icon name="bell" size="sm" />
          </button>

          <button
            class="icon-button hidden sm:inline-flex"
            :title="copy.serviceLabel"
            :aria-label="copy.serviceLabel"
            @click="serviceModalOpen = true"
          >
            <Icon name="chatBubble" size="sm" />
          </button>

          <button
            class="icon-button"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          </button>

          <router-link :to="primaryActionPath" class="nav-cta hidden lg:inline-flex">
            <span v-if="isAuthenticated" class="user-initial">{{ userInitial }}</span>
            {{ primaryActionLabel }}
            <Icon name="arrowRight" size="xs" />
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex flex-1 flex-col">
      <section class="hero-section">
        <canvas ref="heroCanvas" class="hero-motion-canvas" aria-hidden="true"></canvas>

        <div class="hero-content">
          <div class="hero-kicker hero-reveal hero-reveal-1">
            <span></span>
            <strong>{{ siteName }}</strong>
            <small>AI Gateway</small>
            <span></span>
          </div>

          <h1 class="hero-title hero-reveal hero-reveal-2">
            <span class="hero-title-primary">{{ copy.heroTitle }}</span>
            <span class="hero-title-accent">{{ copy.heroAccent }}</span>
          </h1>

          <p class="hero-description hero-reveal hero-reveal-3">
            {{ heroDescription }}
          </p>

          <div class="hero-actions hero-reveal hero-reveal-4">
            <router-link :to="primaryActionPath" class="primary-button">
              {{ primaryActionLabel }}
              <Icon name="arrowRight" size="sm" />
            </router-link>

            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="secondary-button"
            >
              <Icon name="book" size="sm" />
              {{ copy.docsLabel }}
            </a>
          </div>

          <div
            class="hero-highlights hero-reveal hero-reveal-5"
            role="list"
            :aria-label="copy.highlightSectionLabel"
          >
            <article
              v-for="highlight in heroHighlights"
              :key="highlight.title"
              class="hero-highlight"
              role="listitem"
            >
              <span class="highlight-icon">
                <Icon :name="highlight.icon" size="sm" />
              </span>
              <div>
                <h2>{{ highlight.title }}</h2>
                <p>{{ highlight.description }}</p>
              </div>
            </article>
          </div>
        </div>

        <div class="tool-marquee" role="region" :aria-label="copy.toolsLabel">
          <div class="tool-marquee-track">
            <div
              v-for="copyIndex in 2"
              :key="copyIndex"
              class="tool-marquee-set"
              :aria-hidden="copyIndex === 2"
            >
              <span v-for="tool in toolProducts" :key="`${copyIndex}-${tool.name}`" class="tool-item">
                <img v-if="tool.logo" :src="tool.logo" alt="" />
                <span v-else class="tool-fallback">OAI</span>
                <span>{{ tool.name }}</span>
              </span>
            </div>
          </div>
        </div>
      </section>

      <section id="pricing" class="pricing-section" :aria-label="copy.pricingTitle">
        <div class="pricing-container">
          <header class="section-heading">
            <span class="section-kicker">{{ copy.pricingEyebrow }}</span>
            <h2>{{ copy.pricingTitle }}</h2>
            <p>{{ copy.pricingDescription }}</p>
            <div class="exchange-rate-note">
              <span>{{ copy.rechargeRateLabel }}</span>
              <strong>¥1 = $1</strong>
            </div>
          </header>

          <div class="pricing-table-shell">
            <table class="pricing-table">
              <thead>
                <tr>
                  <th scope="col">{{ copy.modelColumnLabel }}</th>
                  <th scope="col">{{ copy.officialPriceLabel }}</th>
                  <th scope="col">{{ copy.ourPriceLabel }}</th>
                  <th scope="col">{{ copy.savingsLabel }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in pricingModels" :key="model.name">
                  <th scope="row" :data-label="copy.modelColumnLabel">
                    <span class="pricing-model-name">
                      <span class="pricing-model-dot"></span>
                      <strong>{{ model.name }}</strong>
                      <span v-if="model.badge" class="pricing-badge">{{ model.badge }}</span>
                    </span>
                  </th>
                  <td :data-label="copy.officialPriceLabel">
                    <div class="price-pair is-official">
                      <span><small>{{ copy.inputLabel }}</small><del>{{ formatUsd(model.officialInput) }}</del></span>
                      <span><small>{{ copy.outputLabel }}</small><del>{{ formatUsd(model.officialOutput) }}</del></span>
                    </div>
                  </td>
                  <td :data-label="copy.ourPriceLabel">
                    <div class="price-pair is-current">
                      <span><small>{{ copy.inputLabel }}</small>{{ formatUsd(model.ourInput) }}</span>
                      <span><small>{{ copy.outputLabel }}</small>{{ formatUsd(model.ourOutput) }}</span>
                    </div>
                  </td>
                  <td :data-label="copy.savingsLabel">
                    <div class="savings-pair">
                      <span>{{ copy.approxSavingsLabel }} {{ calculateMaxSavings(model) }}%</span>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </main>

    <footer class="home-footer">
      <div class="mx-auto flex max-w-6xl flex-col items-center justify-between gap-3 px-4 py-5 text-center sm:flex-row sm:px-6 sm:text-left">
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <div class="flex items-center gap-4">
          <button @click="announcementModalOpen = true">{{ copy.announcementLabel }}</button>
          <button @click="serviceModalOpen = true">{{ copy.serviceLabel }}</button>
          <router-link to="/status">{{ copy.statusNavLabel }}</router-link>
          <a href="#pricing">{{ copy.pricingNavLabel }}</a>

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
          >
            {{ copy.docsLabel }}
          </a>
        </div>
      </div>
    </footer>

    <Teleport to="body">
      <Transition name="modal-fade">
        <div
          v-if="serviceModalOpen"
          class="modal-backdrop"
          role="presentation"
          @click="serviceModalOpen = false"
        >
          <section
            class="modal-panel max-w-md"
            role="dialog"
            aria-modal="true"
            :aria-label="copy.serviceTitle"
            @click.stop
          >
            <header class="modal-header">
              <div>
                <p>{{ copy.serviceLabel }}</p>
                <h2>{{ copy.serviceTitle }}</h2>
              </div>
              <button
                class="icon-button"
                :aria-label="copy.closeLabel"
                @click="serviceModalOpen = false"
              >
                <Icon name="x" size="sm" />
              </button>
            </header>

            <div class="p-6 text-center">
              <div class="support-heading">
                <span class="feature-icon"><Icon name="chatBubble" size="sm" /></span>
                <div class="text-left">
                  <h3>{{ copy.supportCardTitle }}</h3>
                  <p>{{ copy.supportCardDescription }}</p>
                </div>
              </div>

              <div class="support-qr">
                <img
                  v-if="supportQrImageUrl && !supportImageFailed"
                  :src="supportQrImageUrl"
                  @error="supportImageFailed = true"
                  :alt="copy.supportCardTitle"
                  class="h-44 w-44 rounded-lg object-cover"
                />
                <p v-else>{{ copy.supportEmpty }}</p>
              </div>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="modal-fade">
        <div
          v-if="announcementModalOpen"
          class="modal-backdrop"
          role="presentation"
          @click="announcementModalOpen = false"
        >
          <section
            class="modal-panel max-w-2xl"
            role="dialog"
            aria-modal="true"
            :aria-label="copy.announcementTitle"
            @click.stop
          >
            <header class="modal-header">
              <div>
                <p>{{ copy.announcementLabel }}</p>
                <h2>{{ copy.announcementTitle }}</h2>
              </div>
              <button
                class="icon-button"
                :aria-label="copy.closeLabel"
                @click="announcementModalOpen = false"
              >
                <Icon name="x" size="sm" />
              </button>
            </header>

            <div class="max-h-[60vh] overflow-y-auto p-5">
              <div v-if="announcementLoading" class="flex justify-center py-12">
                <span class="loading-ring"></span>
              </div>

              <div v-else-if="visibleAnnouncements.length === 0" class="empty-state">
                <Icon name="bell" size="lg" />
                <h3>{{ copy.announcementEmpty }}</h3>
                <p>{{ copy.announcementEmptyDesc }}</p>
              </div>

              <div v-else class="space-y-3">
                <article
                  v-for="item in visibleAnnouncements"
                  :key="item.id"
                  class="announcement-item"
                  :class="{ 'is-unread': !item.read_at }"
                >
                  <div class="flex items-start justify-between gap-4">
                    <div>
                      <div class="flex items-center gap-2">
                        <span v-if="!item.read_at" class="notification-dot static"></span>
                        <h3>{{ item.title }}</h3>
                      </div>
                      <p class="announcement-meta">
                        {{ formatRelativeWithDateTime(item.created_at) }}
                        <span>&middot;</span>
                        {{ item.read_at ? copy.announcementRead : copy.announcementUnread }}
                      </p>
                    </div>

                    <button
                      v-if="!item.read_at"
                      class="text-button"
                      @click="markAnnouncementRead(item.id)"
                    >
                      {{ copy.markReadLabel }}
                    </button>
                  </div>

                  <div
                    class="announcement-body mt-3 text-sm leading-7 text-slate-600 dark:text-zinc-300"
                    v-html="renderAnnouncement(item.content)"
                  ></div>
                </article>
              </div>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { storeToRefs } from 'pinia'
import { useAnnouncementStore, useAppStore, useAuthStore } from '@/stores'
import { formatRelativeWithDateTime } from '@/utils/format'
import { sanitizeUrl } from '@/utils/url'
import type { UserAnnouncement } from '@/types'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, locale } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()
const { announcements, loading: storeAnnouncementLoading } = storeToRefs(announcementStore)

const DEFAULT_SITE_SUBTITLE = 'AI API Gateway Platform'
const FRONTEND_SUPPORT_QR_IMAGE_URL =
  'https://www.weidus.com/wp-content/uploads/2026/05/1779695656-QQ20260525-155015.png'

const siteName = computed(() => {
  const configuredName = (appStore.cachedPublicSettings?.site_name || appStore.siteName || '').trim()
  if (!configuredName || configuredName.toLowerCase() === 'sub2api') return 'Z-API'
  return configuredName
})
const siteInitial = computed(() => siteName.value.slice(0, 1).toUpperCase())
const siteLogo = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
    allowRelative: true,
    allowDataUrl: true
  })
)
const siteSubtitle = computed(
  () => appStore.cachedPublicSettings?.site_subtitle || DEFAULT_SITE_SUBTITLE
)
const docUrl = computed(() =>
  sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
)
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const supportQrImageUrl = computed(() => sanitizeUrl(FRONTEND_SUPPORT_QR_IMAGE_URL))
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isDark = ref(document.documentElement.classList.contains('dark'))
const heroCanvas = ref<HTMLCanvasElement | null>(null)
let stopHeroMotion: (() => void) | null = null
let redrawHeroMotion: (() => void) | null = null
const serviceModalOpen = ref(false)
const announcementModalOpen = ref(false)
const publicAnnouncements = ref<UserAnnouncement[]>([])
const publicAnnouncementLoading = ref(false)
const supportImageFailed = ref(false)

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const userInitial = computed(() => authStore.user?.email?.charAt(0).toUpperCase() || 'U')
const currentYear = computed(() => new Date().getFullYear())
const isZh = computed(() => locale.value.startsWith('zh'))
const unreadCount = computed(() => announcementStore.unreadCount)
const showAnnouncementDot = computed(() => isAuthenticated.value && unreadCount.value > 0)
const visibleAnnouncements = computed(() =>
  isAuthenticated.value ? announcements.value : publicAnnouncements.value
)
const announcementLoading = computed(() =>
  isAuthenticated.value ? storeAnnouncementLoading.value : publicAnnouncementLoading.value
)

const primaryActionPath = computed(() =>
  isAuthenticated.value ? dashboardPath.value : '/login'
)
const primaryActionLabel = computed(() => {
  if (isAuthenticated.value) return isZh.value ? '进入控制台' : 'Open Console'
  return isZh.value ? '立即开始' : 'Get Started'
})

const copy = computed(() =>
  isZh.value
    ? {
        heroTitle: '一个 Key，连接',
        heroAccent: '全球顶尖 AI 模型',
        heroDescription:
          '稳定接入 Claude Code、Codex、Cursor 等 AI 工具，一个 Key 即可使用主流模型。',
        docsLabel: '接入文档',
        statusNavLabel: '服务状态',
        pricingNavLabel: '定价',
        serviceLabel: '客服',
        serviceTitle: '联系技术支持',
        supportCardTitle: '扫码联系客服',
        supportCardDescription: '充值、接入或使用问题，我们会尽快回复。',
        supportEmpty: '客服二维码暂未配置',
        announcementLabel: '公告',
        announcementTitle: '站点公告',
        announcementEmpty: '暂无公告',
        announcementEmptyDesc: '当前没有需要展示的站点通知。',
        announcementRead: '已读',
        announcementUnread: '未读',
        markReadLabel: '标记已读',
        closeLabel: '关闭',
        highlightSectionLabel: '核心优势',
        toolsLabel: '支持的 AI 工具',
        pricingEyebrow: 'TRANSPARENT PRICING',
        pricingTitle: '模型定价',
        pricingDescription:
          '价格均以美元（$）计价，按输入和输出分别对比官方价格。单位：每百万 Tokens。',
        modelColumnLabel: '模型',
        officialPriceLabel: '官方价格 ($/1M Tokens)',
        ourPriceLabel: `${siteName.value} 价格 ($/1M Tokens)`,
        savingsLabel: '节省',
        approxSavingsLabel: '约省',
        rechargeRateLabel: '充值到账',
        inputLabel: '输入',
        outputLabel: '输出'
      }
    : {
        heroTitle: 'One Key for',
        heroAccent: 'the world’s leading AI models',
        heroDescription:
          'Connect Claude Code, Codex, Cursor, and more with one key for today’s leading models.',
        docsLabel: 'Documentation',
        statusNavLabel: 'Service Status',
        pricingNavLabel: 'Pricing',
        serviceLabel: 'Support',
        serviceTitle: 'Technical Support',
        supportCardTitle: 'Scan to Contact Us',
        supportCardDescription: 'Get help with billing, integration, or everyday use.',
        supportEmpty: 'Support QR code is not configured',
        announcementLabel: 'News',
        announcementTitle: 'Announcements',
        announcementEmpty: 'No announcements',
        announcementEmptyDesc: 'There are no site updates to show right now.',
        announcementRead: 'Read',
        announcementUnread: 'Unread',
        markReadLabel: 'Mark as read',
        closeLabel: 'Close',
        highlightSectionLabel: 'Core advantages',
        toolsLabel: 'Supported AI tools',
        pricingEyebrow: 'TRANSPARENT PRICING',
        pricingTitle: 'Model Pricing',
        pricingDescription:
          'All prices are in USD and compared with official input and output pricing. Unit: one million tokens.',
        modelColumnLabel: 'Model',
        officialPriceLabel: 'Official price ($/1M tokens)',
        ourPriceLabel: `${siteName.value} price ($/1M tokens)`,
        savingsLabel: 'Savings',
        approxSavingsLabel: 'Save about',
        rechargeRateLabel: 'Recharge credit',
        inputLabel: 'Input',
        outputLabel: 'Output'
      }
)

const heroDescription = computed(() => {
  const subtitle = siteSubtitle.value.trim()
  const genericSubtitles = [
    DEFAULT_SITE_SUBTITLE,
    'Subscription to API Conversion Platform'
  ]
  return subtitle && !genericSubtitles.includes(subtitle) ? subtitle : copy.value.heroDescription
})

const heroHighlights = computed(() => [
  {
    icon: 'dollar' as const,
    title: isZh.value ? '最高节省 91%' : 'Save up to 91%',
    description: isZh.value
      ? '热门模型价格清晰，输入与输出成本分别可查。'
      : 'Clear input and output pricing for the models you use most.'
  },
  {
    icon: 'shield' as const,
    title: isZh.value ? '稳定路由' : 'Stable routing',
    description: isZh.value
      ? '自动选择可用上游，减少请求波动。'
      : 'Automatic upstream selection keeps requests moving.'
  },
  {
    icon: 'chartBar' as const,
    title: isZh.value ? '透明计费' : 'Transparent billing',
    description: isZh.value
      ? '请求、用量与余额明细清晰可查。'
      : 'Requests, usage, and balance stay easy to audit.'
  },
  {
    icon: 'chatBubble' as const,
    title: isZh.value ? '专属支持' : 'Dedicated support',
    description: isZh.value
      ? '接入、充值与使用问题快速响应。'
      : 'Fast help with integration, billing, and everyday use.'
  }
])

const toolProducts = [
  { name: 'Codex', logo: '/home-tools/codex.svg' },
  { name: 'VS Code', logo: '/home-tools/vscode.ico' },
  { name: 'Claude Code', logo: '/home-tools/claude.svg' },
  { name: 'OpenClaw', logo: '/home-tools/openclaw.svg' },
  { name: 'Hermes', logo: '/home-tools/hermes.png' },
  { name: 'Cherry Studio', logo: '/home-tools/cherry-studio.png' }
]

const pricingModels = [
  {
    name: 'GPT-6 Astra',
    badge: 'HOT',
    officialInput: 10,
    officialOutput: 50,
    ourInput: 0.45,
    ourOutput: 2.7
  },
  {
    name: 'Claude-Fable-5.1',
    badge: '',
    officialInput: 10,
    officialOutput: 50,
    ourInput: 1.3,
    ourOutput: 6.5
  }
]

function calculateSavings(currentPrice: number, officialPrice: number) {
  return Math.round((1 - currentPrice / officialPrice) * 100)
}

function calculateMaxSavings(model: (typeof pricingModels)[number]) {
  return Math.max(
    calculateSavings(model.ourInput, model.officialInput),
    calculateSavings(model.ourOutput, model.officialOutput)
  )
}

function formatUsd(value: number) {
  return '$' + value.toFixed(2)
}

marked.setOptions({
  breaks: true,
  gfm: true
})
function renderAnnouncement(content: string) {
  if (!content) return ''
  return DOMPurify.sanitize(marked.parse(content) as string)
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  requestAnimationFrame(() => redrawHeroMotion?.())
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  const shouldUseDark = savedTheme ? savedTheme === 'dark' : true
  isDark.value = shouldUseDark
  document.documentElement.classList.toggle('dark', shouldUseDark)
}

function setupHeroMotion(): () => void {
  const canvas = heroCanvas.value
  const context = canvas?.getContext('2d')
  if (!canvas || !context) return () => undefined

  const motionPreference = window.matchMedia('(prefers-reduced-motion: reduce)')
  let animationFrame = 0
  let canvasWidth = 1
  let canvasHeight = 1
  let lastTimestamp = 0

  const waveY = (wave: number, x: number, time: number, compact: boolean) => {
    const waveCount = compact ? 3 : 5
    const spread = compact ? 0.18 : 0.135
    const baseY = canvasHeight * (compact ? 0.32 : 0.27) + canvasHeight * spread * wave
    const amplitude = canvasHeight * (compact ? 0.014 : 0.02) * (1 + (wave % 2) * 0.22)
    const direction = wave % 2 === 0 ? 1 : -1
    const primaryFrequency = compact ? 0.0085 : 0.0062

    return (
      baseY +
      Math.sin(x * primaryFrequency + time * (0.16 + wave * 0.018) * direction + wave * 1.35) *
        amplitude +
      Math.sin(x * 0.0022 - time * 0.1 + wave * 0.72) * amplitude * 0.48 +
      ((wave - (waveCount - 1) / 2) / waveCount) * Math.sin(time * 0.09) * 8
    )
  }

  const render = (timestamp: number) => {
    lastTimestamp = timestamp
    const time = timestamp / 1000
    const compact = canvasWidth < 640
    const primary = isDark.value ? '#41d9c8' : '#13b2a1'
    const secondary = isDark.value ? '#62a8ff' : '#4b7fc2'
    const highlight = isDark.value ? '#c2fff7' : '#0b8076'

    context.clearRect(0, 0, canvasWidth, canvasHeight)
    context.save()
    context.lineCap = 'round'

    const threadCount = compact ? 28 : 68
    const threadStep = canvasWidth / Math.max(1, threadCount - 1)

    for (let thread = 0; thread < threadCount; thread += 1) {
      const baseX = thread * threadStep
      const phase = thread * 0.63
      context.beginPath()

      for (let y = -24; y <= canvasHeight + 24; y += compact ? 34 : 28) {
        const x =
          baseX +
          Math.sin(y * 0.0054 + time * 0.11 + phase) * (compact ? 2.5 : 4.5) +
          Math.sin(time * 0.07 - phase) * 2.2
        if (y === -24) context.moveTo(x, y)
        else context.lineTo(x, y)
      }

      context.strokeStyle = thread % 6 === 0 ? secondary : primary
      context.globalAlpha =
        (isDark.value ? 0.026 : 0.018) +
        (0.5 + Math.sin(time * 0.15 + phase) * 0.5) * (isDark.value ? 0.018 : 0.012)
      context.lineWidth = thread % 6 === 0 ? 0.8 : 0.55
      context.stroke()
    }

    const waveCount = compact ? 3 : 5
    for (let wave = 0; wave < waveCount; wave += 1) {
      context.beginPath()
      for (let x = -24; x <= canvasWidth + 24; x += compact ? 14 : 10) {
        const y = waveY(wave, x, time, compact)
        if (x === -24) context.moveTo(x, y)
        else context.lineTo(x, y)
      }

      context.strokeStyle = wave % 3 === 1 ? secondary : primary
      context.globalAlpha = (isDark.value ? 0.15 : 0.11) - wave * (isDark.value ? 0.015 : 0.012)
      context.lineWidth = wave === Math.floor(waveCount / 2) ? 1.45 : 0.85
      context.stroke()

      const nodeCount = compact ? 1 : 2
      for (let node = 0; node < nodeCount; node += 1) {
        const progress = (time * (0.012 + wave * 0.0018) + node * 0.49 + wave * 0.14) % 1
        const x = progress * canvasWidth
        const y = waveY(wave, x, time, compact)
        context.beginPath()
        context.arc(x, y, wave === Math.floor(waveCount / 2) ? 2 : 1.5, 0, Math.PI * 2)
        context.fillStyle = wave % 3 === 1 ? secondary : highlight
        context.globalAlpha = isDark.value ? 0.48 : 0.34
        context.shadowColor = context.fillStyle
        context.shadowBlur = isDark.value ? 12 : 7
        context.fill()
        context.shadowBlur = 0
      }
    }

    context.restore()
  }

  const resize = () => {
    const rect = canvas.getBoundingClientRect()
    canvasWidth = Math.max(1, Math.round(rect.width))
    canvasHeight = Math.max(1, Math.round(rect.height))
    const pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
    canvas.width = Math.round(canvasWidth * pixelRatio)
    canvas.height = Math.round(canvasHeight * pixelRatio)
    context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0)
    render(lastTimestamp || performance.now())
  }

  const animate = (timestamp: number) => {
    render(timestamp)
    animationFrame = requestAnimationFrame(animate)
  }

  const start = () => {
    cancelAnimationFrame(animationFrame)
    if (motionPreference.matches) render(performance.now())
    else animationFrame = requestAnimationFrame(animate)
  }

  const resizeObserver = new ResizeObserver(resize)
  const handleMotionPreferenceChange = () => start()
  resizeObserver.observe(canvas)
  motionPreference.addEventListener('change', handleMotionPreferenceChange)
  redrawHeroMotion = () => render(performance.now())
  resize()
  start()

  return () => {
    cancelAnimationFrame(animationFrame)
    resizeObserver.disconnect()
    motionPreference.removeEventListener('change', handleMotionPreferenceChange)
    redrawHeroMotion = null
  }
}
async function openAnnouncementModal() {
  announcementModalOpen.value = true
  if (isAuthenticated.value) {
    await announcementStore.fetchAnnouncements(true)
    return
  }
  await fetchPublicAnnouncements()
}

async function markAnnouncementRead(id: number) {
  if (!isAuthenticated.value) return
  await announcementStore.markAsRead(id)
}

async function fetchPublicAnnouncements() {
  publicAnnouncementLoading.value = true
  try {
    publicAnnouncements.value = []
  } finally {
    publicAnnouncementLoading.value = false
  }
}

onMounted(async () => {
  initTheme()
  stopHeroMotion = setupHeroMotion()
  authStore.checkAuth()

  if (!appStore.publicSettingsLoaded) {
    await appStore.fetchPublicSettings()
  }

  if (isAuthenticated.value) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  stopHeroMotion?.()
})
</script>

<style scoped>
.home-shell {
  --home-bg: #f2f8f8;
  --home-surface: rgba(255, 255, 255, 0.66);
  --home-surface-strong: rgba(255, 255, 255, 0.88);
  --home-soft: rgba(217, 240, 239, 0.72);
  --home-border: rgba(71, 85, 105, 0.1);
  --home-glass: rgba(255, 255, 255, 0.46);
  --home-glass-strong: rgba(255, 255, 255, 0.64);
  --home-glass-border: rgba(255, 255, 255, 0.86);
  --home-glass-highlight: rgba(255, 255, 255, 0.92);
  --home-muted: #5f7472;
  --home-accent: #13b2a1;
  --home-accent-strong: #0e998c;
  --home-secondary: #438ab6;
  --home-shadow: 0 18px 54px rgba(33, 89, 83, 0.09);
  background: var(--home-bg);
  color: #102522;
}

.home-shell.is-dark {
  --home-bg: #08100f;
  --home-surface: rgba(15, 29, 27, 0.72);
  --home-surface-strong: rgba(18, 35, 32, 0.9);
  --home-soft: rgba(25, 52, 48, 0.66);
  --home-border: rgba(148, 163, 184, 0.12);
  --home-glass: rgba(12, 28, 26, 0.5);
  --home-glass-strong: rgba(17, 38, 35, 0.68);
  --home-glass-border: rgba(115, 231, 217, 0.14);
  --home-glass-highlight: rgba(255, 255, 255, 0.07);
  --home-muted: #9bb1ae;
  --home-accent: #41d9c8;
  --home-accent-strong: #73e7d9;
  --home-secondary: #74b5df;
  --home-shadow: 0 22px 64px rgba(0, 0, 0, 0.24);
  color: #effaf8;
}

.home-header {
  position: sticky;
  top: 0;
  z-index: 50;
  width: 100%;
  padding: 12px 16px 0;
  background: transparent;
}

.home-nav {
  position: relative;
  overflow: visible;
  border: 1px solid var(--home-glass-border);
  border-radius: 8px;
  background: var(--home-glass);
  box-shadow:
    0 1px 0 var(--home-glass-highlight) inset,
    0 -1px 0 rgba(19, 178, 161, 0.04) inset,
    0 20px 60px rgba(33, 89, 83, 0.1);
  backdrop-filter: blur(30px) saturate(165%);
  -webkit-backdrop-filter: blur(30px) saturate(165%);
}

.is-dark .home-nav {
  box-shadow:
    0 1px 0 var(--home-glass-highlight) inset,
    0 -1px 0 rgba(65, 217, 200, 0.04) inset,
    0 20px 60px rgba(0, 0, 0, 0.3);
}

.home-brand {
  border-radius: 6px;
  transition: opacity 160ms ease;
}

.home-brand:hover {
  opacity: 0.78;
}

.brand-mark,
.hero-brand-mark {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border: 1px solid var(--home-glass-border);
  border-radius: 7px;
  background: var(--home-glass-strong);
  color: var(--home-accent-strong);
  box-shadow: 0 6px 18px rgba(19, 178, 161, 0.1);
  font-weight: 700;
}

.brand-mark {
  width: 36px;
  height: 36px;
  padding: 5px;
  font-size: 13px;
}

.hero-brand-mark {
  width: 28px;
  height: 28px;
  padding: 4px;
  font-size: 11px;
}

.nav-link,
.nav-cta,
.icon-button,
.primary-button,
.secondary-button {
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  transition:
    color 160ms ease,
    background-color 160ms ease,
    border-color 160ms ease,
    box-shadow 160ms ease,
    transform 160ms ease;
}

.nav-link {
  display: inline-flex;
  height: 36px;
  gap: 6px;
  padding: 0 11px;
  color: var(--home-muted);
  font-size: 13px;
  font-weight: 650;
}

.nav-link:hover {
  background: var(--home-glass-strong);
  color: inherit;
}

.nav-status-link {
  width: 36px;
  padding: 0;
}

.icon-button {
  position: relative;
  display: inline-flex;
  width: 36px;
  height: 36px;
  border: 1px solid transparent;
  color: var(--home-muted);
}

.icon-button:hover {
  border-color: var(--home-glass-border);
  background: var(--home-glass-strong);
  color: inherit;
}

.nav-cta {
  height: 36px;
  gap: 7px;
  padding: 0 14px;
  border: 1px solid color-mix(in srgb, var(--home-accent) 72%, transparent);
  background: var(--home-accent);
  color: #ffffff;
  box-shadow: 0 8px 22px rgba(19, 178, 161, 0.2);
  font-size: 13px;
  font-weight: 700;
}

.is-dark .nav-cta {
  color: #062119;
}

.nav-cta:hover {
  transform: translateY(-1px);
  box-shadow: 0 11px 28px rgba(19, 178, 161, 0.28);
}

.user-initial {
  display: inline-flex;
  width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.24);
  color: inherit;
  font-size: 10px;
  font-weight: 800;
}

.notification-dot {
  position: absolute;
  top: 7px;
  right: 7px;
  width: 6px;
  height: 6px;
  border: 1px solid var(--home-surface-strong);
  border-radius: 50%;
  background: #ef4444;
}

.notification-dot.static {
  position: static;
  display: inline-block;
  border: 0;
}

.hero-section {
  position: relative;
  isolation: isolate;
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: center;
  overflow: hidden;
  border-bottom: 1px solid var(--home-border);
  background: transparent;
  color: inherit;
}

.hero-motion-canvas {
  position: absolute;
  inset: 0;
  z-index: 0;
  width: 100%;
  height: 100%;
  opacity: 0.86;
  pointer-events: none;
}

.is-dark .hero-motion-canvas {
  opacity: 0.92;
}

.hero-content {
  position: relative;
  z-index: 1;
  display: flex;
  width: min(100%, 1320px);
  flex-direction: column;
  align-items: center;
  margin: 0 auto;
  padding: 86px 24px 0;
  text-align: center;
}

.hero-kicker {
  display: inline-flex;
  align-items: center;
  gap: 11px;
  color: var(--home-muted);
  font-size: 12px;
  line-height: 1;
  text-transform: uppercase;
}

.hero-kicker > span {
  width: 42px;
  height: 1px;
  background: color-mix(in srgb, var(--home-accent) 62%, var(--home-border));
}

.hero-kicker strong {
  color: var(--home-accent-strong);
  font-size: 12px;
  font-weight: 780;
}

.hero-kicker small {
  font-size: 11px;
  font-weight: 650;
}

.hero-title {
  width: 100%;
  max-width: 1280px;
  margin-top: 30px;
  font-size: 58px;
  font-weight: 800;
  line-height: 1.06;
  letter-spacing: 0;
  text-shadow:
    0 1px 0 var(--home-glass-highlight),
    0 18px 48px rgba(33, 89, 83, 0.1);
  text-wrap: balance;
}

.hero-title-primary,
.hero-title-accent {
  display: block;
}

.hero-title-accent {
  margin-top: 7px;
  color: var(--home-accent-strong);
}

.hero-description {
  max-width: 790px;
  margin-top: 28px;
  color: var(--home-muted);
  font-size: 18px;
  line-height: 1.78;
  text-wrap: balance;
}

.hero-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 36px;
}

.primary-button,
.secondary-button {
  display: inline-flex;
  min-width: 182px;
  height: 54px;
  gap: 11px;
  padding: 0 28px;
  font-size: 16px;
  font-weight: 750;
  line-height: 1;
}

.primary-button {
  border: 1px solid color-mix(in srgb, var(--home-accent) 75%, transparent);
  background: var(--home-accent);
  color: #ffffff;
  box-shadow: 0 14px 34px rgba(19, 178, 161, 0.22);
}

.is-dark .primary-button {
  color: #062119;
  box-shadow: 0 14px 36px rgba(0, 0, 0, 0.22);
}

.secondary-button {
  border: 1px solid var(--home-border);
  background: color-mix(in srgb, var(--home-surface-strong) 78%, transparent);
  color: inherit;
  box-shadow: 0 10px 30px rgba(33, 89, 83, 0.06);
  backdrop-filter: blur(18px) saturate(135%);
  -webkit-backdrop-filter: blur(18px) saturate(135%);
}

.primary-button:hover,
.secondary-button:hover {
  transform: translateY(-2px);
}

.secondary-button:hover {
  border-color: color-mix(in srgb, var(--home-accent) 36%, var(--home-border));
  background: var(--home-surface-strong);
}

.hero-highlights {
  display: grid;
  width: min(100%, 1140px);
  grid-template-columns: 1fr;
  gap: 12px;
  margin-top: 70px;
}

.hero-highlight {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 13px;
  border: 1px solid var(--home-glass-border);
  border-radius: 8px;
  background: var(--home-glass);
  padding: 18px;
  text-align: left;
  box-shadow:
    0 1px 0 var(--home-glass-highlight) inset,
    0 18px 46px rgba(33, 89, 83, 0.08);
  backdrop-filter: blur(26px) saturate(165%);
  -webkit-backdrop-filter: blur(26px) saturate(165%);
  transition:
    border-color 180ms ease,
    transform 180ms ease,
    background-color 180ms ease;
}

.hero-highlight:hover {
  border-color: color-mix(in srgb, var(--home-accent) 22%, var(--home-glass-border));
  background: var(--home-glass-strong);
  transform: translateY(-3px);
}

.highlight-icon,
.feature-icon {
  display: inline-flex;
  width: 36px;
  height: 36px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border: 1px solid color-mix(in srgb, var(--home-accent) 24%, var(--home-border));
  border-radius: 7px;
  background: color-mix(in srgb, var(--home-accent) 9%, var(--home-surface));
  color: var(--home-accent-strong);
}

.hero-highlight h2,
.support-heading h3,
.announcement-item h3,
.empty-state h3 {
  font-size: 14px;
  font-weight: 760;
}

.hero-highlight p,
.support-heading p,
.empty-state p {
  margin-top: 5px;
  color: var(--home-muted);
  font-size: 12px;
  line-height: 1.65;
}

.tool-marquee {
  position: relative;
  z-index: 1;
  width: min(calc(100% - 32px), 1180px);
  overflow: hidden;
  margin: 82px auto 64px;
  border: 1px solid var(--home-glass-border);
  border-radius: 8px;
  background: var(--home-glass);
  padding: 22px 0;
  box-shadow:
    0 1px 0 var(--home-glass-highlight) inset,
    var(--home-shadow);
  backdrop-filter: blur(26px) saturate(160%);
  -webkit-backdrop-filter: blur(26px) saturate(160%);
}

.tool-marquee-track {
  display: flex;
  width: max-content;
  animation: tool-marquee 34s linear infinite;
  will-change: transform;
}

.tool-marquee-set {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
}

.tool-item {
  display: inline-flex;
  width: 196px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  gap: 11px;
  color: var(--home-muted);
  font-size: 14px;
  font-weight: 700;
  white-space: nowrap;
}

.tool-item img {
  width: 27px;
  height: 27px;
  object-fit: contain;
  opacity: 0.76;
  filter: grayscale(1);
}

.is-dark .tool-item img {
  opacity: 0.66;
  filter: grayscale(1) brightness(1.42);
}

.tool-fallback {
  display: inline-flex;
  width: 29px;
  height: 29px;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--home-border);
  border-radius: 50%;
  color: var(--home-muted);
  font-size: 9px;
  font-weight: 800;
}

.hero-reveal {
  animation: hero-enter 700ms both cubic-bezier(0.22, 1, 0.36, 1);
}

.hero-reveal-1 {
  animation-delay: 40ms;
}

.hero-reveal-2 {
  animation-delay: 100ms;
}

.hero-reveal-3 {
  animation-delay: 160ms;
}

.hero-reveal-4 {
  animation-delay: 220ms;
}

.hero-reveal-5 {
  animation-delay: 280ms;
}

.pricing-section {
  position: relative;
  scroll-margin-top: 96px;
  border-bottom: 1px solid var(--home-border);
  background: transparent;
  padding: 92px 24px 104px;
}

.pricing-container {
  width: min(100%, 1180px);
  margin: 0 auto;
}

.section-heading {
  max-width: 760px;
  margin: 0 auto;
  text-align: center;
}

.section-kicker {
  color: var(--home-accent-strong);
  font-size: 11px;
  font-weight: 780;
  text-transform: uppercase;
}

.section-heading h2 {
  margin-top: 12px;
  font-size: 44px;
  font-weight: 800;
  line-height: 1.14;
  letter-spacing: 0;
}

.section-heading p {
  margin-top: 16px;
  color: var(--home-muted);
  font-size: 15px;
  line-height: 1.8;
}

.exchange-rate-note {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  margin-top: 22px;
  border: 1px solid color-mix(in srgb, var(--home-accent) 25%, var(--home-border));
  border-radius: 7px;
  background: color-mix(in srgb, var(--home-surface) 84%, transparent);
  padding: 9px 13px;
  color: var(--home-muted);
  box-shadow: 0 10px 28px rgba(33, 89, 83, 0.06);
  backdrop-filter: blur(16px) saturate(130%);
  -webkit-backdrop-filter: blur(16px) saturate(130%);
}

.exchange-rate-note span {
  font-size: 12px;
  font-weight: 680;
}

.exchange-rate-note strong {
  color: var(--home-accent-strong);
  font-size: 15px;
  font-weight: 800;
  letter-spacing: 0;
}
.pricing-table-shell {
  margin-top: 42px;
  overflow: hidden;
  border: 1px solid var(--home-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-surface) 90%, transparent);
  box-shadow: var(--home-shadow);
  backdrop-filter: blur(22px) saturate(135%);
  -webkit-backdrop-filter: blur(22px) saturate(135%);
}

.pricing-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}

.pricing-table th,
.pricing-table td {
  min-width: 0;
  border-bottom: 1px solid var(--home-border);
  padding: 25px 22px;
  text-align: left;
  vertical-align: middle;
}

.pricing-table tbody tr {
  transition: background-color 160ms ease;
}

.pricing-table tbody tr:hover {
  background: color-mix(in srgb, var(--home-soft) 52%, transparent);
}

.pricing-table tr:last-child th,
.pricing-table tr:last-child td {
  border-bottom: 0;
}

.pricing-table thead th {
  background: color-mix(in srgb, var(--home-soft) 48%, transparent);
  color: var(--home-muted);
  font-size: 11px;
  font-weight: 720;
  text-transform: uppercase;
}

.pricing-table thead th:first-child {
  width: 25%;
}

.pricing-table thead th:nth-child(2),
.pricing-table thead th:nth-child(3) {
  width: 27%;
}

.pricing-table thead th:last-child {
  width: 21%;
}

.pricing-model-name {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.pricing-model-name strong {
  overflow-wrap: anywhere;
  font-size: 16px;
  font-weight: 780;
}

.pricing-model-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--home-accent);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--home-accent) 12%, transparent);
}

.pricing-badge {
  border: 1px solid color-mix(in srgb, var(--home-accent) 42%, var(--home-border));
  border-radius: 6px;
  background: color-mix(in srgb, var(--home-accent) 8%, transparent);
  padding: 3px 7px;
  color: var(--home-accent-strong);
  font-size: 10px;
  font-weight: 780;
}

.price-pair {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
}

.price-pair span {
  display: inline-flex;
  flex-direction: column;
  gap: 4px;
  font-size: 15px;
  font-weight: 740;
}

.price-pair small {
  color: var(--home-muted);
  font-size: 10px;
  font-weight: 650;
}

.price-pair.is-official span {
  color: var(--home-muted);
}

.price-pair.is-official del {
  text-decoration-color: color-mix(in srgb, var(--home-muted) 88%, transparent);
  text-decoration-thickness: 1.5px;
  text-decoration-line: line-through;
}
.price-pair.is-current span {
  color: var(--home-accent-strong);
  font-size: 17px;
}

.savings-pair {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.savings-pair span {
  border: 1px solid color-mix(in srgb, var(--home-accent) 34%, var(--home-border));
  border-radius: 6px;
  background: color-mix(in srgb, var(--home-accent) 9%, transparent);
  padding: 7px 10px;
  color: var(--home-accent-strong);
  font-size: 12px;
  font-weight: 760;
  white-space: nowrap;
}

.home-footer {
  border-top: 0;
  background: transparent;
  color: var(--home-muted);
  font-size: 12px;
}

.home-footer button,
.home-footer a {
  transition: color 160ms ease;
}

.home-footer button:hover,
.home-footer a:hover {
  color: inherit;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 120;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(9, 12, 16, 0.68);
  backdrop-filter: blur(8px);
}

.modal-panel {
  --home-surface: #ffffff;
  --home-soft: #eef1f4;
  --home-border: #dfe3e8;
  --home-muted: #64748b;
  --home-accent: #13b2a1;
  --home-accent-strong: #0e998c;
  width: 100%;
  overflow: hidden;
  border: 1px solid var(--home-border, #dfe3e8);
  border-radius: 8px;
  background: var(--home-surface, #ffffff);
  color: #0f172a;
  box-shadow: 0 24px 70px rgba(0, 0, 0, 0.22);
}

:global(.dark .modal-panel) {
  --home-surface: #12151a;
  --home-soft: #181c22;
  --home-border: #272c34;
  --home-muted: #9ca3af;
  --home-accent: #41d9c8;
  --home-accent-strong: #73e7d9;
  color: #f4f4f5;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid var(--home-border, #dfe3e8);
  padding: 16px 20px;
}

.modal-header p {
  color: var(--home-muted, #64748b);
  font-size: 11px;
  font-weight: 650;
}

.modal-header h2 {
  margin-top: 2px;
  font-size: 18px;
  font-weight: 700;
}

.support-heading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
}

.support-qr {
  display: flex;
  min-height: 208px;
  align-items: center;
  justify-content: center;
  margin-top: 20px;
  border: 1px solid var(--home-border, #dfe3e8);
  border-radius: 8px;
  background: var(--home-soft, #eef1f4);
  color: var(--home-muted, #64748b);
  font-size: 13px;
}

.loading-ring {
  width: 28px;
  height: 28px;
  border: 3px solid var(--home-border, #dfe3e8);
  border-top-color: var(--home-accent, #13b2a1);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 42px 24px;
  color: var(--home-muted, #64748b);
  text-align: center;
}

.empty-state h3 {
  margin-top: 12px;
  color: inherit;
}

.announcement-item {
  border: 1px solid var(--home-border, #dfe3e8);
  border-radius: 8px;
  padding: 16px;
}

.announcement-item.is-unread {
  border-color: color-mix(in srgb, var(--home-accent, #13b2a1) 45%, var(--home-border, #dfe3e8));
}

.announcement-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 5px;
  color: var(--home-muted, #64748b);
  font-size: 11px;
}

.text-button {
  color: var(--home-accent-strong, #0e998c);
  font-size: 12px;
  font-weight: 650;
}

.announcement-body :deep(p) {
  margin-bottom: 8px;
}

.announcement-body :deep(a) {
  color: var(--home-accent-strong, #0e998c);
  text-decoration: underline;
}

.announcement-body :deep(code) {
  border-radius: 4px;
  background: var(--home-soft, #eef1f4);
  padding: 2px 5px;
}

.modal-fade-enter-active,
.modal-fade-leave-active {
  transition: opacity 180ms ease;
}

.modal-fade-enter-from,
.modal-fade-leave-to {
  opacity: 0;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes hero-enter {
  from {
    opacity: 0;
    transform: translateY(16px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes tool-marquee {
  to {
    transform: translateX(-50%);
  }
}

@media (min-width: 640px) {
  .hero-content {
    padding: 106px 28px 0;
  }

  .hero-title {
    font-size: 62px;
  }

  .hero-description {
    font-size: 18px;
  }

  .hero-highlights {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1100px) {
  .nav-status-link {
    width: auto;
    padding: 0 11px;
  }

  .hero-content {
    padding-top: 110px;
  }

  .hero-title {
    font-size: 68px;
  }

  .hero-title-primary,
  .hero-title-accent {
    display: inline;
  }

  .hero-title-accent {
    margin-top: 0;
    margin-left: 0.16em;
  }

  .hero-highlights {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (min-width: 1440px) {
  .hero-title {
    font-size: 72px;
  }
}

@media (max-width: 639px) {
  .home-header {
    padding: 8px 8px 0;
  }

  .home-nav {
    padding-right: 10px !important;
    padding-left: 10px !important;
  }

  .home-brand {
    gap: 8px;
  }

  .brand-mark {
    width: 34px;
    height: 34px;
  }

  .hero-content {
    padding: 56px 16px 0;
  }

  .hero-motion-canvas {
    opacity: 0.72;
  }

  .hero-kicker {
    gap: 8px;
  }

  .hero-kicker > span {
    width: 23px;
  }

  .hero-title {
    max-width: 410px;
    margin-top: 20px;
    font-size: 40px;
    line-height: 1.1;
  }

  .hero-title-accent {
    margin-top: 5px;
  }

  .hero-description {
    max-width: 370px;
    margin-top: 19px;
    font-size: 15px;
    line-height: 1.75;
  }

  .hero-actions {
    width: 100%;
    flex-direction: column;
    margin-top: 25px;
  }

  .primary-button,
  .secondary-button {
    width: min(100%, 334px);
  }

  .hero-highlights {
    margin-top: 50px;
  }

  .hero-highlight {
    padding: 16px;
  }

  .tool-marquee {
    width: calc(100% - 20px);
    margin-top: 58px;
    margin-bottom: 38px;
    padding: 19px 0;
  }

  .tool-item {
    width: 174px;
    font-size: 13px;
  }

  .pricing-section {
    padding: 68px 16px 78px;
  }

  .section-heading h2 {
    font-size: 34px;
  }

  .section-heading p {
    font-size: 14px;
  }

  .pricing-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
    box-shadow: none;
    backdrop-filter: none;
  }

  .pricing-table,
  .pricing-table tbody,
  .pricing-table tr,
  .pricing-table th,
  .pricing-table td {
    display: block;
    width: 100%;
  }

  .pricing-table {
    table-layout: auto;
  }

  .pricing-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .pricing-table tbody {
    display: grid;
    gap: 12px;
  }

  .pricing-table tr {
    overflow: hidden;
    border: 1px solid var(--home-border);
    border-radius: 8px;
    background: color-mix(in srgb, var(--home-surface) 90%, transparent);
    box-shadow: 0 14px 38px rgba(38, 70, 61, 0.06);
    backdrop-filter: blur(18px) saturate(130%);
    -webkit-backdrop-filter: blur(18px) saturate(130%);
  }

  .pricing-table th,
  .pricing-table td,
  .pricing-table tr:last-child th,
  .pricing-table tr:last-child td {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 18px;
    border-bottom: 1px solid var(--home-border);
    padding: 15px 16px;
    text-align: right;
  }

  .pricing-table td:last-child {
    border-bottom: 0;
  }

  .pricing-table th::before,
  .pricing-table td::before {
    content: attr(data-label);
    flex: 0 0 34%;
    color: var(--home-muted);
    font-size: 10px;
    font-weight: 680;
    text-align: left;
    text-transform: uppercase;
  }

  .pricing-model-name,
  .price-pair,
  .savings-pair {
    justify-content: flex-end;
  }

  .pricing-model-name,
  .price-pair,
  .savings-pair {
    flex: 1;
  }

  .price-pair {
    gap: 8px 14px;
  }

  .price-pair span {
    align-items: flex-end;
    font-size: 14px;
  }

  .price-pair.is-current span {
    font-size: 15px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .hero-reveal {
    animation: none;
  }

  .tool-marquee {
    overflow-x: auto;
  }

  .tool-marquee-track {
    animation: none;
  }
}
</style>
