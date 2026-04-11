<template>
  <!-- 在 Console 中：只显示内容，不显示导航 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问时：显示完整布局 -->
  <div v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <h1>{{ t('develop.layout.title') }}</h1>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || t('develop.layout.user') }}
            <el-icon class="el-icon--right"><arrow-down /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('develop.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu
          :default-active="$route.path"
          router
          class="sidebar-menu"
        >
          <el-menu-item index="/query">
            <el-icon><Document /></el-icon>
            <span>{{ t('develop.nav.queryEditor') }}</span>
          </el-menu-item>

          <el-menu-item index="/notebook">
            <el-icon><Notebook /></el-icon>
            <span>{{ t('develop.nav.notebook') }}</span>
          </el-menu-item>

          <el-menu-item index="/sql-tasks">
            <el-icon><FolderOpened /></el-icon>
            <span>{{ t('develop.nav.queryTasks') }}</span>
          </el-menu-item>

          <el-menu-item index="/workflow">
            <el-icon><Connection /></el-icon>
            <span>{{ t('develop.nav.workflow') }}</span>
          </el-menu-item>

          <el-menu-item index="/tasks">
            <el-icon><List /></el-icon>
            <span>{{ t('develop.nav.tasks') }}</span>
          </el-menu-item>

          <el-menu-item index="/executions">
            <el-icon><Monitor /></el-icon>
            <span>{{ t('develop.nav.executions') }}</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { useI18n } from 'vue-i18n'
import {
  User,
  ArrowDown,
  SwitchButton,
  Document,
  Notebook,
  FolderOpened,
  Connection,
  List,
  Monitor
} from '@element-plus/icons-vue'

const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()
// 同步初始化，避免 iframe 模式下先渲染完整布局再切换导致子组件重挂载
const isInIframe = ref(window.self !== window.top)

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push('/login')
  }
}
</script>

<style scoped>
/* 内容模式（Console 中） */
.content-only {
  width: 100%;
  height: 100vh;
  overflow: auto;
}

/* 完整布局（独立访问） */
.layout {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.header {
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  height: 60px;
}

.header-left h1 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  color: var(--addp-text-secondary);
  font-size: 14px;
}

.user-dropdown:hover {
  color: var(--el-color-primary);
}

.main-container {
  flex: 1;
  overflow: hidden;
}

.sidebar {
  background: var(--addp-bg-primary);
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
}

.sidebar-menu {
  border-right: none;
}

.content {
  background: var(--addp-bg-secondary);
  overflow: auto;
  padding: 0;
}

/* 自定义滚动条 */
.sidebar::-webkit-scrollbar {
  width: 6px;
}

.sidebar::-webkit-scrollbar-track {
  background: var(--addp-bg-secondary);
}

.sidebar::-webkit-scrollbar-thumb {
  background: var(--addp-border-secondary);
  border-radius: 3px;
}

.sidebar::-webkit-scrollbar-thumb:hover {
  background: var(--addp-text-tertiary);
}
</style>
