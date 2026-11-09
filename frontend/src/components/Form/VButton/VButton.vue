<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import { buttonClasses } from './classes'
import VIcon from '../../Media/VIcon/VIcon.vue'
import type { IconName } from '../../Media/VIcon/icons'

/**
 * Every button on the page.
 *
 * Twenty of them were written by hand across twelve files, in four variants and
 * two sizes, and the sizes had already come apart: an ordering control sat
 * beside a picker a quarter taller than it because one carried btn-sm and the
 * other could not.
 *
 * Some of them are links — going back to a list is navigation, not an action —
 * and a link that looks like a button still has to be a link, or middle-click
 * and open-in-new-tab stop working. Passing `to` makes it one.
 */
const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'ghost' | 'bare'
    size?: 'md' | 'sm'
    /** A leading mark, by its name in the icon registry. */
    icon?: IconName | ''
    /** Marks an action that destroys something, so it reads before it is read. */
    danger?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
    /**
     * Where this goes. Empty means it goes nowhere and is a button.
     *
     * Empty rather than undefined, which is how every other optional string on
     * these components says "not given": the build runs with
     * exactOptionalPropertyTypes, under which a default of undefined is not a
     * default at all.
     */
    to?: RouteLocationRaw
  }>(),
  {
    variant: 'ghost',
    size: 'md',
    icon: '',
    danger: false,
    disabled: false,
    type: 'button',
    to: '',
  },
)

// Declared rather than inferred: a component whose slots are inferred cannot be
// used inside another's slot without vue-tsc failing to name the type it made.
defineSlots<{ default?: () => unknown }>()

const classes = computed(() => buttonClasses(props.variant, props.size, props.danger))
</script>

<template>
  <RouterLink v-if="props.to !== ''" :class="classes" :to="props.to">
    <VIcon v-if="props.icon !== ''" :name="props.icon" :size="props.size === 'sm' ? 'sm' : 'md'" />
    <slot />
  </RouterLink>
  <button v-else :class="classes" :type="props.type" :disabled="props.disabled">
    <VIcon v-if="props.icon !== ''" :name="props.icon" :size="props.size === 'sm' ? 'sm' : 'md'" />
    <slot />
  </button>
</template>
