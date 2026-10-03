<template>
  <article class="pelican-group" data-testid="pelican-group">
    <aside class="pelican-identity">
      <div class="flex items-center gap-2.5">
        <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-primary-100/80 bg-primary-50 text-primary-600 dark:border-primary-500/15 dark:bg-primary-500/10 dark:text-primary-300"><Icon name="lightbulb" size="sm" /></span>
        <div class="min-w-0 flex-1">
          <h2 class="line-clamp-2 break-words text-[15px] font-semibold leading-5 tracking-tight text-gray-900 dark:text-white" :title="group.group_name">{{ group.group_name }}</h2>
          <span class="mt-1 inline-flex items-center gap-1.5 whitespace-nowrap rounded-md px-1.5 py-0.5 text-[10px] font-medium leading-4" :class="statusClass"><span class="h-1 w-1 rounded-full bg-current" :class="active && 'motion-safe:animate-pulse'" />{{ t(`pelicanMonitor.status.${group.latest_run?.status || 'idle'}`) }}</span>
        </div>
      </div>
      <div class="mt-3 grid grid-cols-2 divide-x divide-gray-200/70 rounded-xl border border-gray-100 bg-white/80 py-2 dark:divide-dark-600 dark:border-dark-700 dark:bg-dark-900/30">
        <div class="min-w-0 px-2">
          <p class="text-[10px] leading-4 text-gray-400">{{ t('pelicanMonitor.rate') }}</p>
          <p class="mt-1 break-all text-[17px] font-semibold leading-6 tracking-tight text-primary-700 dark:text-primary-300" data-testid="pelican-rate">{{ rate }}</p>
        </div>
        <div class="min-w-0 px-2">
          <p class="text-[10px] leading-4 text-gray-400">{{ t('pelicanMonitor.next') }}</p>
          <time v-if="group.enabled && countdown && !active" class="mt-1 block font-mono text-base font-semibold leading-6 tabular-nums text-gray-800 dark:text-gray-100" :datetime="group.next_run_at || undefined">{{ countdown }}</time>
          <p v-else class="mt-1 text-[11px] font-medium leading-6" :class="active ? 'text-violet-600 dark:text-violet-300' : 'text-gray-500 dark:text-gray-400'">{{ t(!group.enabled ? 'pelicanMonitor.paused' : active ? 'pelicanMonitor.afterCurrent' : 'pelicanMonitor.waitingSchedule') }}</p>
        </div>
      </div>
      <PelicanModelInfo :model="group.model" :effort="group.reasoning_effort" class="mt-3" data-testid="pelican-plan-model" />
      <p class="mt-2 flex items-center gap-1.5 pl-0 pr-2.5 text-[10px] leading-4 text-gray-500 dark:text-gray-400"><Icon name="clock" size="xs" class="shrink-0 text-primary-600/60 dark:text-primary-400/60" aria-hidden="true" /><span>{{ interval }}</span></p>
      <div class="mt-auto flex flex-wrap items-center justify-between gap-x-2 gap-y-1 pt-3 text-[10px] leading-4">
        <span class="text-gray-400">{{ t('pelicanMonitor.lastRun') }}</span>
        <time :datetime="group.latest_run?.started_at || group.last_run_at || undefined" class="tabular-nums text-gray-500 dark:text-gray-400">{{ pelicanDate(group.latest_run?.started_at || group.last_run_at, locale) }}</time>
      </div>
    </aside>
    <section class="min-w-0 flex-1 p-3.5 lg:p-4">
      <div class="mb-2 flex flex-wrap items-center justify-between gap-2"><div class="flex items-center gap-2"><h3 class="text-xs font-semibold text-gray-800 dark:text-gray-100">{{ t('pelicanMonitor.recentWorks') }}</h3><span class="text-[10px] text-gray-400">{{ t('pelicanMonitor.newestFirst') }}</span></div><span class="text-[10px] tabular-nums text-gray-400">{{ works.filter(run => !pelicanActive(run)).length }}/20</span></div>
      <div v-if="works.length" ref="gallery" class="pelican-gallery" data-testid="pelican-gallery">
        <div v-for="(run, index) in works" :key="run.id" class="pelican-work" :class="index === 0 && 'pelican-work-latest'" :data-run-id="run.id">
          <PelicanArtworkPreview :run="run" class="!min-h-0 !flex-none h-[148px]" @open="emit('open', run)" />
          <button type="button" class="block w-full space-y-1 px-2.5 py-2 text-left transition-colors enabled:cursor-zoom-in enabled:hover:bg-primary-50/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 disabled:cursor-default dark:enabled:hover:bg-primary-500/5" :disabled="run.status !== 'succeeded'" :aria-label="`${t('pelicanMonitor.open')} · ${pelicanDate(run.started_at || run.created_at, locale)}`" :aria-haspopup="run.status === 'succeeded' ? 'dialog' : undefined" @click="emit('open', run)">
            <div class="flex items-center justify-between gap-2">
              <span class="text-[11px] font-semibold" :class="index === 0 ? 'text-primary-600 dark:text-primary-300' : 'text-gray-400'">{{ index === 0 ? t('pelicanMonitor.latest') : `#${run.id}` }}</span>
              <span class="flex items-center gap-1.5" :title="t(`pelicanMonitor.status.${run.status}`)">
                <span v-if="pelicanActive(run)" class="text-[10px] font-medium text-amber-600 dark:text-amber-400">{{ t(`pelicanMonitor.status.${run.status}`) }}</span>
                <span role="img" :aria-label="t(`pelicanMonitor.status.${run.status}`)" class="h-[7px] w-[7px] shrink-0 rounded-full" :class="run.status === 'succeeded' ? 'bg-emerald-500' : run.status === 'failed' ? 'bg-rose-500' : 'bg-amber-400 motion-safe:animate-pulse'" />
              </span>
            </div>
            <time class="block text-[11px] leading-4 tabular-nums text-gray-600 dark:text-gray-300" :datetime="run.started_at || run.created_at">{{ pelicanDate(run.started_at || run.created_at, locale, true) }}</time>
            <div class="flex items-center justify-between gap-2 text-[10px]"><PelicanModelInfo :model="run.model" :effort="run.reasoning_effort" compact /><span v-if="run.duration_ms != null && !pelicanActive(run)" class="shrink-0 font-mono tabular-nums text-gray-600 dark:text-gray-300" :title="t('pelicanMonitor.duration')">{{ pelicanDuration(run) }}</span></div>
          </button>
        </div>
      </div>
      <div v-else class="flex min-h-[202px] flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-gray-200 text-center text-xs text-gray-400 dark:border-dark-700"><Icon name="lightbulb" size="lg" />{{ t('pelicanMonitor.waiting') }}</div>
    </section>
  </article>
</template>
<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PelicanMonitorGroup, PelicanRun } from '@/api/pelicanMonitor'
import Icon from '@/components/icons/Icon.vue'
import PelicanArtworkPreview from './PelicanArtworkPreview.vue'
import PelicanModelInfo from './PelicanModelInfo.vue'
import { pelicanActive, pelicanCountdown, pelicanDate, pelicanDuration, pelicanInterval, pelicanWorks } from './pelicanFormat'
const props = defineProps<{ group: PelicanMonitorGroup; now: number }>()
const emit = defineEmits<{ open: [run: PelicanRun] }>()
const { t, locale } = useI18n()
const gallery = ref<HTMLElement | null>(null)
const works = computed(() => pelicanWorks(props.group))
watch(() => works.value[0]?.id, async (id, previous) => {
  if (!id || !previous || id === previous) return
  await nextTick()
  if (gallery.value) gallery.value.scrollLeft = 0
})
const active = computed(() => pelicanActive(props.group.latest_run))
const countdown = computed(() => pelicanCountdown(props.group.next_run_at, props.now))
const interval = computed(() => { const value = pelicanInterval(props.group.interval_seconds); return value.minutes === undefined ? t('pelicanMonitor.everySeconds', { count: value.seconds }) : t('pelicanMonitor.everyMinutes', { count: value.minutes }) })
const rate = computed(() => typeof props.group.group_rate_multiplier === 'number' && Number.isFinite(props.group.group_rate_multiplier) ? `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 4 }).format(props.group.group_rate_multiplier)}×` : '—')
const statusClass = computed(() => props.group.latest_run?.status === 'failed' ? 'bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-300' : active.value ? 'bg-violet-50 text-violet-600 dark:bg-violet-500/10 dark:text-violet-300' : props.group.latest_run?.status === 'succeeded' ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400')
</script>
<style scoped>
.pelican-group { display:flex; overflow:hidden; min-width:0; border:1px solid rgb(229 231 235); border-radius:16px; background:white; box-shadow:0 2px 8px rgb(15 23 42 / .025); }
.pelican-identity { display:flex; flex-direction:column; flex:0 0 228px; min-width:0; padding:16px 18px; border-right:1px solid rgb(243 244 246); background:linear-gradient(135deg,rgb(240 253 250 / .55),white 65%); }
.pelican-gallery { display:flex; gap:8px; overflow-x:auto; padding:1px 1px 4px; scroll-snap-type:x proximity; scrollbar-width:thin; scrollbar-color:rgb(203 213 225) transparent; }
.pelican-work { flex:0 0 204px; overflow:hidden; border:1px solid rgb(229 231 235); border-radius:12px; scroll-snap-align:start; background:white; }
.pelican-work-latest { border-color:rgb(94 234 212 / .6); }
@media(max-width:767px) { .pelican-group { flex-direction:column; } .pelican-identity { flex-basis:auto; border-right:0; border-bottom:1px solid rgb(243 244 246); padding:14px; } }
:global(.dark .pelican-group),:global(.dark .pelican-work) { background:rgb(24 31 43); border-color:rgb(55 65 81); }
:global(.dark .pelican-identity) { background:linear-gradient(135deg,rgb(20 184 166 / .045),transparent 70%); border-color:rgb(55 65 81); }
:global(.dark .pelican-work-latest) { border-color:rgb(20 184 166 / .35); }
</style>
