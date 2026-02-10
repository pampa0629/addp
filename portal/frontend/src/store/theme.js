import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'

/**
 * Portal 主题管理 Store
 *
 * 功能:
 * 1. 管理主题模式 (system/light/dark)
 * 2. 监听系统主题变化
 * 3. 持久化用户选择到 localStorage
 * 4. 自动应用主题到 DOM
 */
export const useThemeStore = defineStore('theme', () => {
  // ==================== 状态定义 ====================

  /**
   * 主题模式
   * @type {'system' | 'light' | 'dark'}
   */
  const mode = ref(localStorage.getItem('theme-mode') || 'system')

  /**
   * 系统主题偏好 (通过 matchMedia 监听)
   * @type {boolean}
   */
  const systemPrefersDark = ref(false)

  // ==================== 计算属性 ====================

  /**
   * 计算实际应用的主题
   * @returns {boolean} true 表示深色模式, false 表示浅色模式
   */
  const isDark = computed(() => {
    if (mode.value === 'system') {
      return systemPrefersDark.value
    }
    return mode.value === 'dark'
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

    // 返回清理函数 (用于组件卸载)
    return () => {
      if (mediaQuery.removeEventListener) {
        mediaQuery.removeEventListener('change', handleChange)
      } else if (mediaQuery.removeListener) {
        mediaQuery.removeListener(handleChange)
      }
    }
  }

  /**
   * 应用主题到 DOM
   */
  const applyTheme = () => {
    const html = document.documentElement
    if (isDark.value) {
      html.classList.add('dark')
      console.log('[Theme] 应用深色主题')
    } else {
      html.classList.remove('dark')
      console.log('[Theme] 应用浅色主题')
    }
  }

  /**
   * 设置主题模式
   * @param {string} newMode - 'system' | 'light' | 'dark'
   */
  const setMode = (newMode) => {
    if (!['system', 'light', 'dark'].includes(newMode)) {
      console.error('[Theme] 无效的主题模式:', newMode)
      return
    }

    mode.value = newMode
    localStorage.setItem('theme-mode', newMode)
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

  // ==================== 监听器 ====================

  // 监听 isDark 变化,自动应用主题
  watch(isDark, applyTheme, { immediate: true })

  // ==================== 导出 ====================

  return {
    // 状态
    mode,
    isDark,
    systemPrefersDark,

    // 方法
    setMode,
    toggle,
    initSystemThemeListener,
    applyTheme
  }
})
