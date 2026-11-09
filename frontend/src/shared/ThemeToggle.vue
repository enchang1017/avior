<script setup lang="ts">
import { computed } from 'vue'

import { useTheme } from './theme'
import { VButton, VIcon, type IconName } from '@/components'

/**
 * Night or day, one click. The label names the palette that is showing.
 */
const { theme, toggle } = useTheme()

const face = computed((): { icon: IconName; label: string } =>
  theme.value === 'light' ? { icon: 'sun', label: 'Light' } : { icon: 'moon', label: 'Dark' },
)
</script>

<template>
  <VButton
    variant="bare"
    class="theme"
    :title="`Theme: ${face.label}. Click to change.`"
    :aria-label="`Theme: ${face.label}`"
    @click="toggle"
  >
    <VIcon :name="face.icon" />
  </VButton>
</template>
