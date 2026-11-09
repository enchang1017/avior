/**
 * The shared components, in one place to import from.
 *
 * Grouped by what they are for rather than by feature, because that is how a
 * view looks for one. Naming follows the `V` prefix convention used across the
 * estate's other Vue codebases, so a component is recognisable as one of these
 * at its call site rather than looked up.
 *
 * This is a directory rather than a package: Avior has one frontend, and a
 * package with one consumer is a build step bought for nothing. If a second
 * ever appears, this directory is what moves.
 */
export { default as VBadge } from './Data/VBadge/VBadge.vue'
export { default as VBarChart } from './Data/VChart/VBarChart.vue'
export { default as VKpiCard } from './Data/VChart/VKpiCard.vue'
export { default as VTimeChart } from './Data/VChart/VTimeChart.vue'
export type {
  BarDatum,
  BarSegment,
  ChartTone,
  Delta,
  Series,
  SeriesPoint,
  TimeChartMode,
  TimeChartScale,
} from './Data/VChart/types'
export { default as VReadout, type ReadoutPart } from './Data/VReadout/VReadout.vue'
export { default as VTable } from './Data/VTable/VTable.vue'
export { type TableVariant } from './Data/VTable/variants'
export { default as VBanner } from './Feedback/VBanner/VBanner.vue'
export { default as VBeacon } from './Feedback/VBeacon/VBeacon.vue'
export { default as VEmpty } from './Feedback/VEmpty/VEmpty.vue'
export { default as VLoader } from './Feedback/VLoader/VLoader.vue'
export { default as VTwinkles } from './Feedback/VTwinkles/VTwinkles.vue'
export { default as VSkeleton } from './Feedback/VSkeleton/VSkeleton.vue'
export { default as VButton } from './Form/VButton/VButton.vue'
export { default as VCheckbox } from './Form/VCheckbox/VCheckbox.vue'
export { default as VDateTime } from './Form/VDateTime/VDateTime.vue'
export { default as VField } from './Form/VField/VField.vue'
export { default as VFilterBar } from './Form/VFilterBar/VFilterBar.vue'
export { default as VInput } from './Form/VInput/VInput.vue'
export { default as VScopeSearch } from './Form/VScopeSearch/VScopeSearch.vue'
export { parseQuery, type ParsedQuery } from './Form/VScopeSearch/scopes'
export { default as VFilterSelect } from './Form/VFilterSelect/VFilterSelect.vue'
export { default as VSelect, type SelectOption } from './Form/VSelect/VSelect.vue'
export { default as VIcon } from './Media/VIcon/VIcon.vue'
export { type IconName, isIconName } from './Media/VIcon/icons'
export { default as VLogo } from './Media/VLogo/VLogo.vue'
export { default as VHeroStar } from './Media/VHeroStar/VHeroStar.vue'
export { default as VStatusIcon, type Status } from './Media/VStatusIcon/VStatusIcon.vue'
export { default as VCard } from './Layout/VCard/VCard.vue'
export { default as VDeepSpace } from './Layout/VDeepSpace/VDeepSpace.vue'
export { default as VConnectionBar } from './Layout/VConnectionBar/VConnectionBar.vue'
export { default as VResultPane, type ResultTab } from './Layout/VResultPane/VResultPane.vue'
export { default as VPageHeader } from './Layout/VPageHeader/VPageHeader.vue'
export { default as VSql } from './Layout/VSql/VSql.vue'
export { default as VToolbar } from './Layout/VToolbar/VToolbar.vue'
export { default as VEditorTab } from './Navigation/VEditorTab/VEditorTab.vue'
export { default as VPager } from './Navigation/VPager/VPager.vue'
export { default as VTabs, type TabOption } from './Navigation/VTabs/VTabs.vue'
export { default as VDialog } from './Overlay/VDialog/VDialog.vue'
export { default as VMenu } from './Overlay/VMenu/VMenu.vue'
export { default as VPalette } from './Overlay/VPalette/VPalette.vue'
export { default as VPopover } from './Overlay/VPopover/VPopover.vue'
export { type Tone, toneAccent, toneLine, toneText, toneWash } from './tone'
export { default as VCostGauge } from './Data/VCostGauge/VCostGauge.vue'
export { default as VSchemaChart } from './Data/VSchemaChart/VSchemaChart.vue'
export { default as VConnectors } from './Data/VConnectors/VConnectors.vue'
export { type Connector } from './Data/VConnectors/types'
export { default as VSkyline } from './Data/VSkyline/VSkyline.vue'
export { default as VSparkline } from './Data/VSparkline/VSparkline.vue'
export { default as VTrajectory } from './Data/VTrajectory/VTrajectory.vue'
