<template>
  <!-- Console 嵌入模式：只显示内容 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问模式：完整布局 -->
  <div v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <span class="logo-icon">🕸</span>
        <h1>知识图谱</h1>
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
          <el-menu-item index="/ontologies">
            <el-icon><Document /></el-icon>
            <span>本体建模</span>
          </el-menu-item>
          <el-menu-item index="/graphs">
            <el-icon><Connection /></el-icon>
            <span>知识图谱</span>
          </el-menu-item>
          <el-menu-item index="/knowledge-service">
            <el-icon><Share /></el-icon>
            <span>知识服务</span>
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
import { User, ArrowDown, SwitchButton, Document, Connection, Share } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/ontologies')) return '/ontologies'
  if (path.startsWith('/graphs')) return '/graphs'
  if (path.startsWith('/knowledge-service')) return '/knowledge-service'
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
  display: flex;
  flex-direction: column;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  height: 60px;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-left h1 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
}

.logo-icon {
  font-size: 20px;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

.main-container {
  flex: 1;
  overflow: hidden;
}

.sidebar {
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
}

.sidebar-menu {
  border-right: none;
  background: transparent;
}

.content {
  background: var(--addp-bg-secondary) !important;
  overflow-y: auto;
}

.content-only {
  background: var(--addp-bg-secondary) !important;
  height: 100%;
}
</style>
