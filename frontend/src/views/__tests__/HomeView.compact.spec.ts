import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { announcementStore, appStore, authStore, localeRef } = vi.hoisted(() => ({
  announcementStore: {
    announcements: { value: [], __v_isRef: true },
    loading: { value: false, __v_isRef: true },
    unreadCount: 0,
    fetchAnnouncements: vi.fn(),
    markAsRead: vi.fn(),
  },
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { email?: string } | null,
    checkAuth: vi.fn(),
  },
  localeRef: { value: 'zh-CN' },
}))

vi.mock('@/stores', () => ({
  useAnnouncementStore: () => announcementStore,
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: localeRef, t: (key: string) => key }),
  }
})

function mountHome(settings: Record<string, unknown> = {}) {
  appStore.cachedPublicSettings = {
    site_name: 'Test site',
    site_subtitle: 'Test subtitle',
    ...settings,
  }

  return mount(HomeView, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        LocaleSwitcher: { template: '<div data-testid="locale-switcher" />' },
        Icon: { template: '<span data-testid="icon" />' },
      },
    },
  })
}

function primaryDestination(wrapper: ReturnType<typeof mountHome>) {
  return wrapper.get('.hero-actions').findComponent(RouterLinkStub).props('to')
}

describe('HomeView compact mode', () => {
  beforeEach(() => {
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.user = null
    authStore.checkAuth.mockClear()
    announcementStore.fetchAnnouncements.mockClear()
    announcementStore.markAsRead.mockClear()
    appStore.fetchPublicSettings.mockClear()
    localeRef.value = 'zh-CN'
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
  })

  it('renders custom HTML ahead of the redesigned home', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: '<section id="custom-home">Custom home</section>',
    })

    expect(wrapper.get('#custom-home').text()).toBe('Custom home')
    expect(wrapper.find('.home-shell').exists()).toBe(false)
  })

  it('renders custom URL content ahead of the redesigned home', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: ' https://example.com/home ',
    })

    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/home')
    expect(wrapper.find('.home-shell').exists()).toBe(false)
  })

  it('treats whitespace-only custom content as empty', () => {
    const wrapper = mountHome({ compact_home_enabled: false, home_content: ' \n\t ' })

    expect(wrapper.get('.home-shell').text()).toContain('Test site')
  })

  it('renders the redesigned home when compact mode is disabled', () => {
    const wrapper = mountHome({ compact_home_enabled: false })
    expect(wrapper.find('.home-shell').exists()).toBe(true)
    expect(wrapper.find('.pricing-section').exists()).toBe(true)
  })

  it('renders the compact home when compact mode is enabled', () => {
    const wrapper = mountHome({ compact_home_enabled: true })
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(true)
    expect(wrapper.find('.home-shell').exists()).toBe(false)
  })

  it('links unauthenticated visitors to login', () => {
    expect(primaryDestination(mountHome())).toBe('/login')
  })

  it('links authenticated users to their dashboard', () => {
    authStore.isAuthenticated = true

    expect(primaryDestination(mountHome())).toBe('/dashboard')
  })

  it('links administrators to the admin dashboard', () => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true

    const wrapper = mountHome()
    expect(primaryDestination(wrapper)).toBe('/admin/dashboard')
    expect(authStore.checkAuth).toHaveBeenCalledOnce()
    expect(announcementStore.fetchAnnouncements).toHaveBeenCalledOnce()
    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })

})
