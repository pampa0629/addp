<template>
  <!-- Console 嵌入模式：只显示内容 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问模式：显示完整布局 -->
  <el-container v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <el-icon class="logo-icon"><DataAnalysis /></el-icon>
        <span class="title">数据标准与建模</span>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || '用户' }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                退出登录
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-sub-menu index="standard">
            <template #title>
              <el-icon><Document /></el-icon>
              <span>数据标准</span>
            </template>
            <el-menu-item index="/standard/domains">
              <el-icon><Grid /></el-icon>
              <span>业务域管理</span>
            </el-menu-item>
            <el-menu-item index="/standard/glossaries">
              <el-icon><Reading /></el-icon>
              <span>业务术语词典</span>
            </el-menu-item>
            <el-menu-item index="/standard/elements">
              <el-icon><Collection /></el-icon>
              <span>数据元管理</span>
            </el-menu-item>
            <el-menu-item index="/standard/code-sets">
              <el-icon><List /></el-icon>
              <span>码值集管理</span>
            </el-menu-item>
            <el-menu-item index="/standard/units">
              <el-icon><Odometer /></el-icon>
              <span>计量单位</span>
            </el-menu-item>
            <el-menu-item index="/standard/classifications">
              <el-icon><Share /></el-icon>
              <span>分类与分级</span>
            </el-menu-item>
            <el-menu-item index="/standard/dimension-hierarchies">
              <el-icon><SortDown /></el-icon>
              <span>维度层级</span>
            </el-menu-item>
            <el-menu-item index="/standard/metrics">
              <el-icon><TrendCharts /></el-icon>
              <span>指标管理</span>
            </el-menu-item>
            <el-menu-item index="/standard/documents">
              <el-icon><Files /></el-icon>
              <span>标准文档</span>
            </el-menu-item>
          </el-sub-menu>
          <el-sub-menu index="modeling">
            <template #title>
              <el-icon><Box /></el-icon>
              <span>数据建模</span>
            </template>
            <el-menu-item index="/modeling/dw-layers">
              <el-icon><Tickets /></el-icon>
              <span>数仓分层</span>
            </el-menu-item>
            <el-menu-item index="/modeling/entities">
              <el-icon><Memo /></el-icon>
              <span>业务实体</span>
            </el-menu-item>
            <el-menu-item index="/modeling/logical-tables">
              <el-icon><Operation /></el-icon>
              <span>逻辑表设计</span>
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
  User, ArrowDown, SwitchButton, Document,
  Grid, Reading, Collection, DataAnalysis,
  Box, Tickets, Memo, Operation, List,
  Odometer, Share, TrendCharts, Files, SortDown
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/standard/domains')) return '/standard/domains'
  if (path.startsWith('/standard/glossaries')) return '/standard/glossaries'
  if (path.startsWith('/standard/elements')) return '/standard/elements'
  if (path.startsWith('/standard/code-sets')) return '/standard/code-sets'
  if (path.startsWith('/standard/units')) return '/standard/units'
  if (path.startsWith('/standard/classifications')) return '/standard/classifications'
  if (path.startsWith('/standard/metrics')) return '/standard/metrics'
  if (path.startsWith('/standard/documents')) return '/standard/documents'
  if (path.startsWith('/standard/dimension-hierarchies')) return '/standard/dimension-hierarchies'
  if (path.startsWith('/modeling/dw-layers')) return '/modeling/dw-layers'
  if (path.startsWith('/modeling/entities')) return '/modeling/entities'
  if (path.startsWith('/modeling/logical-tables')) return '/modeling/logical-tables'
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
