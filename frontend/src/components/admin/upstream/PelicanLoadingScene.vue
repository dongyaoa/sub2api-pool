<template>
  <div class="pelican-scene absolute inset-0 flex flex-col items-center justify-center gap-2 px-3 py-3" :class="queued && 'is-queued'" role="status" :aria-label="label">
    <svg class="pelican-icon min-h-0 shrink-0" viewBox="0 0 128 94" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path class="paper-corners" d="M4 18V8h10m100 0h10v10M4 76v10h10m100 0h10V76" />
      <g transform="translate(8 8)">
        <circle class="wheel-rim" cx="25" cy="55" r="16" />
        <circle class="wheel-rim" cx="88" cy="55" r="16" />
        <g class="wheel-spokes" style="transform-origin: 25px 55px"><path d="M25 42v26M12 55h26m-22-9 18 18m-18 0 18-18" /></g>
        <g class="wheel-spokes" style="transform-origin: 88px 55px"><path d="M88 42v26M75 55h26m-22-9 18 18m-18 0 18-18" /></g>
        <path class="bike-frame" d="m25 55 21-25 15 25H25l17-17h35l11 17M46 30l-4-6m-6 0h13m39 31-12-31h8" />
        <path class="ink-trace frame-trace" pathLength="100" d="m25 55 21-25 15 25H25l17-17h35l11 17" />
        <circle class="crank" cx="61" cy="55" r="3.5" />
        <path class="pelican-body" d="M43 34c-10 0-16-6-16-14 7 5 12 6 17 3 5-3 10-1 12 3l1-10c0-8 4-13 10-13 6 0 10 4 10 9 0 5-4 8-10 9v7c0 8-6 12-13 11-5 0-8-2-11-5Z" />
        <path class="ink-trace" pathLength="100" d="M43 34c-10 0-16-6-16-14 7 5 12 6 17 3 5-3 10-1 12 3l1-10c0-8 4-13 10-13 6 0 10 4 10 9 0 5-4 8-10 9v7c0 8-6 12-13 11-5 0-8-2-11-5Z" />
        <path class="beak" d="m75 11 27 5-28 5c-5-1-7-3-7-5l8-5Z" />
        <path class="beak-fold" d="m73 16 24 .3" />
        <circle class="eye" cx="71" cy="9" r="1.15" stroke="none" />
        <path class="wing" d="m38 27 8 3 7-.5" />
        <path d="m56 38 8 7-4 10m-4 0h8m-1-29-2 10 10 2" />
      </g>
    </svg>
    <div class="scene-caption flex shrink-0 items-center justify-center gap-2 text-[10px] font-medium leading-4">
      <span>{{ label }}</span><span class="drawing-dots flex items-center gap-[3px]" aria-hidden="true"><i /><i /><i /></span>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ label: string; queued?: boolean }>(), { queued: false })
</script>

<style scoped>
.pelican-scene { --scene-background: #f7faf8; --ink: #275f54; --accent: #39a18b; color: var(--ink); background: var(--scene-background); }
.pelican-icon { width: 124px; max-width: 100%; height: 92px; overflow: visible; }
.paper-corners { stroke: #d8e6dd; stroke-width: 1; }
.wheel-rim { stroke: #397766; }
.wheel-spokes { stroke: #88b0a0; stroke-width: .85; animation: wheels 5.6s linear infinite; }
.bike-frame { stroke: #468e79; }
.pelican-body, .crank { fill: var(--scene-background); }
.ink-trace { stroke: var(--accent); stroke-width: 2; stroke-dasharray: 15 85; stroke-dashoffset: 0; animation: draw-line 4.2s linear infinite; }
.frame-trace { animation-delay: -2.1s; opacity: .75; }
.beak { fill: #f3dfab; stroke: #b29b5f; stroke-width: 1.3; }
.beak-fold { stroke: #c8b47c; stroke-width: .8; }
.eye { fill: var(--ink); }
.wing { stroke: #5b917e; stroke-width: 1.35; }
.scene-caption { color: #4c7c6c; letter-spacing: .025em; }
.drawing-dots i { display: block; width: 3px; height: 3px; border-radius: 50%; background: var(--accent); opacity: .45; animation: drawing-dot 1.8s ease-in-out infinite; }
.drawing-dots i:nth-child(2) { animation-delay: .2s; }
.drawing-dots i:nth-child(3) { animation-delay: .4s; }
.is-queued .wheel-spokes { animation-duration: 11.2s; }
.is-queued .ink-trace { animation-duration: 8.4s; opacity: .5; }
.is-queued .drawing-dots i { background: #c0a36a; animation-duration: 2.6s; }
@keyframes wheels { to { transform: rotate(360deg); } }
@keyframes draw-line { to { stroke-dashoffset: -100; } }
@keyframes drawing-dot { 0%, 70%, 100% { opacity: .35; transform: translateY(0); } 35% { opacity: 1; transform: translateY(-2px); } }
@media (prefers-reduced-motion: reduce) {
  .wheel-spokes, .ink-trace, .drawing-dots i { animation: none; }
  .ink-trace { stroke-dasharray: none; stroke-dashoffset: 0; opacity: .3; }
  .drawing-dots i { opacity: .65; }
}
.dark .pelican-scene { --scene-background: #1e2b26; --ink: #b4d7c6; --accent: #6acdb1; }
.dark .paper-corners { stroke: #354a40; }
.dark .wheel-rim, .dark .bike-frame { stroke: #84bba4; }
.dark .wheel-spokes { stroke: #557f6c; }
.dark .beak { fill: #514833; stroke: #c8b077; }
.dark .beak-fold { stroke: #a59365; }
.dark .wing { stroke: #83b8a0; }
.dark .scene-caption { color: #a1c8b5; }
</style>
