<template>
  <el-container class="portal-container">
    <el-header class="header">
      <div class="header-left">
        <el-icon :size="28" style="margin-right: 12px">
          <Platform />
        </el-icon>
        <h1>全域数据平台</h1>
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
                退出登录
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="content-container">
      <el-aside :width="sidebarWidth" :class="['sidebar', { collapsed: isCollapsed }]">
        <div class="collapse-toggle">
          <el-button
            circle
            size="small"
            :icon="isCollapsed ? Expand : Fold"
            @click="toggleSidebar"
            :title="isCollapsed ? '展开菜单' : '收起菜单'"
          />
        </div>
        <el-menu
          :default-active="activeMenu"
          @select="handleMenuSelect"
          class="el-menu-vertical"
          :collapse="isCollapsed"
        >
          <el-menu-item index="/">
            <el-icon><HomeFilled /></el-icon>
            <span>门户首页</span>
          </el-menu-item>

          <el-sub-menu index="transfer">
            <template #title>
              <el-icon><Upload /></el-icon>
              <span>数据传输</span>
            </template>
            <el-menu-item index="/transfer/tasks">
              <el-icon><List /></el-icon>
              <span>传输任务</span>
            </el-menu-item>
            <el-menu-item index="/transfer/executions">
              <el-icon><Timer /></el-icon>
              <span>执行记录</span>
            </el-menu-item>
            <el-menu-item index="/transfer/local-resources">
              <el-icon><Connection /></el-icon>
              <span>本地存储引擎</span>
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="manager">
            <template #title>
              <el-icon><DataAnalysis /></el-icon>
              <span>数据管理</span>
            </template>
            <el-menu-item index="/manager/data-explorer">
              <el-icon><Search /></el-icon>
              <span>数据探查</span>
            </el-menu-item>
            <el-menu-item index="/manager/fulltext-search">
              <el-icon><Document /></el-icon>
              <span>全文检索</span>
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="meta">
            <template #title>
              <el-icon><Box /></el-icon>
              <span>元数据</span>
            </template>
            <el-menu-item index="/meta/scan">
              <el-icon><Search /></el-icon>
              <span>元数据扫描</span>
            </el-menu-item>
            <el-menu-item index="/meta/tasks">
              <el-icon><Monitor /></el-icon>
              <span>任务监控</span>
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="orchestrator">
            <template #title>
              <el-icon><Operation /></el-icon>
              <span>编排引擎</span>
            </template>
            <el-menu-item index="/orchestrator/orchestrations">
              <el-icon><List /></el-icon>
              <span>编排任务</span>
            </el-menu-item>
            <el-menu-item index="/orchestrator/executions">
              <el-icon><Timer /></el-icon>
              <span>执行记录</span>
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="system">
            <template #title>
              <el-icon><Setting /></el-icon>
              <span>系统管理</span>
            </template>
            <el-menu-item index="/system/users">
              <el-icon><User /></el-icon>
              <span>用户管理</span>
            </el-menu-item>
            <el-menu-item index="/system/logs">
              <el-icon><Document /></el-icon>
              <span>日志管理</span>
            </el-menu-item>
            <el-menu-item index="/system/resources">
              <el-icon><Connection /></el-icon>
              <span>存储引擎</span>
            </el-menu-item>
            <el-menu-item index="/system/developer">
              <el-icon><Monitor /></el-icon>
              <span>开发中心</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <div v-if="currentModule === 'home'" class="home-view">
          <el-row :gutter="20">
            <el-col :span="12">
              <el-card shadow="hover" class="module-card" @click="navigateToModule('system')">
                <div class="card-content">
                  <el-icon :size="48" color="#409EFF"><Setting /></el-icon>
                  <h2>系统管理</h2>
                  <p>用户管理、日志查询、存储引擎配置</p>
                </div>
              </el-card>
            </el-col>
            <el-col :span="12">
              <el-card shadow="hover" class="module-card" @click="navigateToModule('manager')">
                <div class="card-content">
                  <el-icon :size="48" color="#67C23A"><DataAnalysis /></el-icon>
                  <h2>数据管理</h2>
                  <p>数据源管理、目录组织、数据预览</p>
                </div>
              </el-card>
            </el-col>
          </el-row>
          <el-row :gutter="20" style="margin-top: 20px;">
            <el-col :span="12">
              <el-card shadow="hover" class="module-card" @click="navigateToModule('meta')">
                <div class="card-content">
                  <el-icon :size="48" color="#E6A23C"><Box /></el-icon>
                  <h2>元数据管理</h2>
                  <p>元数据解析、数据血缘、数据目录</p>
                </div>
              </el-card>
            </el-col>
            <el-col :span="12">
              <el-card shadow="hover" class="module-card" @click="navigateToModule('transfer')">
                <div class="card-content">
                  <el-icon :size="48" color="#F56C6C"><Upload /></el-icon>
                  <h2>数据传输</h2>
                  <p>数据导入、数据导出、任务调度</p>
                </div>
              </el-card>
            </el-col>
          </el-row>
          <el-row :gutter="20" style="margin-top: 20px;">
            <el-col :span="12">
              <el-card shadow="hover" class="module-card" @click="navigateToModule('orchestrator')">
                <div class="card-content">
                  <el-icon :size="48" color="#FF7875"><Operation /></el-icon>
                  <h2>编排引擎</h2>
                  <p>工作流编排、任务调度、执行管理</p>
                </div>
              </el-card>
            </el-col>
          </el-row>
        </div>
        <div v-else class="iframe-wrapper">
          <div class="iframe-container">
            <iframe
              v-if="iframeUrl"
              :src="iframeUrl"
              :key="iframeUrl"
              frameborder="0"
              @load="handleIframeLoad"
              class="module-iframe"
            ></iframe>
            <div v-else class="loading-container">
              <el-icon class="is-loading" :size="32"><Loading /></el-icon>
              <p>等待选择模块...</p>
              <p style="font-size: 12px; color: #909399;">currentModule: {{ currentModule }}</p>
            </div>
          </div>
        </div>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { ElMessage } from 'element-plus'
import { Fold, Expand, Operation } from '@element-plus/icons-vue'

const router = useRouter()
const authStore = useAuthStore()

const user = computed(() => authStore.user)
const activeMenu = ref('/')
const currentModule = ref('home')
const iframeUrl = ref('')
const isCollapsed = ref(false)

// 生产环境使用 Nginx 路由的相对路径
// 开发环境使用当前主机名 + 端口（避免硬编码 localhost 导致跨机器访问空白）
const isDevelopment = import.meta.env.DEV
const protocol = window.location.protocol
const hostname = window.location.hostname
const moduleUrls = {
  system: isDevelopment ? `${protocol}//${hostname}:5173` : `${protocol}//${hostname}/system`,
  manager: isDevelopment ? `${protocol}//${hostname}:5174` : `${protocol}//${hostname}/manager`,
  meta: isDevelopment ? `${protocol}//${hostname}:5175` : `${protocol}//${hostname}/meta`,
  transfer: isDevelopment ? `${protocol}//${hostname}:5176` : `${protocol}//${hostname}/transfer`,
  orchestrator: isDevelopment ? `${protocol}//${hostname}:5177` : `${protocol}//${hostname}/orchestrator`,
  develop: isDevelopment ? `${protocol}//${hostname}:5178` : `${protocol}//${hostname}/develop`
}

onMounted(async () => {
  if (authStore.isAuthenticated && !authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('Failed to fetch user:', error)
    }
  }
})

const handleMenuSelect = (index) => {
  console.log('Portal: Menu selected:', index)
  activeMenu.value = index

  // 安全检查：确保 index 是字符串
  if (!index || typeof index !== 'string' || index === '/') {
    currentModule.value = 'home'
    iframeUrl.value = ''
    console.log('Portal: Navigating to home, clearing iframe')
    return
  }

  const parts = index.split('/').filter(p => p) // 过滤掉空字符串
  if (parts.length === 0) {
    currentModule.value = 'home'
    iframeUrl.value = ''
    return
  }

  const module = parts[0] // system, manager, meta, transfer
  const page = parts[1] || '' // users, logs, datasources, etc.

  console.log('Portal: Parsed - module:', module, 'page:', page)
  currentModule.value = module

  if (moduleUrls[module]) {
    // 构建完整的 URL，并附加认证token作为URL参数
    const token = authStore.token
    let url = ''

    // Manager 模块的路由映射
    // Manager 路由使用 /manager/ 作为 base，路径结构：/manager/, /manager/directories 等
    const managerPageMap = {
      'data-explorer': 'data-explorer',
      'fulltext-search': 'fulltext-search',
      '': 'data-explorer'
    }

    // Meta 模块的路由映射
    // Meta 路由使用 /meta/ 作为 base，当前默认指向 /meta/scan
    const metaPageMap = {
      'scan': 'scan',  // 对应 /meta/scan (元数据扫描)
      'tasks': 'tasks',
      '': 'scan'
    }

    // Transfer 模块的路由映射
    const transferPageMap = {
      'tasks': 'tasks',
      'executions': 'executions',
      'local-resources': 'local-resources',
      '': 'tasks'
    }

    // Orchestrator 模块的路由映射
    const orchestratorPageMap = {
      'orchestrations': 'orchestrations',
      'executions': 'executions',
      '': 'orchestrations'
    }

    if (module === 'manager') {
      // Manager 模块的路由: /data-explorer 等 (不需要 /manager 前缀)
      const actualPage = managerPageMap[page] !== undefined ? managerPageMap[page] : 'data-explorer'
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (module === 'meta') {
      // Meta 模块的路由: /scan, /lineage 等 (不需要 /meta 前缀)
      const actualPage = metaPageMap[page] !== undefined ? metaPageMap[page] : page
      if (actualPage) {
        url = `${moduleUrls[module]}/${actualPage}`
      } else {
        url = `${moduleUrls[module]}/`
      }
    } else if (module === 'transfer') {
      // Transfer 模块的路由: /tasks, /executions 等 (不需要 /transfer 前缀)
      const actualPage = transferPageMap[page] !== undefined ? transferPageMap[page] : page
      if (actualPage) {
        url = `${moduleUrls[module]}/${actualPage}`
      } else {
        url = `${moduleUrls[module]}/`
      }
    } else if (module === 'orchestrator') {
      // Orchestrator 模块的路由: /orchestrations, /executions 等 (不需要 /orchestrator 前缀)
      const actualPage = orchestratorPageMap[page] !== undefined ? orchestratorPageMap[page] : page
      if (actualPage) {
        url = `${moduleUrls[module]}/${actualPage}`
      } else {
        url = `${moduleUrls[module]}/`
      }
    } else if (module === 'system') {
      // System 模块的路由: /users, /logs 等 (不需要 /system 前缀)
      if (page) {
        url = `${moduleUrls[module]}/${page}`
      } else {
        url = `${moduleUrls[module]}/`
      }
    } else if (page) {
      // 其他模块保持原有逻辑
      url = `${moduleUrls[module]}/${page}`
    } else {
      url = moduleUrls[module]
    }

    // 如果有token，添加到URL参数中
    if (token) {
      const separator = url.includes('?') ? '&' : '?'
      url = `${url}${separator}token=${encodeURIComponent(token)}`
    }

    iframeUrl.value = url
    console.log('Portal: Setting iframe URL:', iframeUrl.value)
    console.log('Portal: currentModule:', currentModule.value)
  } else {
    console.error('Portal: Module URL not found for:', module)
  }
}

const navigateToModule = (module) => {
  if (module === 'system') {
    handleMenuSelect('/system/users')
  } else if (module === 'manager') {
    handleMenuSelect('/manager/data-explorer')
  } else if (module === 'meta') {
    handleMenuSelect('/meta/scan')
  } else if (module === 'transfer') {
    handleMenuSelect('/transfer/tasks')
  } else if (module === 'orchestrator') {
    handleMenuSelect('/orchestrator/orchestrations')
  }
}

const handleIframeLoad = () => {
  console.log('Iframe loaded successfully:', iframeUrl.value)
}

const handleLogout = () => {
  authStore.logout()
  ElMessage.success('已退出登录')
  router.push('/login')
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
}

const sidebarWidth = computed(() => (isCollapsed.value ? '72px' : '240px'))
</script>

<style scoped>
.portal-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.header {
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
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
  font-size: 24px;
  font-weight: 600;
  margin: 0;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
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
  background: #f5f7fa;
}

.sidebar {
  background: #fff;
  border-right: 1px solid #e4e7ed;
  display: flex;
  flex-direction: column;
  transition: width 0.2s ease;
}

.sidebar.collapsed {
  align-items: center;
}

.collapse-toggle {
  display: flex;
  justify-content: flex-end;
  padding: 12px 12px 6px;
}

.sidebar.collapsed .collapse-toggle {
  justify-content: center;
}

.collapse-toggle :deep(.el-button) {
  border: none;
}

.sidebar.collapsed :deep(.el-menu-vertical) {
  border-right: none;
}

.content-container {
  flex: 1;
  min-height: 0;
  height: auto;
}

.main-content {
  background: #f5f7fa;
  padding: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  height: auto;
}

.home-view {
  padding: 40px;
}

.module-card {
  cursor: pointer;
  transition: all 0.3s;
  height: 200px;
}

.module-card:hover {
  transform: translateY(-5px);
  box-shadow: 0 12px 24px rgba(0, 0, 0, 0.1);
}

.module-card-disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.module-card-disabled:hover {
  transform: none;
  box-shadow: none;
}

.card-content {
  text-align: center;
  padding: 20px;
}

.card-content h2 {
  margin: 15px 0 10px 0;
  font-size: 20px;
  color: #303133;
}

.card-content p {
  color: #909399;
  font-size: 14px;
  margin: 0;
}

.iframe-wrapper {
  flex: 1;
  display: flex;
  min-height: 0;
}

.iframe-container {
  width: 100%;
  flex: 1;
  min-height: 0;
  height: calc(100vh - 64px);
}

.module-iframe {
  width: 100%;
  height: 100%;
  border: none;
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #909399;
}

.el-menu-vertical {
  border-right: none;
}
</style>
