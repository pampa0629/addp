<template>
  <!-- Console 嵌入模式：只显示内容 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问模式：显示完整布局 -->
  <el-container v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <el-icon class="logo-icon"><Folder /></el-icon>
        <span class="title">{{ t('asset.layout.title') }}</span>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || t('asset.layout.user') }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('asset.layout.logout') }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-sub-menu index="asset-catalog">
            <template #title>
              <el-icon><Folder /></el-icon>
              <span>{{ t('asset.layout.assetCatalog') }}</span>
            </template>
            <el-menu-item index="/asset/type-definitions">
              <el-icon><Grid /></el-icon>
              <span>{{ t('asset.layout.assetTypes') }}</span>
            </el-menu-item>
            <el-menu-item index="/asset/categories">
              <el-icon><Files /></el-icon>
              <span>{{ t('asset.layout.catalogManagement') }}</span>
            </el-menu-item>
            <el-menu-item index="/asset/assets">
              <el-icon><List /></el-icon>
              <span>{{ t('asset.layout.assetWorkbench') }}</span>
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="asset-apply">
            <template #title>
              <el-icon><Document /></el-icon>
              <span>{{ t('asset.layout.applicationAndAuth') }}</span>
            </template>
            <el-menu-item index="/asset/applications">
              <el-icon><Tickets /></el-icon>
              <span>{{ t('asset.layout.applicationAndAuth') }}</span>
            </el-menu-item>
          </el-sub-menu>

          <el-menu-item index="/asset/dashboard">
            <el-icon><DataAnalysis /></el-icon>
            <span>{{ t('asset.layout.dashboard') }}</span>
          </el-menu-item>
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
import { useI18n } from 'vue-i18n'
import {
  User, ArrowDown, SwitchButton, Folder,
  Grid, Files, List, Document, Tickets, DataAnalysis
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { t } = useI18n()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/asset/type-definitions')) return '/asset/type-definitions'
  if (path.startsWith('/asset/categories')) return '/asset/categories'
  if (path.startsWith('/asset/assets')) return '/asset/assets'
  if (path.startsWith('/asset/applications')) return '/asset/applications'
  if (path.startsWith('/asset/dashboard')) return '/asset/dashboard'
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
  min-height: 0;
  padding: 20px;
  margin: 0;
  background: var(--addp-bg-secondary) !important;
  overflow: visible;
  box-sizing: border-box;
}
</style>
