import { defineTheme } from '@astryxdesign/core/theme'
import { neutralTheme } from '@astryxdesign/theme-neutral'

import { brand } from './brand'

export const aviorTheme = defineTheme({
  name: 'avior',
  extends: neutralTheme,
  tokens: {
    '--color-accent': ['#1f6fb8', brand.blue],
    '--color-text-accent': ['#1f6fb8', brand.blue],
    '--color-background-body': ['#ffffff', brand.night],
    '--color-background-surface': ['#ffffff', brand.night],
    '--color-background-card': ['#ffffff', brand.nightRaised],
    '--color-background-muted': ['#eef0ea', brand.nightSunken],
    '--color-text-primary': ['#0e1426', brand.ink],
    '--color-text-secondary': ['#3d4560', brand.inkSoft],
    '--color-border': ['#d9dce6', brand.line],
  },
})
