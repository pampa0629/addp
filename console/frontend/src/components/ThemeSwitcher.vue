<template>
  <el-dropdown trigger="click" @command="handleCommand">
    <el-button circle :icon="currentIcon" :title="currentTitle" />
    <template #dropdown>
      <el-dropdown-menu>
        <!-- 自动从配置生成菜单项 -->
        <el-dropdown-item
          v-for="theme in themes"
          :key="theme.value"
          :command="theme.value"
          :class="{ 'is-active': mode === theme.value }"
        >
          <el-icon><component :is="icons[theme.icon]" /></el-icon>
          {{ theme.label }}
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup>
import { computed } from 'vue'
import { useThemeStore } from '../store/theme'
import { THEME_CONFIGS, getThemeConfig } from '@common-ui/config/themes'
import { Monitor, Sunny, Moon, Cloudy, MagicStick } from '@element-plus/icons-vue'

// 图标映射
const icons = {
  Monitor,
  Sunny,
  Moon,
  Cloudy,
  MagicStick
}

// 主题配置列表
const themes = THEME_CONFIGS

const themeStore = useThemeStore()

// 当前主题模式
const mode = computed(() => themeStore.mode)

// 实际应用的主题 (深色或浅色)
const isDark = computed(() => themeStore.isDark)

// 当前显示的图标 (根据当前主题配置)
const currentIcon = computed(() => {
  const config = getThemeConfig(mode.value)
  return config ? icons[config.icon] : icons.Sunny
})

// 按钮标题
const currentTitle = computed(() => {
  const config = getThemeConfig(mode.value)
  if (!config) return '主题切换'

  const actualTheme = config.isDarkTheme === null
    ? (isDark.value ? '深色' : '浅色')
    : (config.isDarkTheme ? '深色' : '浅色')

  return `当前主题: ${config.label} (${actualTheme})`
})

// 处理下拉菜单选择
const handleCommand = (command) => {
  themeStore.setMode(command)
}
</script>

<style scoped>
.is-active {
  background-color: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
</style>
