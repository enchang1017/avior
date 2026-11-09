import { defineTheme } from '@astryxdesign/core/theme'
import { neutralTheme } from '@astryxdesign/theme-neutral'

import { brand } from './brand'

const sans = '"Geist Variable", ui-sans-serif, system-ui, sans-serif'
const mono = '"Geist Mono Variable", ui-monospace, "SF Mono", Menlo, monospace'

export const aviorTheme = defineTheme({
  name: 'avior',
  extends: neutralTheme,
  tokens: {
    '--font-family-heading': sans,
    '--font-family-body': sans,
    '--font-family-code': mono,
    '--color-accent': brand.amber,
    '--color-text-accent': brand.amber,
    '--color-icon-accent': brand.amber,
    '--color-on-accent': brand.onAmber,
    '--color-background-body': brand.night,
    '--color-background-surface': brand.night2,
    '--color-background-card': brand.night2,
    '--color-background-popover': brand.night3,
    '--color-background-muted': brand.night3,
    '--color-text-primary': brand.ink,
    '--color-text-secondary': brand.inkSoft,
    '--color-border': brand.line,
    '--radius-element': '4px',
    '--radius-container': '8px',
    '--radius-inner': '4px',
  },
})
