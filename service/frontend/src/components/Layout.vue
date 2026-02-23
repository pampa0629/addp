<template>
  <!-- 在 Console 中：只显示内容，不显示导航 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问时：显示完整布局 -->
  <div v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <h1>Service 数据服务</h1>
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
        <el-menu
          :default-active="activeMenu"
          router
          class="sidebar-menu"
        >
          <el-menu-item index="/query-services">
            <el-icon><Search /></el-icon>
            <span>查询服务</span>
          </el-menu-item>

          <el-menu-item index="/registered-services">
            <el-icon><Link /></el-icon>
            <span>注册服务</span>
          </el-menu-item>

          <el-menu-item index="/published-services">
            <el-icon><Share /></el-icon>
            <span>服务发布</span>
          </el-menu-item>

          <el-menu-item index="/catalog">
            <el-icon><FolderOpened /></el-icon>
            <span>服务目录</span>
          </el-menu-item>

          <el-menu-item index="/tile">
            <el-icon><Grid /></el-icon>
            <span>瓦片服务</span>
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
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import {
  User,
  ArrowDown,
  SwitchButton,
  Search,
  Link,
  Share,
  FolderOpened,
  Grid
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

// 子页面（详情、表单）激活父级菜单项
const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/query-services')) return '/query-services'
  if (path.startsWith('/registered-services')) return '/registered-services'
  if (path.startsWith('/published-services')) return '/published-services'
  if (path.startsWith('/tile')) return '/tile'
  if (path.startsWith('/services')) return '/registered-services' // legacy routes
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
.content-only {
  width: 100%;
  height: 100vh;
  overflow: auto;
}

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
