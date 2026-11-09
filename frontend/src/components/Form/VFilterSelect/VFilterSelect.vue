<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { caret, controlBox, controlSize, listPanel, optionClass } from '../controls'
import { useListbox } from '../useListbox'
import VIcon from '../../Media/VIcon/VIcon.vue'

/**
 * A filter that offers what exists and still accepts what does not.
 *
 * Written rather than reached for because the two native options each fail one
 * half of the job: a `select` cannot be typed into, and a `datalist` cannot be
 * styled at all, so it arrives as the operating system's own list in the middle
 * of a page that looks nothing like it.
 *
 * Free text is kept on purpose. These are filters, not choices from a closed
 * set: a database or table that no row currently mentions is still a reasonable
 * thing to ask about, and refusing to let somebody type it would make the
 * control narrower than the question.
 */
const props = withDefaults(
  defineProps<{
    id: string
    modelValue: string
    options: readonly string[]
    /** Shown when nothing is chosen, and as the label of the clearing entry. */
    emptyLabel: string
    placeholder?: string
    /** Matches VButton's, so a row of controls is one height. */
    size?: 'md' | 'sm'
    /** False when the options given are already what the text matches. */
    matchTyped?: boolean
  }>(),
  { placeholder: '', size: 'md', matchTyped: true },
)

/**
 * `pick` is a choice made, from the list or with Enter on what was typed, as
 * against `update:modelValue`, which follows every keystroke.
 */
const emit = defineEmits<{ 'update:modelValue': [string]; pick: [string] }>()

const field = ref<HTMLInputElement | null>(null)
const root = ref<HTMLElement | null>(null)

/**
 * What the typed text matches.
 *
 * Matching on a substring rather than a prefix, because these names are
 * qualified — somebody looking for `bet` means `public.bet`, and a prefix match
 * would offer them nothing.
 */
const matches = computed(() => {
  const typed = props.modelValue.trim().toLowerCase()
  if (typed === '' || !props.matchTyped) return props.options
  return props.options.filter((option) => option.toLowerCase().includes(typed))
})

function choose(value: string) {
  emit('update:modelValue', value)
  emit('pick', value)
  list.close()
}

function onEnter(event: KeyboardEvent) {
  if (list.open.value && list.active.value >= 0) {
    list.confirm(event)
    return
  }
  emit('pick', props.modelValue)
  list.close()
}

const list = useListbox(
  () => matches.value.length,
  (index) => {
    const picked = matches.value[index]
    if (picked !== undefined) choose(picked)
  },
)
const { open, active } = list

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLInputElement).value)
  list.show()
  active.value = -1
}

function toggle() {
  list.toggle()
  field.value?.focus()
}

watch(
  () => props.options,
  () => {
    active.value = -1
  },
)
</script>

<template>
  <div ref="root" class="relative flex min-w-0 items-center">
    <input
      :id="props.id"
      ref="field"
      class="w-full pr-7 pl-2 font-[inherit]"
      :class="[controlBox, controlSize(props.size)]"
      type="text"
      role="combobox"
      autocomplete="off"
      :aria-expanded="open"
      :aria-controls="`${props.id}-list`"
      :value="props.modelValue"
      :placeholder="props.placeholder || props.emptyLabel"
      @input="onInput"
      @focus="list.show()"
      @blur="list.blur()"
      @keydown.down.prevent="list.move(1)"
      @keydown.up.prevent="list.move(-1)"
      @keydown.enter="onEnter"
      @keydown.esc="list.close()"
    />
    <button
      type="button"
      :class="[caret, 'hover:text-fg']"
      tabindex="-1"
      :aria-label="open ? 'Close the list' : 'Open the list'"
      @mousedown.prevent="toggle"
    >
      <VIcon name="chevron-down" />
    </button>

    <ul v-if="open" :id="`${props.id}-list`" :class="listPanel" role="listbox">
      <li
        v-if="props.modelValue !== ''"
        :class="optionClass({ kind: 'clear' })"
        role="option"
        :aria-selected="false"
        @mousedown.prevent="choose('')"
      >
        {{ props.emptyLabel }}
      </li>
      <li
        v-for="(option, index) in matches"
        :key="option"
        :class="
          optionClass({
            kind: 'value',
            mono: true,
            active: index === active,
            chosen: option === props.modelValue,
          })
        "
        role="option"
        :aria-selected="option === props.modelValue"
        @mousedown.prevent="choose(option)"
      >
        {{ option }}
      </li>
      <li v-if="matches.length === 0" :class="optionClass({ kind: 'none', wrap: true })">
        Nothing here matches. It will still be asked for.
      </li>
    </ul>
  </div>
</template>
