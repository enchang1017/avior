<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { caret, controlBox, controlSize, OptionList } from '../controls'
import { useListbox } from '../useListbox'
import VIcon from '../../Media/VIcon/VIcon.vue'

/** One thing that can be chosen. The value is what the caller stores. */
export type SelectOption = { value: string; label: string }

/**
 * Choose one of a known set.
 *
 * The sibling of VFilterSelect, and the difference is the only thing worth
 * saying about either: this one will not accept a value that is not on the
 * list. A verdict is `allow`, `deny` or nothing, and letting somebody type
 * `allowed` would produce a request the server refuses for a reason they would
 * have to guess at.
 *
 * Written rather than styled over a native `select`, because a native one
 * renders its list with the operating system's own widget — a different font,
 * a different highlight, and on this page a jarring one. Typing still filters,
 * since a column picker over a wide table is a closed set with forty things in
 * it.
 */
const props = withDefaults(
  defineProps<{
    id: string
    modelValue: string
    options: readonly SelectOption[]
    disabled?: boolean
    ariaLabel?: string
    /** Matches VButton's, so a row of controls is one height. */
    size?: 'md' | 'sm'
  }>(),
  { disabled: false, size: 'md', ariaLabel: '' },
)

const emit = defineEmits<{ 'update:modelValue': [string] }>()

const field = ref<HTMLInputElement | null>(null)
/** What has been typed since the list opened, which filters it. */
const typed = ref('')

const chosen = computed(() => props.options.find((option) => option.value === props.modelValue))

/** What the closed control reads, which is the label and never the value. */
const shown = computed(() => chosen.value?.label ?? '')

const matches = computed(() => {
  const search = typed.value.trim().toLowerCase()
  if (search === '') return props.options
  return props.options.filter((option) => option.label.toLowerCase().includes(search))
})

function choose(value: string) {
  emit('update:modelValue', value)
  typed.value = ''
  list.close()
}

const list = useListbox(
  () => matches.value.length,
  (index) => {
    const picked = matches.value[index]
    if (picked !== undefined) choose(picked.value)
  },
)
const { open, active } = list

function onInput(event: Event) {
  typed.value = (event.target as HTMLInputElement).value
  list.show()
  active.value = -1
}

function toggle() {
  list.toggle()
  field.value?.focus()
}

// Whatever was typed while looking is discarded on the way out: this control
// stores a choice, and half a word is not one.
watch(open, (isOpen) => {
  if (!isOpen) typed.value = ''
})
</script>

<template>
  <div class="relative flex min-w-0 flex-1 items-center">
    <input
      :id="props.id"
      ref="field"
      :aria-label="props.ariaLabel"
      class="w-full cursor-pointer pr-7 pl-2 font-[inherit]"
      :class="[controlBox, controlSize(props.size)]"
      type="text"
      role="combobox"
      autocomplete="off"
      :disabled="props.disabled"
      :aria-expanded="open"
      :aria-controls="`${props.id}-list`"
      :value="open ? typed : shown"
      :placeholder="shown"
      @input="onInput"
      @focus="list.show()"
      @click="list.show()"
      @blur="list.blur()"
      @keydown.down.prevent="list.move(1)"
      @keydown.up.prevent="list.move(-1)"
      @keydown.enter="list.confirm"
      @keydown.esc="list.escape"
    />
    <button
      type="button"
      :class="[caret, 'enabled:hover:text-fg disabled:cursor-default disabled:opacity-50']"
      tabindex="-1"
      :disabled="props.disabled"
      :aria-label="open ? 'Close the list' : 'Open the list'"
      @mousedown.prevent="toggle"
    >
      <VIcon name="chevron-down" />
    </button>

    <OptionList
      v-if="open"
      :id="`${props.id}-list`"
      :anchor="field"
      :options="matches"
      :value="props.modelValue"
      :active="active"
      @choose="choose"
      @close="list.close"
    />
  </div>
</template>
