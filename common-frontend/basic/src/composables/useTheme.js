import { ref, computed, watch } from 'vue'
import { THEME_VALUES, getThemeConfig, isThemeDark } from '../config/themes'

/**
 * 主题管理 Composable
 *
 * 功能:
 * 1. 管理主题模式（从 themes.js 配置自动读取）
 * 2. 监听系统主题变化
 * 3. 持久化用户选择到 localStorage
 * 4. 自动应用主题到 DOM
 * 5. 支持跨 iframe 的 postMessage 通信
 *
 * 注意: Element Plus 深色 CSS 应该在各模块的 main.js 中导入
 *
 * @param {Object} options - 配置选项
 * @param {string} options.storageKey - localStorage 键名，默认 'theme-mode'
 * @param {boolean} options.listenToPortal - 是否监听 Portal 的 postMessage，默认 true
 * @param {string} options.portalOrigin - Portal 来源验证，默认 window.location.origin
 * @returns {Object} 主题管理 API
 */
export function useTheme(options = {}) {
  const {
    storageKey = 'theme-mode',
    listenToPortal = true,
    portalOrigin = window.location.origin
  } = options

  // ==================== 状态定义 ====================

  /**
   * 主题模式（从配置文件自动读取支持的主题）
   * @type {Ref<string>}
   */
  const mode = ref(localStorage.getItem(storageKey) || 'system')

  /**
   * 系统主题偏好 (通过 matchMedia 监听)
   * @type {Ref<boolean>}
   */
  const systemPrefersDark = ref(false)

  /**
   * 系统主题监听器清理函数
   * @type {Function | null}
   */
  let cleanupSystemListener = null

  /**
   * Portal 消息监听器清理函数
   * @type {Function | null}
   */
  let cleanupPortalListener = null

  // ==================== 计算属性 ====================

  /**
   * 计算实际应用的主题
   * @returns {boolean} true 表示深色模式, false 表示浅色模式
   */
  const isDark = computed(() => {
    return isThemeDark(mode.value, systemPrefersDark.value)
  })

  // ==================== 方法 ====================

  /**
   * 初始化系统主题监听器
   * @returns {Function} 清理函数 (用于组件卸载)
   */
  const initSystemThemeListener = () => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')

    // 设置初始值
    systemPrefersDark.value = mediaQuery.matches
    console.log('[Theme] 系统主题初始化:', mediaQuery.matches ? '深色' : '浅色')

    // 监听系统主题变化
    const handleChange = (e) => {
      systemPrefersDark.value = e.matches
      console.log('[Theme] 系统主题变化:', e.matches ? '深色' : '浅色')
    }

    // 兼容不同浏览器的监听方法
    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener('change', handleChange)
    } else if (mediaQuery.addListener) {
      // Safari < 14 兼容
      mediaQuery.addListener(handleChange)
    }

    // 返回清理函数
    return () => {
      if (mediaQuery.removeEventListener) {
        mediaQuery.removeEventListener('change', handleChange)
      } else if (mediaQuery.removeListener) {
        mediaQuery.removeListener(handleChange)
      }
    }
  }

  /**
   * 初始化 Portal postMessage 监听器
   * @returns {Function} 清理函数
   */
  const initPortalListener = () => {
    // 检测是否在 iframe 中
    const isInIframe = window.self !== window.top

    if (!isInIframe) {
      console.log('[Theme] 不在 iframe 中，跳过 Portal 监听器初始化')
      return () => {}
    }

    const handleMessage = (event) => {
      // 验证来源：允许来自同一 hostname 的消息（即使端口不同）
      try {
        const eventUrl = new URL(event.origin)
        const currentUrl = new URL(window.location.origin)

        // 只检查 hostname 是否相同，忽略端口差异（开发环境下各模块端口不同）
        if (eventUrl.hostname !== currentUrl.hostname) {
          console.warn('[Theme] 忽略来自不同 hostname 的消息:', event.origin)
          return
        }
      } catch (e) {
        console.warn('[Theme] 无效的来源:', event.origin)
        return
      }

      // 验证消息格式
      if (event.data?.type !== 'theme-change' || event.data?.source !== 'addp-portal') {
        return
      }

      console.log('[Theme] 收到 Portal 主题切换消息:', {
        mode: event.data.mode,
        isDark: event.data.isDark,
        from: event.origin
      })

      // 应用主题（不触发 localStorage 保存，因为 Portal 已经保存了）
      mode.value = event.data.mode
    }

    window.addEventListener('message', handleMessage)
    console.log('[Theme] Portal 消息监听器已启动')

    return () => {
      window.removeEventListener('message', handleMessage)
      console.log('[Theme] Portal 消息监听器已清理')
    }
  }

  /**
   * 应用主题到 DOM
   */
  const applyTheme = () => {
    const html = document.documentElement
    const config = getThemeConfig(mode.value)

    // 移除所有主题 class（从配置文件读取）
    THEME_VALUES.forEach(value => {
      if (value !== 'system' && value !== 'light') {
        html.classList.remove(value)
      }
    })

    // 根据模式添加对应的 class
    if (!config) {
      console.warn('[Theme] 未知主题:', mode.value)
      return
    }

    // system 模式：跟随系统偏好
    if (mode.value === 'system') {
      if (systemPrefersDark.value) {
        html.classList.add('dark')
        console.log('[Theme] 应用系统深色主题')
      } else {
        console.log('[Theme] 应用系统浅色主题')
      }
      return
    }

    // light 模式：不添加任何 class
    if (mode.value === 'light') {
      console.log('[Theme] 应用浅色主题')
      return
    }

    // 其他主题：添加对应的 class
    html.classList.add(mode.value)

    // 如果是深色主题，同时添加 dark class 以触发 Element Plus 深色 CSS
    if (config.isDarkTheme) {
      html.classList.add('dark')
      console.log(`[Theme] 应用 ${config.label}（深色系，同时启用 Element Plus 深色模式）`)
    } else {
      console.log(`[Theme] 应用 ${config.label}`)
    }
  }

  /**
   * 设置主题模式
   * @param {string} newMode - 主题值（从配置文件自动验证）
   */
  const setMode = (newMode) => {
    if (!THEME_VALUES.includes(newMode)) {
      console.error('[Theme] 无效的主题模式:', newMode, '支持的主题:', THEME_VALUES)
      return
    }

    mode.value = newMode
    localStorage.setItem(storageKey, newMode)
    console.log('[Theme] 主题模式设置为:', newMode)
  }

  /**
   * 切换深色/浅色模式 (用于快捷切换按钮)
   * 注意: 此方法会将 mode 设置为 'light' 或 'dark',不会设置为 'system'
   */
  const toggle = () => {
    const newMode = isDark.value ? 'light' : 'dark'
    setMode(newMode)
  }

  /**
   * 初始化主题系统
   */
  const init = () => {
    console.log('[Theme] 初始化主题系统...')

    // 1. 启动系统主题监听
    cleanupSystemListener = initSystemThemeListener()

    // 2. 启动 Portal 消息监听（如果在 iframe 中且启用）
    if (listenToPortal) {
      cleanupPortalListener = initPortalListener()
    }

    // 3. 立即应用当前主题
    applyTheme()

    console.log('[Theme] 主题系统初始化完成', {
      mode: mode.value,
      isDark: isDark.value,
      isInIframe: window.self !== window.top
    })
  }

  /**
   * 清理监听器 (用于组件卸载)
   */
  const cleanup = () => {
    if (cleanupSystemListener) {
      cleanupSystemListener()
      cleanupSystemListener = null
    }
    if (cleanupPortalListener) {
      cleanupPortalListener()
      cleanupPortalListener = null
    }
    console.log('[Theme] 主题系统已清理')
  }

  // ==================== 监听器 ====================

  // 监听 mode 和 systemPrefersDark 变化,自动应用主题
  watch([mode, systemPrefersDark], applyTheme)

  // 注意: 不使用 onUnmounted 自动清理，因为 useTheme 通常在应用级别使用
  // 如果在组件内部使用，可以手动调用 cleanup() 方法

  // ==================== 导出 ====================

  return {
    // 状态
    mode,
    isDark,
    systemPrefersDark,

    // 方法
    setMode,
    toggle,
    init,
    cleanup,
    applyTheme
  }
}
