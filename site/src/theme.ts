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
    '--color-text-accent': [brand.day.amberText, brand.amber],
    '--color-icon-accent': [brand.day.amberText, brand.amber],
    '--color-on-accent': brand.onAmber,
    '--color-background-body': [brand.day.paper, brand.night],
    '--color-background-surface': [brand.day.paper2, brand.night2],
    '--color-background-card': [brand.day.paper, brand.night2],
    '--color-background-popover': [brand.day.paper, brand.night3],
    '--color-background-muted': [brand.day.paper2, brand.night3],
    '--color-text-primary': [brand.day.ink, brand.ink],
    '--color-text-secondary': [brand.day.inkSoft, brand.inkSoft],
    '--color-border': [brand.day.line, brand.line],
    '--radius-element': '4px',
    '--radius-container': '8px',
    '--radius-inner': '4px',
  },
})
