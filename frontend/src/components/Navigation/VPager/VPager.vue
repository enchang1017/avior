<script setup lang="ts">
import VButton from '../../Form/VButton/VButton.vue'
import VIcon from '../../Media/VIcon/VIcon.vue'

/**
 * Previous, which page, next.
 *
 * The page number is shown rather than a total: the endpoints report whether
 * more exists, not how much, and inventing a count would be a number nobody
 * computed.
 *
 * The label is a slot because the two lists say different things — one names
 * the page, the other also says how many rows of how many it is showing — and
 * flattening them to the shorter one would drop what somebody paging through an
 * audit trail is reading.
 */
const props = withDefaults(
  defineProps<{ page: number; canGoBack: boolean; hasMore: boolean; busy?: boolean }>(),
  { busy: false },
)

defineEmits<{ previous: []; next: [] }>()
</script>

<template>
  <div class="flex items-center gap-2 border-t border-line bg-surface px-4 py-2 text-xs">
    <span class="hud text-fg-muted">
      <slot>Page {{ props.page }}</slot>
    </span>
    <div class="flex-1" />
    <VButton
      icon="chevron-left"
      :disabled="!props.canGoBack || props.busy"
      @click="$emit('previous')"
    >
      Previous
    </VButton>
    <VButton :disabled="!props.hasMore || props.busy" @click="$emit('next')">
      Next
      <VIcon name="chevron-right" />
    </VButton>
  </div>
</template>
