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
        <span class="title">{{ t('model.layout.title') }}</span>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || t('model.layout.user') }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('model.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-sub-menu index="modeling">
            <template #title>
              <el-icon><Box /></el-icon>
              <span>{{ t('model.layout.modeling') }}</span>
            </template>
            <el-menu-item index="/modeling/dw-layers">
              <el-icon><Tickets /></el-icon>
              <span>{{ t('model.layout.dwLayers') }}</span>
            </el-menu-item>
            <el-menu-item index="/modeling/entities">
              <el-icon><Memo /></el-icon>
              <span>{{ t('model.layout.entities') }}</span>
            </el-menu-item>
            <el-menu-item index="/modeling/er-diagram">
              <el-icon><Connection /></el-icon>
              <span>{{ t('model.layout.erDiagram') }}</span>
            </el-menu-item>
            <el-menu-item index="/modeling/logical-tables">
              <el-icon><Operation /></el-icon>
              <span>{{ t('model.layout.logicalTables') }}</span>
            </el-menu-item>
            <el-menu-item index="/modeling/star-schema">
              <el-icon><Star /></el-icon>
              <span>{{ t('model.layout.starSchema') }}</span>
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
  User, ArrowDown, SwitchButton,
  DataAnalysis, Box, Tickets, Memo, Operation, Connection, Star
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
  if (path.startsWith('/standard/domains')) return '/standard/domains'
  if (path.startsWith('/standard/glossaries')) return '/standard/glossaries'
  if (path.startsWith('/standard/elements')) return '/standard/elements'
  if (path.startsWith('/standard/code-sets')) return '/standard/code-sets'
  if (path.startsWith('/modeling/dw-layers')) return '/modeling/dw-layers'
  if (path.startsWith('/modeling/entities')) return '/modeling/entities'
  if (path.startsWith('/modeling/logical-tables')) return '/modeling/logical-tables'
  if (path.startsWith('/modeling/er-diagram')) return '/modeling/er-diagram'
  if (path.startsWith('/modeling/star-schema')) return '/modeling/star-schema'
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
