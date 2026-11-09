<script setup lang="ts">
import VButton from '../VButton/VButton.vue'
import type { IconName } from '../../Media/VIcon/icons'

/**
 * The row of filters above a list, and the button that applies them.
 *
 * A form rather than inputs in a div, so enter applies the filters — which is
 * what somebody who has just typed a table name expects, and what three views
 * each had to remember separately.
 */
const props = withDefaults(
  defineProps<{ busy?: boolean; label?: string; icon?: IconName | '' }>(),
  {
    busy: false,
    label: 'Apply',
    icon: '',
  },
)

defineEmits<{ submit: [] }>()
</script>

<template>
  <form
    class="flex flex-wrap items-end gap-2 border-b border-line bg-surface px-4 py-2.5"
    @submit.prevent="$emit('submit')"
  >
    <slot />
    <VButton type="submit" variant="primary" :icon="props.icon" :disabled="props.busy">
      {{ props.label }}
    </VButton>
  </form>
</template>
