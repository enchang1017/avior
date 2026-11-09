import { type AsyncComponentLoader, type Component, defineAsyncComponent, h } from 'vue'

import { loaderDelayMs, type LoaderSize } from './loaderDelay'

const VLoader = defineAsyncComponent(() => import('@/components/Feedback/VLoader/VLoader.vue'))

/**
 * A lazily loaded component that says it is loading while its chunk is on the
 * way, where it would otherwise leave a blank. The loader itself is fetched only
 * once the wait has outlasted the delay.
 */
export function lazy<T extends Component>(
  loader: AsyncComponentLoader<T>,
  label: string,
  size: LoaderSize = 'section',
): T {
  const loading = () => h(VLoader, { size, label })
  return defineAsyncComponent<T>({
    loader,
    loadingComponent:
      size === 'inline'
        ? loading
        : () => h('div', { class: 'flex justify-center py-10' }, [loading()]),
    delay: loaderDelayMs,
  })
}
