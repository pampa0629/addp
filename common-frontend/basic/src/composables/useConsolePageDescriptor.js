import { unref, watch } from 'vue'
import { buildConsoleModuleRoute } from '../utils/moduleRouteNavigation'
import { syncConsoleRoute } from '../utils/taskOwnerUrl'

function read(value) {
  return unref(value)
}

export function useConsolePageDescriptor(router, moduleName, options) {
  if (!router?.currentRoute) throw new Error('Vue Router instance is required')
  if (!options || typeof options !== 'object') throw new Error('page descriptor options are required')

  return watch(
    () => ({
      fullPath: router.currentRoute.value.fullPath,
      title: String(read(options.title) || '').trim(),
      subject: String(read(options.subject) || '').trim(),
      ready: read(options.ready) !== false,
      recent: read(options.recent) !== false
    }),
    async descriptor => {
      if (!descriptor.ready || !descriptor.title) return
      try {
        await publishConsolePageDescriptor(router, moduleName, descriptor)
      } catch {
        // standalone 模式或 Console 已离开当前 iframe 时不需要同步页面描述。
      }
    },
    { immediate: true }
  )
}

export function publishConsolePageDescriptor(router, moduleName, descriptor) {
  const fullPath = descriptor?.fullPath || router?.currentRoute?.value?.fullPath
  if (!fullPath || !String(descriptor?.title || '').trim()) return false
  return syncConsoleRoute(buildConsoleModuleRoute(moduleName, fullPath), {
    pageDescriptor: {
      title: String(descriptor.title).trim(),
      subject: String(descriptor.subject || '').trim(),
      recent: descriptor.recent !== false
    }
  })
}
