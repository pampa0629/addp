<template>
  <!-- Console 嵌入模式：只显示内容 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问模式：显示完整布局 -->
  <el-container v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <el-icon class="logo-icon"><CircleCheck /></el-icon>
        <span class="title">{{ t('quality.layout.title') }}</span>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || t('quality.layout.user') }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('quality.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-sub-menu index="quality">
            <template #title>
              <el-icon><CircleCheck /></el-icon>
              <span>{{ t('quality.layout.qualityManagement') }}</span>
            </template>
            <el-menu-item index="/quality/rule-applications">
              <el-icon><Setting /></el-icon>
              <span>{{ t('quality.layout.ruleApplications') }}</span>
            </el-menu-item>
            <el-menu-item index="/quality/check-tasks">
              <el-icon><List /></el-icon>
              <span>{{ t('quality.layout.checkTasks') }}</span>
            </el-menu-item>
            <el-menu-item index="/quality/executions">
              <el-icon><Clock /></el-icon>
              <span>{{ t('quality.layout.executions') }}</span>
            </el-menu-item>
            <el-menu-item index="/quality/issues">
              <el-icon><Warning /></el-icon>
              <span>{{ t('quality.layout.issues') }}</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import {
  User, ArrowDown, SwitchButton, CircleCheck,
  Setting, List, Clock, Warning
} from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/quality/rule-applications')) return '/quality/rule-applications'
  if (path.startsWith('/quality/check-tasks')) return '/quality/check-tasks'
  if (path.startsWith('/quality/executions')) return '/quality/executions'
  if (path.startsWith('/quality/issues')) return '/quality/issues'
  return path
})

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push('/login')
  }
}
</script>

<style scoped>
.layout {
  height: 100vh;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logo-icon {
  font-size: 22px;
}

.title {
  font-size: 16px;
  font-weight: 600;
}

.header-right {
  display: flex;
  align-items: center;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

.main-container {
  height: calc(100vh - 60px);
}

.sidebar {
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
}

.sidebar-menu {
  border-right: none;
  height: 100%;
}

.content {
  background: var(--addp-bg-secondary) !important;
  overflow-y: auto;
}

.content-only {
  width: 100%;
  height: auto;
  min-height: 100vh;
  padding: 20px;
  margin: 0;
  background: var(--addp-bg-secondary) !important;
  overflow: visible;
  box-sizing: border-box;
}
</style>
