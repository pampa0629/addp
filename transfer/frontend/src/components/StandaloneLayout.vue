<template>
  <el-container v-if="useLayout" class="layout-container">
    <el-header class="header">
      <div class="header-left">
        <el-icon :size="24" class="header-icon">
          <Upload />
        </el-icon>
        <h1>数据传输模块</h1>
      </div>
      <div class="header-right" v-if="authStore.isAuthenticated && userDisplayName">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><UserFilled /></el-icon>
            {{ userDisplayName }}
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

    <el-container>
      <el-aside width="200px" class="sidebar">
        <el-menu
          :default-active="activeMenu"
          class="el-menu-vertical"
          router
          @select="handleSelect"
        >
          <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <div class="page-header">
          <h2>{{ currentPageTitle }}</h2>
        </div>
        <div class="page-body">
          <router-view />
        </div>
      </el-main>
    </el-container>
  </el-container>

  <div v-else class="content-only">
    <router-view />
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import { ArrowDown, UserFilled, Connection, List, Timer, Upload, SwitchButton } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const isInIframe = ref(false)
if (typeof window !== 'undefined') {
  isInIframe.value = window.self !== window.top
}

const menuItems = [
  { index: '/tasks', label: '传输任务', icon: List },
  { index: '/executions', label: '执行记录', icon: Timer },
  { index: '/local-engines', label: '本地存储引擎', icon: Connection }
]

const activeMenu = computed(() => {
  const { path } = route
  const match = menuItems.find(item => path === item.index || path.startsWith(item.index + '/'))
  return match ? match.index : '/tasks'
})

const currentPageTitle = computed(() => {
  const match = menuItems.find(item => activeMenu.value === item.index)
  if (!match) {
    return '数据传输'
  }
  if (match.index === '/tasks') {
    if (route.name === 'TaskCreate') return '创建任务'
    if (route.name === 'TaskCreateSimple') return '快速创建'
    if (route.name === 'TaskEdit') return '编辑任务'
    if (route.name === 'TaskDetail') return '任务详情'
    return '传输任务'
  }
  if (match.index === '/executions') {
    return route.name === 'ExecutionDetail' ? '执行详情' : '执行记录'
  }
  return match.label
})

const userDisplayName = computed(() => {
  const name = authStore.user?.name || authStore.user?.nickname || authStore.user?.username
  return name || ''
})

const useLayout = computed(() => authStore.isAuthenticated && route.name !== 'Login' && !isInIframe.value)

const handleSelect = (index) => {
  if (index && index !== route.path) {
    router.push(index)
  }
}

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push({ name: 'Login' })
  }
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
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

.header-icon {
  margin-right: 10px;
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

.el-menu-vertical {
  border-right: none;
}

.main-content {
  background: var(--addp-bg-secondary) !important;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.page-header {
  padding: 16px 24px;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
}

.page-header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 500;
  color: var(--addp-text-primary);
}

.page-body {
  flex: 1;
  padding: 20px;
}

.content-only {
  width: 100%;
  min-height: 100vh;
  background: var(--addp-bg-secondary) !important;
  padding: 20px;
  box-sizing: border-box;
}
</style>
