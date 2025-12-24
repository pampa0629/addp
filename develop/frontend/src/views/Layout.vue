<template>
  <!-- 在 Portal 中：只显示内容，不显示导航 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问时：显示完整布局 -->
  <div v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <h1>Develop 数据开发</h1>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || '用户' }}
            <el-icon class="el-icon--right"><arrow-down /></el-icon>
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
          :default-active="$route.path"
          router
          class="sidebar-menu"
        >
          <el-menu-item index="/sql">
            <el-icon><Document /></el-icon>
            <span>SQL 工作台</span>
          </el-menu-item>

          <el-menu-item index="/notebook">
            <el-icon><Notebook /></el-icon>
            <span>Notebook 开发</span>
          </el-menu-item>

          <el-menu-item index="/sql-tasks">
            <el-icon><FolderOpened /></el-icon>
            <span>SQL 任务</span>
          </el-menu-item>

          <el-menu-item index="/workflow">
            <el-icon><Connection /></el-icon>
            <span>工作流编辑器</span>
          </el-menu-item>

          <el-menu-item index="/tasks">
            <el-icon><List /></el-icon>
            <span>任务管理</span>
          </el-menu-item>

          <el-menu-item index="/executions">
            <el-icon><Monitor /></el-icon>
            <span>执行监控</span>
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
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
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
const isInIframe = ref(false)

onMounted(() => {
  // 检测是否在 iframe 中
  isInIframe.value = window.self !== window.top
})

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push('/login')
  }
}
</script>

<style scoped>
/* 内容模式（Portal 中） */
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
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
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
  color: #303133;
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
  color: #606266;
  font-size: 14px;
}

.user-dropdown:hover {
  color: #409eff;
}

.main-container {
  flex: 1;
  overflow: hidden;
}

.sidebar {
  background: #fff;
  border-right: 1px solid #e4e7ed;
  overflow-y: auto;
}

.sidebar-menu {
  border-right: none;
}

.content {
  background: #f5f7fa;
  overflow: auto;
  padding: 0;
}

/* 自定义滚动条 */
.sidebar::-webkit-scrollbar {
  width: 6px;
}

.sidebar::-webkit-scrollbar-track {
  background: #f5f7fa;
}

.sidebar::-webkit-scrollbar-thumb {
  background: #c0c4cc;
  border-radius: 3px;
}

.sidebar::-webkit-scrollbar-thumb:hover {
  background: #909399;
}
</style>
