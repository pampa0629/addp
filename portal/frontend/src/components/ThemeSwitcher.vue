<template>
  <el-dropdown trigger="click" @command="handleCommand">
    <el-button circle :icon="currentIcon" :title="currentTitle" />
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item command="system" :class="{ 'is-active': mode === 'system' }">
          <el-icon><Monitor /></el-icon>
          跟随系统
        </el-dropdown-item>
        <el-dropdown-item command="light" :class="{ 'is-active': mode === 'light' }">
          <el-icon><Sunny /></el-icon>
          浅色模式
        </el-dropdown-item>
        <el-dropdown-item command="dark" :class="{ 'is-active': mode === 'dark' }">
          <el-icon><Moon /></el-icon>
          深色模式
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup>
import { computed } from 'vue'
import { useThemeStore } from '../store/theme'
import { Monitor, Sunny, Moon } from '@element-plus/icons-vue'

const themeStore = useThemeStore()

// 当前主题模式
const mode = computed(() => themeStore.mode)

// 实际应用的主题 (深色或浅色)
const isDark = computed(() => themeStore.isDark)

// 当前显示的图标 (根据实际应用的主题)
const currentIcon = computed(() => {
  return isDark.value ? Moon : Sunny
})

// 按钮标题
const currentTitle = computed(() => {
  const modeText = {
    system: '跟随系统',
    light: '浅色模式',
    dark: '深色模式'
  }[mode.value]

  const actualTheme = isDark.value ? '深色' : '浅色'

  return `当前主题: ${modeText} (${actualTheme})`
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
