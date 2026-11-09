<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/** One tab: what the model stores, and what the tab says. */
export type TabOption = { id: string; label: string; count?: number }

/**
 * A row of tabs over one panel, of which one is open.
 *
 * Only the strip: the panel under it is the caller's, chosen by the model, so
 * the tabs never have to know what they switch between. The open tab is
 * underlined rather than filled, because the strip sits inside a card and a
 * filled tab reads as a second, competing button row.
 */
const model = defineModel<string>({ required: true })

const props = defineProps<{
  tabs: readonly TabOption[]
  /** What the strip chooses between, for somebody who cannot see it. */
  label: string
}>()

/*
 * One underline that slides to the open tab: a 1px bar scaled to the tab's
 * width and moved to its place, transform only. The tab is measured once per
 * change, which is an interaction, never per frame.
 */
const strip = ref<HTMLDivElement | null>(null)
const underline = ref({ x: 0, width: 0 })
const sliding = ref(false)

function place(): void {
  const active = strip.value?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
  underline.value = active
    ? { x: active.offsetLeft, width: active.offsetWidth }
    : { x: 0, width: 0 }
}

watch(model, async () => {
  await nextTick()
  place()
})
watch(
  () => props.tabs,
  async () => {
    await nextTick()
    place()
  },
)
// A tab changes width when its face arrives or its count changes, and the
// underline follows it there rather than keeping the width it was measured at.
let resized: ResizeObserver | null = null
onMounted(() => {
  place()
  requestAnimationFrame(() => (sliding.value = true))
  if (typeof ResizeObserver === 'function' && strip.value !== null) {
    resized = new ResizeObserver(place)
    for (const tab of strip.value.querySelectorAll('[role="tab"]')) resized.observe(tab)
  }
  void document.fonts?.ready.then(place)
})
onBeforeUnmount(() => resized?.disconnect())
</script>

<template>
  <div
    ref="strip"
    class="relative flex gap-4 border-b border-line"
    role="tablist"
    :aria-label="props.label"
  >
    <button
      v-for="tab in props.tabs"
      :key="tab.id"
      type="button"
      role="tab"
      class="tab inline-flex h-9 cursor-pointer items-center justify-center gap-1.5 rounded-none border-0 bg-transparent px-0.5 font-[inherit] text-sm leading-snug whitespace-nowrap no-underline ease-colours hover:text-fg"
      :class="model === tab.id ? 'active font-medium text-primary' : 'font-medium text-fg-soft'"
      :aria-selected="model === tab.id"
      @click="model = tab.id"
    >
      {{ tab.label }}
      <span
        v-if="tab.count !== undefined"
        class="count rounded-pill bg-surface-sunken px-1.5 font-mono text-2xs text-fg-soft"
        >{{ tab.count }}</span
      >
    </button>
    <span
      class="tab-bar pointer-events-none absolute bottom-0 left-0 h-0.5 w-px origin-left bg-primary"
      :class="sliding ? 'transition-transform duration-200 ease-out' : ''"
      :style="{ transform: `translateX(${underline.x}px) scaleX(${underline.width})` }"
      aria-hidden="true"
    />
  </div>
</template>
