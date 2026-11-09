import { type AsyncComponentLoader, type Component, defineAsyncComponent, h } from 'vue'

import DelayedLoader from '@/shared/DelayedLoader.vue'
import type { LoaderSize } from '@/shared/loaderDelay'

/**
 * A lazily loaded part of the lineage page that shows the loader while it is
 * slow to arrive. The delay and the loader's own lazy import are
 * DelayedLoader's, so the page's chunk holds one import of the loader, not two.
 */
export function lazyPart<T extends Component>(
  loader: AsyncComponentLoader<T>,
  label: string,
  size: LoaderSize = 'section',
): T {
  return defineAsyncComponent<T>({
    loader,
    loadingComponent: () => h(DelayedLoader, { size, label }),
    delay: 0,
  })
}
