<template>
  <div ref="badgeRef" class="relative" @keydown.esc="dropdownOpen = false">
    <template v-if="isAdmin">
      <button
        type="button"
        data-testid="version-badge"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs transition-colors"
        :class="hasUpdate ? 'bg-amber-100 text-amber-700 hover:bg-amber-200 dark:bg-amber-900/30 dark:text-amber-400' : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-800 dark:text-dark-400'"
        :title="versionSummary"
        :aria-expanded="dropdownOpen"
        @click="toggleDropdown"
      >
        <span class="font-medium">{{ currentVersion ? 'v' + currentVersion : '—' }}</span>
        <span v-if="hasUpdate" class="h-2 w-2 rounded-full bg-amber-500"></span>
        <Icon v-if="monitoring || submitting" name="refresh" size="xs" class="animate-spin" />
      </button>
      <transition name="dropdown">
        <div
          v-if="dropdownOpen"
          data-testid="version-dropdown"
          class="absolute left-0 z-50 mt-2 w-80 max-w-[calc(100vw-2rem)] overflow-hidden whitespace-normal rounded-xl border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <span class="text-sm font-medium text-gray-700 dark:text-dark-300">{{ t('version.currentVersion') }}</span>
            <button
              type="button"
              data-testid="refresh-version"
              class="rounded-lg p-1.5 text-gray-500 hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700"
              :disabled="appStore.versionLoading || authStopped"
              :title="t('version.refresh')"
              :aria-label="t('version.refresh')"
              @click="refreshAll(true, true)"
            ><Icon name="refresh" size="sm" :class="{ 'animate-spin': appStore.versionLoading }" /></button>
          </div>
          <div class="space-y-3 p-4">
            <div class="text-center">
              <p class="text-2xl font-bold text-gray-900 dark:text-white">{{ currentVersion ? 'v' + currentVersion : '—' }}</p>
              <p data-testid="version-summary" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ versionSummary }}</p>
            </div>
            <div v-if="hasUpdate && !busy && !completed" data-testid="update-available" class="flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 p-3 text-amber-600 dark:border-amber-700/50 dark:bg-amber-900/20 dark:text-amber-400">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40"><Icon name="download" size="md" /></span>
              <div><p class="text-sm font-semibold">{{ t('version.updateAvailable') }}</p><p class="mt-0.5 text-xs">v{{ appStore.latestVersion }}</p></div>
            </div>
            <p v-if="versionWarning" class="break-words text-xs text-amber-700 dark:text-amber-400">{{ versionWarning }}</p>
            <div v-if="job || busy" data-testid="update-job" class="space-y-1 rounded-lg bg-gray-50 p-3 dark:bg-dark-700/50" role="status" aria-live="polite">
              <p class="text-sm font-medium" :class="jobFailed ? 'text-red-600 dark:text-red-400' : completed ? 'text-green-600 dark:text-green-400' : 'text-gray-700 dark:text-dark-200'">{{ jobLabel }}</p>
              <p v-if="job" class="text-xs text-gray-500 dark:text-dark-400">v{{ job.version }}</p>
            </div>
            <p v-if="connectionLost" data-testid="update-reconnecting" class="text-xs text-amber-700 dark:text-amber-400">{{ t('version.reconnecting') }}</p>
            <p v-if="updateError" data-testid="update-error" class="break-words text-xs text-red-600 dark:text-red-400" role="alert">{{ updateError }}</p>
            <div v-if="unavailableReason && !busy && !timedOut" data-testid="update-unavailable" class="space-y-1 rounded-lg bg-blue-50 p-3 text-xs text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">
              <p>{{ unavailableReason }}</p>
              <a v-if="needsSetup" :href="POOL_SETUP_URL" target="_blank" rel="noopener noreferrer" class="underline">{{ t('version.setupInstructions') }}</a>
            </div>
            <button
              v-if="canUpdate || busy"
              type="button"
              data-testid="update-now"
              class="flex w-full items-center justify-center gap-2 rounded-xl bg-primary-500 px-4 py-3 text-sm font-semibold text-white transition-colors hover:bg-primary-600 disabled:cursor-wait disabled:opacity-70"
              :disabled="busy"
              @click="requestUpdate"
            ><Icon :name="busy ? 'refresh' : 'download'" size="sm" :class="{ 'animate-spin': busy }" />{{ busy ? jobLabel : t('version.updateNow') }}</button>
            <button v-if="timedOut" type="button" data-testid="retry-update-status" class="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-700" @click="refreshAll(true, true)">{{ t('version.checkAgain') }}</button>
            <a :href="releaseURL" target="_blank" rel="noopener noreferrer" class="flex items-center justify-center gap-1 text-xs text-gray-500 hover:underline dark:text-dark-400">
              {{ t('version.viewChangelog') }}<Icon name="externalLink" size="xs" />
            </a>
          </div>
        </div>
      </transition>
      <ConfirmDialog
        :show="confirmation !== null"
        :title="t('version.confirmTitle')"
        :message="t(appStore.versionInfo?.update_method === 'container' ? 'version.confirmContainerUpdate' : 'version.confirmProgramUpdate', { version: confirmation?.version || '' })"
        :confirm-text="t('version.updateNow')"
        @cancel="confirmation = null"
        @confirm="submitUpdate"
      />
    </template>
    <span v-else-if="currentVersion" class="text-xs text-gray-500 dark:text-dark-400">v{{ currentVersion }}</span>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import { useSystemUpdate } from '@/composables/useSystemUpdate'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from './ConfirmDialog.vue'

const POOL_RELEASES_URL = 'https://github.com/dongyaoa/sub2api-pool/releases'
const POOL_SETUP_URL = 'https://github.com/dongyaoa/sub2api-pool/blob/main/deploy/POOL_ONLINE_UPDATE.md'
const props = defineProps<{ version?: string }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.isAdmin)
const badgeRef = ref<HTMLElement | null>(null)
const dropdownOpen = ref(false)
const {
  confirmation, job, busy, submitting, monitoring, completed, connectionLost, updateError, authStopped, timedOut,
  versionKnown, hasUpdate, jobFailed, jobLabel, unavailableReason, needsSetup, versionWarning, canUpdate,
  refreshAll, requestUpdate, submitUpdate
} = useSystemUpdate(isAdmin)
const currentVersion = computed(() => isAdmin.value
  ? appStore.currentVersion || props.version || appStore.siteVersion
  : props.version || appStore.siteVersion)
const versionSummary = computed(() => appStore.versionLoading && !versionKnown.value ? t('version.checking')
  : !versionKnown.value ? t('version.checkUnknown')
    : hasUpdate.value ? t('version.latestVersion') + ': v' + appStore.latestVersion : t('version.upToDate'))
const releaseURL = computed(() => {
  // Only this repository's version tags are allowed; API-provided URLs are never rendered.
  const match = /^(\d+\.\d+\.\d+)-pool\.(\d+)$/.exec(appStore.latestVersion)
  return match ? `${POOL_RELEASES_URL}/tag/pool-v${match[1]}.${match[2]}` : POOL_RELEASES_URL
})
function toggleDropdown() {
  dropdownOpen.value = !dropdownOpen.value
  if (dropdownOpen.value) void refreshAll(true)
}
function handleClickOutside(event: MouseEvent) {
  if (badgeRef.value && !badgeRef.value.contains(event.target as Node)) dropdownOpen.value = false
}
watch(isAdmin, () => { dropdownOpen.value = false })
onMounted(() => { document.addEventListener('click', handleClickOutside) })
onBeforeUnmount(() => { document.removeEventListener('click', handleClickOutside) })
</script>

<style scoped>
.dropdown-enter-active, .dropdown-leave-active { transition: all 0.2s ease; }
.dropdown-enter-from, .dropdown-leave-to { opacity: 0; transform: scale(0.95) translateY(-4px); }
</style>
