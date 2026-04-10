<template>
  <!-- Console iframe 中仅渲染内容区域 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问时显示完整布局 -->
  <el-container v-else class="layout-container">
    <el-header class="layout-header">
      <div class="header-left">
        <el-icon :size="22" class="header-icon">
          <DataAnalysis />
        </el-icon>
        <h1>{{ t('meta.layout.title') }}</h1>
      </div>
      <div class="header-right">
        <el-dropdown trigger="click">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ userDisplayName }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="handleShowProfile">
                <el-icon><Avatar /></el-icon>
                {{ t('meta.layout.accountInfo') }}
              </el-dropdown-item>
              <el-dropdown-item divided @click="handleLogout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('meta.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container>
      <el-aside width="220px" class="sidebar">
        <el-menu
          :default-active="activeMenu"
          router
          class="el-menu-vertical"
        >
          <el-menu-item index="/scan">
            <el-icon><Search /></el-icon>
            <span>{{ t('meta.layout.metadataScan') }}</span>
          </el-menu-item>
          <el-menu-item index="/tasks">
            <el-icon><Timer /></el-icon>
            <span>{{ t('meta.layout.taskMonitor') }}</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox, ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  DataAnalysis,
  Search,
  Timer,
  User,
  ArrowDown,
  SwitchButton,
  Avatar
} from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const userDisplayName = computed(() => authStore.user?.username || t('meta.layout.notLoggedIn'))

const activeMenu = computed(() => {
  return route.path.startsWith('/tasks') ? '/tasks' : '/scan'
})

const handleShowProfile = () => {
  ElMessageBox.alert(
    t('meta.layout.currentUser', { name: authStore.user?.username || t('meta.layout.notLoggedIn') }),
    t('meta.layout.accountInfo'),
    {
      confirmButtonText: t('meta.layout.gotIt')
    }
  )
}

const handleLogout = () => {
  ElMessageBox.confirm(t('meta.layout.logoutConfirm'), t('meta.layout.hint'), {
    confirmButtonText: t('meta.layout.logoutBtn'),
    cancelButtonText: t('meta.layout.cancel'),
    type: 'warning'
  })
    .then(() => {
      authStore.logout()
      ElMessage.success(t('meta.layout.logoutSuccess'))
      router.push({ name: 'Login' })
    })
    .catch(() => {})
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.layout-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 18px;
}

.header-icon {
  color: var(--el-color-primary);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.user-dropdown {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 6px 10px;
  border-radius: 4px;
  color: var(--addp-text-secondary);
  transition: background 0.2s;
}

.user-dropdown:hover {
  background: var(--addp-bg-secondary);
}

.sidebar {
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
}

.el-menu-vertical {
  border-right: none;
}

.main-content {
  background: var(--addp-bg-secondary) !important;
  padding: 24px;
  overflow: auto;
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
