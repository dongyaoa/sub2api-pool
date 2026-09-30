<template>
  <div class="absolute inset-0 flex flex-col items-center justify-center gap-4 overflow-hidden bg-gradient-to-br from-teal-50 via-white to-violet-50 dark:from-teal-950/20 dark:via-dark-900 dark:to-violet-950/20" role="status">
    <div class="relative flex h-16 w-16 items-center justify-center rounded-2xl border border-white bg-white/90 text-primary-500 shadow-sm dark:border-dark-600 dark:bg-dark-800">
      <svg viewBox="0 0 48 48" class="h-10 w-10" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><circle cx="12" cy="34" r="8" /><circle cx="37" cy="34" r="8" /><path d="m12 34 10-16 8 16H12l8-10h12l5 10M28 13l4 11M24 20c-10-2-11-10-6-14 5-3 11 0 12 5l12 2-13 5-5-1v3Z" /><circle cx="25" cy="10" r=".8" fill="currentColor" /><path d="m20 20 5 7-5 6" /></svg>
      <span v-if="!queued" class="pelican-wait absolute -inset-1 rounded-[20px] border border-primary-300/70" />
    </div>
    <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</span>
    <div v-if="!queued" class="h-0.5 w-20 overflow-hidden rounded-full bg-primary-100 dark:bg-primary-900/30"><div class="pelican-progress h-full w-8 rounded-full bg-primary-400" /></div>
  </div>
</template>
<script setup lang="ts">
defineProps<{ label: string; queued?: boolean }>()
</script>
<style scoped>
.pelican-wait { animation: pelican-breathe 2.5s ease-in-out infinite; }
.pelican-progress { animation: pelican-travel 1.8s ease-in-out infinite alternate; }
@keyframes pelican-breathe { 50% { opacity: .25; transform: scale(1.06); } }
@keyframes pelican-travel { to { transform: translateX(48px); } }
@media(prefers-reduced-motion:reduce) { .pelican-wait,.pelican-progress { animation:none; } }
</style>
