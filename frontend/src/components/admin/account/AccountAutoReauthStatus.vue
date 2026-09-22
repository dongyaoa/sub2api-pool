<template>
  <div class="space-y-0.5 text-xs" aria-live="polite" data-testid="account-auto-reauth-status">
    <template v-if="status">
      <div class="text-gray-500 dark:text-dark-400">{{ t('admin.accounts.autoReauth.bound') }}</div>
      <div :class="getOpenAIAutoReauthStateClass(status)">
        {{ t(getOpenAIAutoReauthStateLabelKey(status)) }}
        <span v-if="status.attempts > 0" class="ml-1">· {{ t('admin.accounts.autoReauth.attempts', { count: status.attempts }) }}</span>
      </div>
    </template>
    <span v-else-if="loadFailed" class="text-amber-700 dark:text-amber-400">{{ t('admin.accounts.autoReauth.rowStatusUnavailable') }}</span>
    <span v-else-if="!loaded" class="text-gray-400">{{ t('admin.accounts.autoReauth.rowStatusLoading') }}</span>
    <span v-else class="text-gray-400 dark:text-dark-500">{{ t('admin.accounts.autoReauth.notBound') }}</span>
    <div v-if="status && loadFailed" class="text-amber-700 dark:text-amber-400">{{ t('admin.accounts.autoReauth.rowStatusUnavailable') }}</div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OpenAIAutoReauthAccount } from '@/api/admin/openaiAutoReauth'
import { getOpenAIAutoReauthStateClass, getOpenAIAutoReauthStateLabelKey } from '@/utils/openaiAutoReauthStatus'

defineProps<{ status?: OpenAIAutoReauthAccount; loaded: boolean; loadFailed: boolean }>()
const { t } = useI18n()
</script>
