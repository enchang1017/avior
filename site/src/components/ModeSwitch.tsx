import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { Monitor, Moon, Sun } from 'lucide-react'

import { isColorMode, type ColorMode } from '../colorMode'

type ModeSwitchProps = {
  mode: ColorMode
  onChange: (mode: ColorMode) => void
}

export function ModeSwitch({ mode, onChange }: ModeSwitchProps) {
  return (
    <SegmentedControl
      size="sm"
      label="Color mode"
      value={mode}
      onChange={(value) => {
        if (isColorMode(value)) onChange(value)
      }}
    >
      <SegmentedControlItem value="light" label="Light" isLabelHidden icon={<Sun size={15} strokeWidth={1.75} />} />
      <SegmentedControlItem value="dark" label="Dark" isLabelHidden icon={<Moon size={15} strokeWidth={1.75} />} />
      <SegmentedControlItem value="system" label="System" isLabelHidden icon={<Monitor size={15} strokeWidth={1.75} />} />
    </SegmentedControl>
  )
}
