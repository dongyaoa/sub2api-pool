<template>
  <div
    class="pelican-scene absolute inset-0 flex flex-col items-center justify-center gap-2 px-3 py-3"
    :class="queued && 'is-queued'"
    role="status"
    :aria-label="label"
  >
    <svg class="pelican-illustration min-h-0 shrink-0" viewBox="0 0 160 104" fill="none" aria-hidden="true">
      <circle class="scene-halo" cx="79" cy="48" r="39" />
      <path class="orbit-track" d="M39 29a44 44 0 0 1 80 10M119 65a44 44 0 0 1-71 19" />
      <path class="orbit-ink" pathLength="100" d="M39 29a44 44 0 0 1 80 10M119 65a44 44 0 0 1-71 19" />
      <circle class="scene-spark spark-one" cx="35" cy="47" r="2" />
      <path class="scene-spark spark-two" d="M116 18v6m-3-3h6" />
      <ellipse class="bird-shadow" cx="77" cy="88" rx="29" ry="3" />
      <g class="pelican-bird">
        <path class="bird-feet" d="m68 74-3 11h-7m22-12 2 12h7" />
        <path
          class="bird-body"
          d="M57 76c-15-1-25-12-26-25 7 5 13 6 20 2l11-6c2-2 2-5 2-9 0-11 6-18 16-18 10 0 17 7 17 15 0 7-5 12-13 14-5 1-6 3-5 7 4 13-5 22-22 20Z"
        />
        <path class="bird-neck" d="M74 35c-4 6-5 12-2 19" />
        <path class="bird-wing" d="M43 58c10-4 21-3 29 4-3 9-20 13-29-4Z" />
        <path class="wing-detail" d="M48 61c6 3 11 4 17 3" />
        <path class="bill-pouch" d="m88 36 40 4-31 10c-9 2-14-4-14-9l5-5Z" />
        <path class="bird-bill" d="m90 32 39 8-43 1c-3 0-4-3-2-5l6-4Z" />
        <path class="bill-highlight" d="m93 35 19 4" />
        <circle class="bird-eye" cx="86" cy="29" r="1.7" />
        <circle class="bird-cheek" cx="82" cy="35" r="2.5" />
      </g>
    </svg>
    <div class="scene-caption flex shrink-0 items-center justify-center gap-2 text-[10px] font-medium leading-4">
      <span class="status-dot" aria-hidden="true" />
      <span>{{ label }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ label: string; queued?: boolean }>(), { queued: false })
</script>

<style scoped>
.pelican-scene {
  --scene-background: #f6faf9;
  --scene-halo: #e8f4ee;
  --scene-track: #d6e9e2;
  --scene-ink: #53776c;
  --scene-accent: #2aa891;
  --bird-white: #fffefa;
  --bird-wing: #dceae0;
  background: radial-gradient(ellipse at 50% 42%, #eef7f2 0, var(--scene-background) 68%);
  color: var(--scene-ink);
}
.pelican-illustration { width: min(76%, 200px); height: auto; max-height: calc(100% - 32px); overflow: visible; }
.scene-halo { fill: var(--scene-halo); }
.orbit-track { stroke: var(--scene-track); stroke-width: 1; stroke-linecap: round; }
.orbit-ink { stroke: var(--scene-accent); stroke-width: 1.4; stroke-linecap: round; stroke-dasharray: 8 92; animation: sketch-orbit 7s linear infinite; opacity: .55; }
.scene-spark { stroke: var(--scene-accent); stroke-width: 1.2; stroke-linecap: round; opacity: .45; }
.spark-one { fill: var(--scene-accent); stroke: none; animation: spark-breathe 3.6s ease-in-out infinite; }
.spark-two { animation: spark-breathe 3.6s ease-in-out -1.8s infinite; }
.bird-shadow { fill: #cbded4; opacity: .45; animation: shadow-breathe 3.6s ease-in-out infinite; }
.pelican-bird { animation: bird-float 3.6s ease-in-out infinite; }
.bird-feet { stroke: #ae9164; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.bird-body { fill: var(--bird-white); stroke: #91b2a2; stroke-width: 1.2; stroke-linejoin: round; }
.bird-neck { stroke: #d7e4da; stroke-width: 1.2; stroke-linecap: round; }
.bird-wing { fill: var(--bird-wing); }
.wing-detail { stroke: #b3cdbd; stroke-width: 1.1; stroke-linecap: round; }
.bill-pouch { fill: #ecd29a; }
.bird-bill { fill: #eab459; }
.bill-highlight { stroke: #f8d993; stroke-width: 1.3; stroke-linecap: round; }
.bird-eye { fill: #36594c; }
.bird-cheek { fill: #f0d6bf; opacity: .55; }
.scene-caption { color: var(--scene-ink); letter-spacing: .045em; }
.status-dot { width: 4px; height: 4px; border-radius: 50%; background: var(--scene-accent); animation: status-breathe 2.4s ease-in-out infinite; }
.is-queued .pelican-bird, .is-queued .bird-shadow { animation: none; }
.is-queued .orbit-ink { animation-duration: 14s; opacity: .3; }
.is-queued .scene-spark { opacity: .2; animation: none; }
.is-queued .status-dot { background: #ad9b75; animation-duration: 4s; }
@keyframes bird-float { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-3px); } }
@keyframes shadow-breathe { 0%, 100% { opacity: .45; } 50% { opacity: .25; } }
@keyframes sketch-orbit { to { stroke-dashoffset: -100; } }
@keyframes spark-breathe { 0%, 100% { opacity: .25; } 50% { opacity: .65; } }
@keyframes status-breathe { 0%, 100% { opacity: .35; } 50% { opacity: 1; } }
@media (prefers-reduced-motion: reduce) {
  .pelican-bird, .bird-shadow, .orbit-ink, .scene-spark, .status-dot { animation: none; }
  .orbit-ink { stroke-dasharray: 8 92; opacity: .45; }
}
.dark .pelican-scene {
  --scene-background: #172723;
  --scene-halo: #223d33;
  --scene-track: #355247;
  --scene-ink: #a7c9bb;
  --scene-accent: #62bfa6;
  --bird-white: #e2ebe0;
  --bird-wing: #abc5b5;
  background: radial-gradient(ellipse at 50% 42%, #1c332b 0, var(--scene-background) 68%);
}
.dark .bird-shadow { fill: #071b13; }
.dark .bird-body { stroke: #91b2a2; }
.dark .bird-neck { stroke: #bdd1c2; }
.dark .wing-detail { stroke: #86aa93; }
.dark .bill-pouch { fill: #cbb47b; }
.dark .bird-bill { fill: #dbad5c; }
.dark .bill-highlight { stroke: #f2d293; }
</style>
