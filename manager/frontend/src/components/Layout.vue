<template>
  <!-- 当在 iframe 中时，只显示内容区域 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问时，显示完整布局 -->
  <el-container v-else class="layout-container">
    <el-header class="header">
      <div class="header-left">
        <el-icon :size="24" style="margin-right: 10px">
          <DataAnalysis />
        </el-icon>
        <h1>{{ t('manager.layout.title') }}</h1>
      </div>
      <div class="header-right">
        <el-dropdown>
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ user?.username || 'User' }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="handleLogout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('manager.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="body-container">
      <el-aside width="200px" class="sidebar">
        <el-menu
          :default-active="activeMenu"
          router
          class="el-menu-vertical"
        >
          <el-menu-item index="/data-explorer">
            <el-icon><Search /></el-icon>
            <span>{{ t('manager.layout.dataExplorer') }}</span>
          </el-menu-item>
          <el-menu-item index="/data-retrieval">
            <el-icon><Document /></el-icon>
            <span>{{ t('manager.layout.dataRetrieval') }}</span>
          </el-menu-item>
          <el-menu-item index="/vectorization-tasks">
            <el-icon><List /></el-icon>
            <span>{{ t('manager.layout.vectorizationTasks') }}</span>
          </el-menu-item>
          <el-sub-menu index="/spatial-quick-view">
            <template #title>
              <el-icon><MapLocation /></el-icon>
              <span>{{ t('manager.layout.spatialQuickView') }}</span>
            </template>
            <el-menu-item index="/spatial-quick-view/vector-materialized-view">
              <el-icon><MagicStick /></el-icon>
              <span>{{ t('manager.layout.vectorMaterializedView') }}</span>
            </el-menu-item>
            <el-menu-item index="/spatial-quick-view/vector-tile-cache">
              <el-icon><Grid /></el-icon>
              <span>{{ t('manager.layout.vectorTileCache') }}</span>
            </el-menu-item>
            <el-menu-item index="/spatial-quick-view/raster-cog">
              <el-icon><Picture /></el-icon>
              <span>{{ t('manager.layout.rasterCOG') }}</span>
            </el-menu-item>
            <el-menu-item index="/spatial-quick-view/raster-mosaic">
              <el-icon><Grid /></el-icon>
              <span>{{ t('manager.layout.rasterMosaic') }}</span>
            </el-menu-item>
            <el-menu-item index="/spatial-quick-view/cad-preview">
              <el-icon><MapLocation /></el-icon>
              <span>{{ t('manager.layout.cadPreview') }}</span>
            </el-menu-item>
            <el-menu-item index="/model-3d-glb">
              <el-icon><Box /></el-icon>
              <span>{{ t('manager.layout.model3DGLB') }}</span>
            </el-menu-item>
            <el-menu-item index="/model-3d-tiles">
              <el-icon><Grid /></el-icon>
              <span>{{ t('manager.layout.model3DTiles') }}</span>
            </el-menu-item>
            <el-menu-item index="/gaussian-splat-ksplat">
              <el-icon><Aim /></el-icon>
              <span>{{ t('manager.layout.gaussianSplatKSplat') }}</span>
            </el-menu-item>
            <el-menu-item index="/point-cloud-copc">
              <el-icon><Grid /></el-icon>
              <span>{{ t('manager.layout.pointCloudCOPC') }}</span>
            </el-menu-item>
          </el-sub-menu>
          <el-sub-menu index="/spatial-tasks">
            <template #title>
              <el-icon><Aim /></el-icon>
              <span>{{ t('manager.layout.spatialTasks') }}</span>
            </template>
            <el-menu-item index="/spatial-tasks/vector-tiles">
              <el-icon><Grid /></el-icon>
              <span>{{ t('manager.layout.vectorTiles') }}</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  DataAnalysis,
  User,
  ArrowDown,
  SwitchButton,
  Search,
  Document,
  List,
  Grid,
  MagicStick,
  Picture,
  MapLocation,
  Box,
  Aim
} from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()

// 检测是否在 iframe 中
const isInIframe = ref(window.self !== window.top)
console.log('Manager Layout - isInIframe:', isInIframe.value)
console.log('Manager Layout - window.self:', window.self)
console.log('Manager Layout - window.top:', window.top)

const user = computed(() => authStore.user)
const activeMenu = computed(() => route.path)

onMounted(async () => {
  if (authStore.isAuthenticated && !authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('Failed to fetch user:', error)
    }
  }
})

const handleLogout = () => {
  authStore.logout()
  ElMessage.success(t('manager.layout.logoutSuccess'))
  router.push('/login')
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
  min-height: 0;
  overflow: hidden;
}

.body-container {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.header {
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
}

.header-left h1 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
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

.sidebar {
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
}

.main-content {
  min-width: 0;
  min-height: 0;
  background: var(--addp-bg-secondary) !important;
  padding: 20px;
  overflow: auto;
}

.el-menu-vertical {
  border-right: none;
}

/* iframe 模式样式 */
.content-only {
  width: 100%;
  height: 100vh;
  overflow: hidden;
  background: var(--addp-bg-secondary) !important;
}
</style>
