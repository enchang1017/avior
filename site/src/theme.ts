import { defineTheme } from '@astryxdesign/core/theme'
import { neutralTheme } from '@astryxdesign/theme-neutral'

import { brand } from './brand'

const sans = '"Figtree Variable", ui-sans-serif, system-ui, sans-serif'
const mono = '"Geist Mono Variable", ui-monospace, "SF Mono", Menlo, monospace'

const { day, night } = brand

export const aviorTheme = defineTheme({
  name: 'avior',
  extends: neutralTheme,
  tokens: {
    '--font-family-heading': sans,
    '--font-family-body': sans,
    '--font-family-code': mono,
    '--color-accent': [day.ink, night.ink],
    '--color-on-accent': [day.card, night.paper],
    '--color-text-accent': [day.amberText, brand.amber],
    '--color-icon-accent': [day.amberText, brand.amber],
    '--color-background-body': [day.paper, night.paper],
    '--color-background-surface': [day.card, night.card],
    '--color-background-card': [day.card, night.card],
    '--color-background-popover': [day.card, night.muted],
    '--color-background-muted': [day.muted, night.muted],
    '--color-text-primary': [day.ink, night.ink],
    '--color-text-secondary': [day.inkSoft, night.inkSoft],
    '--color-border': [day.line, night.line],
    '--radius-inner': '0.75rem',
    '--radius-element': '0.875rem',
    '--radius-container': '1.25rem',
    '--radius-page': '2rem',
  },
})
