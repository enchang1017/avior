<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'

import { isGroup, navigation, visibleTo } from './navigation'
import ShellHud from './ShellHud.vue'
import ShellRail from './ShellRail.vue'
import { isPaletteShortcut, usePalette } from '@/shared/palette'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const route = useRoute()
const palette = usePalette()

/*
 * The palette is a chunk of its own, fetched the first time it is opened
 * (ADR 0009 P4): no route pays for it in its first load.
 */
const loadPalette = () => import('@/components/Overlay/VPalette/VPalette.vue')
const VPalette = defineAsyncComponent(loadPalette)

/*
 * Fetched when the browser is next idle after the first paint, so the first
 * ⌘K opens at once instead of waiting on the network — the chunk stays out of
 * every route's first load (P1, P4) and the palette is still instant (P12).
 */
function preloadPalette(): void {
  if ('requestIdleCallback' in window) window.requestIdleCallback(() => void loadPalette())
  else setTimeout(() => void loadPalette(), 200)
}
const paletteWanted = ref(false)
// The first open only asks for the chunk; once it has mounted, the palette is
// opened for real, so it sees the change it opens on.
watch(palette.open, (open) => {
  if (!open || paletteWanted.value) return
  paletteWanted.value = true
  palette.hide()
})
onMounted(() => void import('@/features/search/register'))

/*
 * A route whose code is still on its way shows the loader (FEAT-074 AC-3). The
 * flag follows the navigation exactly; only the showing waits, in
 * `--animate-pending`, so a fast route never flashes it. The loader is a chunk
 * fetched on the first navigation, so neither the shell nor a page's first
 * load carries it (P1, P3).
 */
const RoutePending = defineAsyncComponent(() => import('./RoutePending.vue'))
const router = useRouter()
const pending = ref(false)
const settle = () => void (pending.value = false)
const stops = [
  router.beforeEach(() => void (pending.value = true)),
  router.afterEach(settle),
  router.onError(settle),
]
onBeforeUnmount(() => stops.forEach((stop) => stop()))

const entries = computed(() =>
  visibleTo(navigation, {
    roles: session.caller?.roles ?? [],
    permissions: session.caller?.permissions ?? [],
    features: session.caller?.features ?? [],
  }),
)

const where = computed((): { section: string; page: string } => {
  for (const entry of entries.value) {
    if (!isGroup(entry) && entry.route === route.name) return { section: '', page: entry.label }
    if (isGroup(entry)) {
      const child = entry.children.find((c) => c.route === route.name)
      if (child !== undefined) return { section: entry.label, page: child.label }
    }
  }
  const own = route.meta['title']
  const [section, page] = typeof own === 'string' ? own.split(' · ') : []
  if (page !== undefined) return { section: section ?? '', page }
  return { section: '', page: section ?? 'Avior' }
})

const production = computed(() => session.caller?.environment === 'production')
/*
 * A page fades in when a different page opens, never when one page shows a
 * different route: the object types share one view whose state (the chosen
 * database) must survive moving between them, so the view is keyed by what it
 * is, not by the address.
 */
function pageKey(component: unknown, path: string): string {
  const type = (component as { type?: { __name?: string; name?: string } } | undefined)?.type
  return type?.__name ?? type?.name ?? path
}

function onKeydown(event: KeyboardEvent): void {
  if (!isPaletteShortcut(event)) return
  event.preventDefault()
  palette.toggle()
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  preloadPalette()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden">
    <!-- Production's hairline: a red line over everything with a light
         travelling along it, on the compositor (ADR 0009 P13, measured 0). -->
    <div
      v-if="production"
      class="beacon-line relative h-px flex-none overflow-hidden bg-env-production-line"
      aria-hidden="true"
    >
      <span class="absolute inset-y-0 left-0 w-1/3 animate-beacon-travel beacon-light" />
    </div>

    <div class="flex min-h-0 flex-1">
      <ShellRail :entries="entries" />

      <div class="relative flex min-w-0 flex-1 flex-col overflow-hidden sky">
        <ShellHud
          :section="where.section"
          :page="where.page"
          :read-only="route.name === 'query-console'"
        />
        <RouterView v-slot="{ Component, route: shown }">
          <div
            :key="pageKey(Component, shown.path)"
            class="route flex min-h-0 min-w-0 flex-1 animate-page flex-col"
          >
            <component :is="Component" />
          </div>
        </RouterView>
        <RoutePending v-if="pending" />
      </div>
    </div>

    <VPalette v-if="paletteWanted" @vue:mounted="palette.show()" />
  </div>
</template>
