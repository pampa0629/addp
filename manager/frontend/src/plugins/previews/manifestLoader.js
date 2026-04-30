import { loadCustomPlugins } from './index'

let loaded = false
let loadingPromise = null

const DEFAULT_MANIFEST = '/plugins/manifest.json'

function withBasePath(path) {
  if (!path || typeof path !== 'string') return path
  if (/^(https?:)?\/\//.test(path) || path.startsWith('data:') || path.startsWith('blob:')) {
    return path
  }

  const base = import.meta.env?.BASE_URL || '/'
  if (!base || base === '/') {
    return path
  }

  const normalizedBase = base.endsWith('/') ? base.slice(0, -1) : base
  if (path.startsWith(normalizedBase + '/')) {
    return path
  }
  if (path.startsWith('/')) {
    return normalizedBase + path
  }
  return `${normalizedBase}/${path}`
}

function resolveManifestUrl(manualUrl) {
  if (manualUrl) return withBasePath(manualUrl)
  if (typeof window !== 'undefined' && window.__DATA_EXPLORER_PLUGIN_MANIFEST__) {
    return withBasePath(window.__DATA_EXPLORER_PLUGIN_MANIFEST__)
  }
  if (import.meta.env && import.meta.env.VITE_DATA_EXPLORER_PLUGIN_MANIFEST) {
    return withBasePath(import.meta.env.VITE_DATA_EXPLORER_PLUGIN_MANIFEST)
  }
  return withBasePath(DEFAULT_MANIFEST)
}

async function injectScript(src) {
  return new Promise((resolve) => {
    const script = document.createElement('script')
    script.src = withBasePath(src)
    script.async = true
    script.onload = () => resolve(src)
    script.onerror = () => {
      console.warn(`DataExplorer: 插件脚本加载失败 ${src}`)
      resolve(null)
    }
    document.head.appendChild(script)
  })
}

export async function loadRuntimePlugins(manifestUrl) {
  if (typeof window === 'undefined') return []
  if (loaded) return []
  if (loadingPromise) return loadingPromise

  loadingPromise = (async () => {
    const url = resolveManifestUrl(manifestUrl)
    try {
      const response = await fetch(url, { cache: 'no-cache' })
      if (!response.ok) {
        throw new Error(`请求失败: ${response.status}`)
      }

      const manifest = await response.json()
      const scripts = Array.isArray(manifest?.scripts)
        ? manifest.scripts
        : Array.isArray(manifest)
          ? manifest
          : []

      if (scripts.length) {
        const results = await Promise.allSettled(scripts.map((src) => injectScript(src)))
        const failed = results
          .map((result, index) => ({ result, src: scripts[index] }))
          .filter(({ result }) => result.status === 'rejected' || result.value === null)

        if (failed.length) {
          console.warn(
            'DataExplorer: 以下插件脚本加载失败，将忽略:',
            failed.map(({ src, result }) =>
              `${src}${result?.reason ? ` (${result.reason?.message || result.reason})` : ''}`
            )
          )
        }
      }

      const registered = loadCustomPlugins()
      loaded = true
      return registered
    } catch (error) {
      console.warn('DataExplorer: 加载插件清单失败', error)
      return []
    } finally {
      loadingPromise = null
    }
  })()

  return loadingPromise
}

export default {
  loadRuntimePlugins
}
