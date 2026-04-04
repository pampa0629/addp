<template>
  <el-header class="header">
    <div class="header-left">
      <div class="logo-area" @click="$emit('logo-click')">
        <el-icon :size="28" style="margin-right: 12px"><Platform /></el-icon>
        <h1>全域数据平台</h1>
      </div>

      <nav class="group-tabs">
        <el-tooltip
          v-for="group in groups"
          :key="group.key"
          :content="group.label"
          placement="bottom"
          :show-after="200"
        >
          <button
            class="group-tab"
            :class="{ 'is-active': activeGroup === group.key }"
            @click="$emit('group-click', group)"
          >
            <el-icon :size="20"><component :is="group.icon" /></el-icon>
          </button>
        </el-tooltip>
      </nav>
    </div>

    <div class="header-right">
      <ThemeSwitcher style="margin-right: 16px;" />

      <el-dropdown>
        <span class="user-dropdown">
          <el-icon><User /></el-icon>
          {{ user?.username || 'User' }}
          <el-icon class="el-icon--right"><ArrowDown /></el-icon>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="$emit('logout')">
              <el-icon><SwitchButton /></el-icon>
              退出登录
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </el-header>
</template>

<script setup>
import { Platform, User, ArrowDown, SwitchButton } from '@element-plus/icons-vue'
import ThemeSwitcher from '../ThemeSwitcher.vue'

defineProps({
  groups: { type: Array, required: true },
  activeGroup: { type: String, default: null },
  user: { type: Object, default: null },
})

defineEmits(['group-click', 'logo-click', 'logout'])
</script>

<style scoped>
.header {
  background: var(--addp-bg-header);
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
}

.logo-area {
  display: flex;
  align-items: center;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: background 0.2s;
  margin-right: 16px;
  flex-shrink: 0;
}

.logo-area:hover {
  background: var(--addp-bg-secondary);
}

.logo-area h1 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
  background: var(--addp-primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  white-space: nowrap;
}

.group-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}

.group-tabs::-webkit-scrollbar {
  display: none;
}

.group-tab {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--addp-text-secondary);
  cursor: pointer;
  border-radius: 8px;
  transition: background 0.2s, color 0.2s;
  flex-shrink: 0;
}

.group-tab:hover {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
}

.group-tab.is-active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.header-right {
  display: flex;
  align-items: center;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: 4px;
  transition: background 0.3s;
}

.user-dropdown:hover {
  background: var(--addp-bg-secondary);
}
</style>
