<script setup lang="ts">
/**
 * What a list says when it holds nothing.
 *
 * A sentence rather than a blank area, because an empty list and a list that
 * failed to load look identical when neither says anything.
 *
 * Announced, because an empty result is a result: somebody who applied a filter
 * and heard nothing cannot tell it from a page that did not respond.
 */
</script>

<template>
  <p class="px-4 py-10 text-center text-sm text-fg-muted" role="status"><slot /></p>
</template>
