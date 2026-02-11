<template>
  <div class="theme-debugger" v-if="visible">
    <div class="debugger-header">
      <h4>🔍 主题调试器</h4>
      <button @click="visible = false" class="close-btn">✕</button>
    </div>
    <div class="debugger-content">
      <div class="debug-item">
        <strong>Dark 模式:</strong>
        <span :class="{ active: isDark }">{{ isDark ? '开启' : '关闭' }}</span>
      </div>
      <div class="debug-item">
        <strong>HTML dark class:</strong>
        <span :class="{ active: hasDarkClass }">{{ hasDarkClass ? '存在' : '不存在' }}</span>
      </div>
      <div class="debug-section">
        <strong>CSS 变量:</strong>
        <div class="var-item">
          <code>--addp-bg-primary:</code>
          <span class="color-box" :style="{ background: bgPrimary }"></span>
          <code>{{ bgPrimary }}</code>
        </div>
        <div class="var-item">
          <code>--addp-bg-secondary:</code>
          <span class="color-box" :style="{ background: bgSecondary }"></span>
          <code>{{ bgSecondary }}</code>
        </div>
        <div class="var-item">
          <code>--addp-text-primary:</code>
          <span class="color-box" :style="{ background: textPrimary }"></span>
          <code>{{ textPrimary }}</code>
        </div>
      </div>
      <div class="debug-section">
        <strong>实际元素背景:</strong>
        <div class="var-item">
          <code>#app:</code>
          <span class="color-box" :style="{ background: appBg }"></span>
          <code>{{ appBg }}</code>
        </div>
        <div class="var-item">
          <code>.data-explorer:</code>
          <span class="color-box" :style="{ background: explorerBg }"></span>
          <code>{{ explorerBg }}</code>
        </div>
      </div>
      <button @click="refresh" class="refresh-btn">刷新数据</button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'

const visible = ref(true)
const isDark = ref(false)
const hasDarkClass = ref(false)
const bgPrimary = ref('')
const bgSecondary = ref('')
const textPrimary = ref('')
const appBg = ref('')
const explorerBg = ref('')

const refresh = () => {
  // 检查主题状态
  isDark.value = localStorage.getItem('theme-mode') === 'dark'
  hasDarkClass.value = document.documentElement.classList.contains('dark')

  // 获取 CSS 变量
  const rootStyle = getComputedStyle(document.documentElement)
  bgPrimary.value = rootStyle.getPropertyValue('--addp-bg-primary').trim()
  bgSecondary.value = rootStyle.getPropertyValue('--addp-bg-secondary').trim()
  textPrimary.value = rootStyle.getPropertyValue('--addp-text-primary').trim()

  // 获取实际元素背景色
  const appEl = document.getElementById('app')
  const explorerEl = document.querySelector('.data-explorer')

  if (appEl) {
    appBg.value = getComputedStyle(appEl).backgroundColor
  }

  if (explorerEl) {
    explorerBg.value = getComputedStyle(explorerEl).backgroundColor
  }

  console.log('[ThemeDebugger] 主题状态:', {
    isDark: isDark.value,
    hasDarkClass: hasDarkClass.value,
    variables: { bgPrimary: bgPrimary.value, bgSecondary: bgSecondary.value },
    elements: { appBg: appBg.value, explorerBg: explorerBg.value }
  })
}

onMounted(() => {
  refresh()
  // 每秒自动刷新
  setInterval(refresh, 1000)
})
</script>

<style scoped>
.theme-debugger {
  position: fixed;
  top: 60px;
  right: 20px;
  width: 400px;
  background: rgba(0, 0, 0, 0.9);
  color: #00ff00;
  border: 2px solid #00ff00;
  border-radius: 8px;
  padding: 16px;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  z-index: 9999;
  box-shadow: 0 4px 12px rgba(0, 255, 0, 0.3);
}

.debugger-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid #00ff00;
}

.debugger-header h4 {
  margin: 0;
  color: #00ff00;
  font-size: 14px;
}

.close-btn {
  background: none;
  border: 1px solid #00ff00;
  color: #00ff00;
  padding: 2px 8px;
  cursor: pointer;
  border-radius: 4px;
  font-size: 12px;
}

.close-btn:hover {
  background: #00ff00;
  color: #000;
}

.debugger-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.debug-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 4px 0;
}

.debug-item strong {
  color: #ffff00;
}

.debug-item span {
  color: #ff6b6b;
}

.debug-item span.active {
  color: #00ff00;
  font-weight: bold;
}

.debug-section {
  border-top: 1px dashed #00ff00;
  padding-top: 8px;
}

.debug-section strong {
  display: block;
  color: #ffff00;
  margin-bottom: 8px;
}

.var-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 11px;
}

.var-item code {
  color: #00ffff;
  font-family: inherit;
}

.color-box {
  width: 30px;
  height: 16px;
  border: 1px solid #00ff00;
  border-radius: 2px;
}

.refresh-btn {
  background: #00ff00;
  color: #000;
  border: none;
  padding: 8px;
  border-radius: 4px;
  cursor: pointer;
  font-weight: bold;
  margin-top: 8px;
}

.refresh-btn:hover {
  background: #00cc00;
}
</style>
