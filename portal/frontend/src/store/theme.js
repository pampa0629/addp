import { defineStore } from 'pinia'
import { useTheme } from '@common-ui'
import { watch } from 'vue'

/**
 * Portal 主题管理 Store
 *
 * 功能:
 * 1. 使用 common-frontend 的 useTheme composable
 * 2. 监听主题变化并通过 postMessage 通知所有 iframe
 * 3. 保持 Pinia store 结构（为了兼容现有代码）
 */
export const useThemeStore = defineStore('theme', () => {
  // 使用共享的 useTheme composable（不监听 Portal，因为自己就是 Portal）
  const theme = useTheme({
    listenToPortal: false,
    storageKey: 'theme-mode'
  })

  // 初始化主题系统和系统监听
  theme.init()

  // 监听主题变化，广播到所有 iframe
  watch(theme.mode, (newMode) => {
    // 获取所有 iframe
    const iframes = document.querySelectorAll('iframe.module-iframe')

    if (iframes.length === 0) {
      return
    }

    const currentIsDark = theme.isDark.value

    console.log(`[Portal Theme] 广播主题变化到 ${iframes.length} 个 iframe`, {
      mode: newMode,
      isDark: currentIsDark
    })

    iframes.forEach((iframe, index) => {
      try {
        // 从 iframe.src 中提取 origin
        const iframeUrl = new URL(iframe.src)
        const targetOrigin = `${iframeUrl.protocol}//${iframeUrl.host}`

        iframe.contentWindow?.postMessage({
          type: 'theme-change',
          source: 'addp-portal',
          mode: newMode,
          isDark: currentIsDark,
          timestamp: Date.now()
        }, targetOrigin)

        console.log(`[Portal Theme] 已发送消息到 iframe #${index} (${targetOrigin})`)
      } catch (e) {
        console.warn(`[Portal Theme] 发送消息到 iframe #${index} 失败:`, e)
      }
    })
  }, { immediate: true }) // immediate: 确保初始化时也发送一次

  // 导出与原 store 兼容的 API
  return {
    // 状态
    mode: theme.mode,
    isDark: theme.isDark,
    systemPrefersDark: theme.systemPrefersDark,

    // 方法
    setMode: theme.setMode,
    toggle: theme.toggle,
    initSystemThemeListener: theme.init, // 兼容旧 API
    applyTheme: theme.applyTheme
  }
})

