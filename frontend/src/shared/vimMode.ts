import { ref, watch } from 'vue'

/**
 * FEAT-057. Whether the query editor takes Vim keybindings.
 *
 * A personal preference, kept in this browser and never sent to the server: how
 * somebody types a statement is not something the gateway has an opinion about,
 * and it is not part of what gets audited.
 *
 * Off by default. Modal editing is a thing you opt into — an operator who has
 * never chosen must find the editor accepting text.
 */
const storageKey = 'avior.vim'

export function read(): boolean {
  try {
    return window.localStorage.getItem(storageKey) === 'on'
  } catch {
    // A private window, cleared site data, or a browser set to block storage.
    // A keybinding preference is not worth a blank page.
    return false
  }
}

function write(on: boolean): void {
  try {
    window.localStorage.setItem(storageKey, on ? 'on' : 'off')
  } catch {
    // A preference that cannot be remembered still applies for this session.
  }
}

/** The modes the indicator can show. Empty means Vim is off. */
export type VimMode = '' | 'normal' | 'insert' | 'visual' | 'replace'

export function useVimMode() {
  const enabled = ref(read())
  /**
   * The mode as the editor reports it, for the indicator.
   *
   * Held here rather than in the editor component because what it is for is
   * being visible outside the editor: somebody who cannot type has to see why
   * without knowing that a mode exists (AC-4).
   */
  const mode = ref<VimMode>('')

  watch(enabled, (on) => {
    write(on)
    if (!on) mode.value = ''
  })

  return { enabled, mode }
}

/** label is what the indicator reads, in the form Vim itself uses. */
export function label(mode: VimMode): string {
  return mode === '' ? '' : `-- ${mode.toUpperCase()} --`
}
