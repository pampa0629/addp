<template>
  <el-container class="console-container">
    <el-header class="header">
      <div class="header-left">
        <!-- Logo 区域：点击回全局首页 -->
        <div class="logo-area" @click="handleLogoClick">
          <el-icon :size="28" style="margin-right: 12px">
            <Platform />
          </el-icon>
          <h1>全域数据平台</h1>
        </div>

        <!-- 顶部群组图标 Tab 栏 -->
        <nav class="group-tabs">
          <el-tooltip
            v-for="group in MODULE_GROUPS"
            :key="group.key"
            :content="group.label"
            placement="bottom"
            :show-after="200"
          >
            <button
              class="group-tab"
              :class="{ 'is-active': activeGroup === group.key }"
              @click="handleGroupClick(group)"
            >
              <el-icon :size="20"><component :is="group.icon" /></el-icon>
            </button>
          </el-tooltip>
        </nav>
      </div>

      <div class="header-right">
        <!-- 主题切换器 -->
        <ThemeSwitcher style="margin-right: 16px;" />

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
      <!-- 侧边栏：仅在选中群组时显示 -->
      <el-aside v-if="showSidebar" :width="sidebarWidth" :class="['sidebar', { collapsed: isCollapsed }]">
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
          <!-- 数据准备群组 -->
          <template v-if="activeGroupModules.includes('transfer')">
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
              <el-menu-item index="/transfer/local-engines">
                <el-icon><Connection /></el-icon>
                <span>本地资源</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <template v-if="activeGroupModules.includes('meta')">
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
          </template>

          <template v-if="activeGroupModules.includes('manager')">
            <el-sub-menu index="manager">
              <template #title>
                <el-icon><DataAnalysis /></el-icon>
                <span>数据管理</span>
              </template>
              <el-menu-item index="/manager/data-explorer">
                <el-icon><Search /></el-icon>
                <span>数据探查</span>
              </el-menu-item>
              <el-menu-item index="/manager/data-retrieval">
                <el-icon><Document /></el-icon>
                <span>数据检索</span>
              </el-menu-item>
              <el-menu-item index="/manager/vectorization-tasks">
                <el-icon><List /></el-icon>
                <span>向量化任务</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <!-- 数据治理群组 -->
          <template v-if="activeGroupModules.includes('standard')">
            <el-sub-menu index="standard">
              <template #title>
                <el-icon><Reading /></el-icon>
                <span>数据标准</span>
              </template>
              <el-menu-item index="/standard/domains">
                <el-icon><Share /></el-icon>
                <span>业务域管理</span>
              </el-menu-item>
              <el-menu-item index="/standard/glossaries">
                <el-icon><Document /></el-icon>
                <span>业务术语</span>
              </el-menu-item>
              <el-menu-item index="/standard/elements">
                <el-icon><DataBoard /></el-icon>
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
                <el-icon><FolderOpened /></el-icon>
                <span>标准文档</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <template v-if="activeGroupModules.includes('modeling')">
            <el-sub-menu index="modeling">
              <template #title>
                <el-icon><Grid /></el-icon>
                <span>数据建模</span>
              </template>
              <el-menu-item index="/modeling/dw-layers">
                <el-icon><Grid /></el-icon>
                <span>数仓分层</span>
              </el-menu-item>
              <el-menu-item index="/modeling/entities">
                <el-icon><DataBoard /></el-icon>
                <span>业务实体</span>
              </el-menu-item>
              <el-menu-item index="/modeling/er-diagram">
                <el-icon><Share /></el-icon>
                <span>实体关系图</span>
              </el-menu-item>
              <el-menu-item index="/modeling/logical-tables">
                <el-icon><Connection /></el-icon>
                <span>逻辑表设计</span>
              </el-menu-item>
              <el-menu-item index="/modeling/star-schema">
                <el-icon><Grid /></el-icon>
                <span>星型建模视图</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <template v-if="activeGroupModules.includes('quality')">
            <el-sub-menu index="quality">
              <template #title>
                <el-icon><CircleCheck /></el-icon>
                <span>数据质量</span>
              </template>
              <el-menu-item index="/quality/rule-applications">
                <el-icon><Setting /></el-icon>
                <span>规则应用配置</span>
              </el-menu-item>
              <el-menu-item index="/quality/check-tasks">
                <el-icon><List /></el-icon>
                <span>检查任务</span>
              </el-menu-item>
              <el-menu-item index="/quality/executions">
                <el-icon><Timer /></el-icon>
                <span>执行记录</span>
              </el-menu-item>
              <el-menu-item index="/quality/issues">
                <el-icon><Warning /></el-icon>
                <span>问题工单</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <!-- 开发与监控群组 -->
          <template v-if="activeGroupModules.includes('develop')">
            <el-sub-menu index="develop">
              <template #title>
                <el-icon><Edit /></el-icon>
                <span>数据开发</span>
              </template>
              <el-menu-item index="/develop/sql">
                <el-icon><Monitor /></el-icon>
                <span>查询工作台</span>
              </el-menu-item>
              <el-menu-item index="/develop/notebook">
                <el-icon><Notebook /></el-icon>
                <span>Notebook 开发</span>
              </el-menu-item>
              <el-menu-item index="/develop/workflow">
                <el-icon><Connection /></el-icon>
                <span>工作流编辑器</span>
              </el-menu-item>
              <el-menu-item index="/develop/tasks">
                <el-icon><List /></el-icon>
                <span>任务管理</span>
              </el-menu-item>
              <el-menu-item index="/develop/executions">
                <el-icon><Timer /></el-icon>
                <span>执行监控</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <template v-if="activeGroupModules.includes('service')">
            <el-sub-menu index="service">
              <template #title>
                <el-icon><Link /></el-icon>
                <span>数据服务</span>
              </template>
              <el-menu-item index="/service/query-services">
                <el-icon><Upload /></el-icon>
                <span>查询服务</span>
              </el-menu-item>
              <el-menu-item index="/service/tile">
                <el-icon><Grid /></el-icon>
                <span>瓦片服务</span>
              </el-menu-item>
              <el-menu-item index="/service/services">
                <el-icon><Connection /></el-icon>
                <span>服务注册</span>
              </el-menu-item>
              <el-menu-item index="/service/catalog">
                <el-icon><FolderOpened /></el-icon>
                <span>服务目录</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <template v-if="activeGroupModules.includes('orchestrator')">
            <el-sub-menu index="orchestrator">
              <template #title>
                <el-icon><Operation /></el-icon>
                <span>任务编排</span>
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
          </template>

          <template v-if="activeGroupModules.includes('monitor')">
            <el-sub-menu index="monitor">
              <template #title>
                <el-icon><DataLine /></el-icon>
                <span>执行监控</span>
              </template>
              <el-menu-item index="/monitor/dashboard">
                <el-icon><Monitor /></el-icon>
                <span>监控仪表盘</span>
              </el-menu-item>
              <el-menu-item index="/monitor/executions">
                <el-icon><List /></el-icon>
                <span>执行记录</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <!-- 资产管理群组 -->
          <template v-if="activeGroupModules.includes('asset')">
            <el-sub-menu index="asset">
              <template #title>
                <el-icon><Folder /></el-icon>
                <span>资产管理</span>
              </template>
              <el-menu-item index="/asset/type-definitions">
                <el-icon><Grid /></el-icon>
                <span>资产类型</span>
              </el-menu-item>
              <el-menu-item index="/asset/categories">
                <el-icon><Files /></el-icon>
                <span>目录管理</span>
              </el-menu-item>
              <el-menu-item index="/asset/assets">
                <el-icon><List /></el-icon>
                <span>资产工作台</span>
              </el-menu-item>
              <el-menu-item index="/asset/applications">
                <el-icon><Tickets /></el-icon>
                <span>审批管理</span>
              </el-menu-item>
              <el-menu-item index="/asset/dashboard">
                <el-icon><DataAnalysis /></el-icon>
                <span>运营看板</span>
              </el-menu-item>
            </el-sub-menu>
          </template>

          <!-- 系统管理群组 -->
          <template v-if="activeGroupModules.includes('agent')">
            <el-menu-item index="/agent">
              <el-icon><ChatDotRound /></el-icon>
              <span>智能体</span>
            </el-menu-item>
          </template>

          <template v-if="activeGroupModules.includes('system')">
            <el-sub-menu index="system">
              <template #title>
                <el-icon><Setting /></el-icon>
                <span>系统管理</span>
              </template>
              <el-menu-item index="/system/users">
                <el-icon><User /></el-icon>
                <span>用户管理</span>
              </el-menu-item>
              <el-menu-item index="/system/engines">
                <el-icon><Connection /></el-icon>
                <span>引擎管理</span>
              </el-menu-item>
              <el-menu-item index="/system/applications">
                <el-icon><Key /></el-icon>
                <span>应用管理</span>
              </el-menu-item>
              <el-menu-item index="/system/logs">
                <el-icon><Document /></el-icon>
                <span>日志审计</span>
              </el-menu-item>
              <el-menu-item index="/system/cleanup">
                <el-icon><Delete /></el-icon>
                <span>垃圾清理</span>
              </el-menu-item>
              <el-menu-item index="/system/docs">
                <el-icon><Monitor /></el-icon>
                <span>API 文档</span>
              </el-menu-item>
            </el-sub-menu>
          </template>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <!-- 首页视图 -->
        <div v-if="currentModule === 'home'" class="home-view">
          <!-- 全局首页欢迎语 -->
          <div v-if="!activeGroup" class="home-welcome">
            <h2>欢迎使用全域数据平台</h2>
            <p>请从顶部选择功能区域，或直接点击模块卡片进入</p>
          </div>

          <div class="cards-grid">
            <!-- 数据门户卡片：全局首页或资产管理群组时显示 -->
            <el-card
              v-if="!activeGroup || activeGroup === 'asset'"
              shadow="hover"
              class="module-card"
              @click="openPortal()"
            >
              <div class="card-content">
                <el-icon :size="48" style="color: var(--addp-module-portal)"><DataBoard /></el-icon>
                <h2>数据门户</h2>
                <p>数据消费者门户，搜索与申请数据资产</p>
              </div>
            </el-card>

            <!-- 其余模块卡片（按当前群组过滤） -->
            <el-card
              v-for="card in homeCards"
              :key="card.module"
              shadow="hover"
              class="module-card"
              @click="navigateToModule(card.module)"
            >
              <div class="card-content">
                <el-icon :size="48" :style="{ color: `var(${card.cssVar})` }">
                  <component :is="card.icon" />
                </el-icon>
                <h2>{{ card.label }}</h2>
                <p>{{ card.desc }}</p>
              </div>
            </el-card>
          </div>
        </div>

        <!-- iframe 区域 -->
        <div v-else class="iframe-wrapper">
          <div class="iframe-container">
            <iframe
              v-if="iframeUrl"
              :src="iframeUrl"
              :key="iframeUrl"
              frameborder="0"
              allow="clipboard-write; clipboard-read"
              @load="handleIframeLoad"
              class="module-iframe"
            ></iframe>
            <div v-else class="loading-container">
              <el-icon class="is-loading" :size="32"><Loading /></el-icon>
              <p>等待选择模块...</p>
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
import {
  Fold, Expand, Operation, Edit, Key, Notebook, Delete, Connection,
  Grid, FolderOpened, DataLine, Reading, Share, DataBoard, List,
  Odometer, TrendCharts, SortDown, Document, CircleCheck, Folder,
  Files, Tickets, Upload, Box, DataAnalysis, Setting, Link, Warning,
  Coin, Tools, Shop, ChatDotRound
} from '@element-plus/icons-vue'
import ThemeSwitcher from '../components/ThemeSwitcher.vue'

const router = useRouter()
const authStore = useAuthStore()

const user = computed(() => authStore.user)
const activeMenu = ref('/')
const currentModule = ref('home')
const iframeUrl = ref('')
const isCollapsed = ref(false)
const activeGroup = ref(null)  // null = 全局首页

// 群组导航配置
const MODULE_GROUPS = [
  { key: 'data-prepare', label: '数据准备',   icon: Coin,    modules: ['transfer', 'meta', 'manager'] },
  { key: 'data-govern',  label: '数据治理',   icon: Reading,  modules: ['standard', 'modeling', 'quality'] },
  { key: 'dev-monitor',  label: '开发与监控', icon: Tools,   modules: ['develop', 'service', 'orchestrator', 'monitor'] },
  { key: 'asset',        label: '资产管理',   icon: Folder,   modules: ['asset'] },
  { key: 'portal',       label: '资产门户',   icon: Shop,     modules: [], isPortal: true },
  { key: 'agent', label: '智能体',    icon: ChatDotRound, modules: ['agent'] },
  { key: 'system',       label: '系统管理',   icon: Setting,  modules: ['system'] },
]

// 首页模块卡片配置（颜色全部使用 CSS 变量）
const ALL_HOME_CARDS = [
  { module: 'transfer',     label: '数据传输',   icon: Upload,       cssVar: '--addp-module-transfer',     desc: '数据导入、数据导出、任务调度' },
  { module: 'meta',         label: '元数据管理', icon: Box,          cssVar: '--addp-module-meta',          desc: '元数据解析、数据血缘、数据目录' },
  { module: 'manager',      label: '数据管理',   icon: DataAnalysis, cssVar: '--addp-module-manager',       desc: '数据探查、目录组织、数据预览' },
  { module: 'standard',     label: '数据标准',   icon: Reading,      cssVar: '--addp-module-standard',      desc: '业务域、业务术语、数据元、码值集' },
  { module: 'modeling',     label: '数据建模',   icon: Grid,         cssVar: '--addp-module-modeling',      desc: '数仓分层、业务实体、逻辑表设计' },
  { module: 'quality',      label: '数据质量',   icon: CircleCheck,  cssVar: '--addp-module-quality',       desc: '规则应用、质量检查、执行记录' },
  { module: 'develop',      label: '数据开发',   icon: Edit,         cssVar: '--addp-module-develop',       desc: '查询工作台、工作流编辑器、Notebook' },
  { module: 'service',      label: '数据服务',   icon: Link,         cssVar: '--addp-module-service',       desc: '外部服务注册、OGC 服务、查询服务' },
  { module: 'orchestrator', label: '任务编排',   icon: Operation,    cssVar: '--addp-module-orchestrator',  desc: '工作流编排、任务调度、执行管理' },
  { module: 'monitor',      label: '执行监控',   icon: DataLine,     cssVar: '--addp-module-monitor',       desc: '任务执行监控、统计分析、健康检查' },
  { module: 'asset',        label: '资产管理',   icon: Folder,       cssVar: '--addp-module-asset',         desc: '资产类型、分类管理、申请与授权' },
  { module: 'system',       label: '系统管理',   icon: Setting,      cssVar: '--addp-module-system',        desc: '用户管理、日志查询、引擎配置' },
  { module: 'agent',        label: '智能体',     icon: ChatDotRound, cssVar: '--addp-module-agent',         desc: '自然语言对话、数据管理、智能分析' },
]

// 计算属性
const currentGroupConfig = computed(() =>
  MODULE_GROUPS.find(g => g.key === activeGroup.value) || null
)

const showSidebar = computed(() =>
  !!activeGroup.value && !!currentGroupConfig.value && !currentGroupConfig.value.isPortal
)

const activeGroupModules = computed(() =>
  currentGroupConfig.value?.modules || []
)

const homeCards = computed(() => {
  if (!activeGroup.value) return ALL_HOME_CARDS
  return ALL_HOME_CARDS.filter(c => activeGroupModules.value.includes(c.module))
})

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
  develop: isDevelopment ? `${protocol}//${hostname}:5178` : `${protocol}//${hostname}/develop`,
  service: isDevelopment ? `${protocol}//${hostname}:5180` : `${protocol}//${hostname}/service`,
  monitor: isDevelopment ? `${protocol}//${hostname}:5179` : `${protocol}//${hostname}/monitor`,
  standard: isDevelopment ? `${protocol}//${hostname}:5181` : `${protocol}//${hostname}/standard`,
  modeling: isDevelopment ? `${protocol}//${hostname}:5182` : `${protocol}//${hostname}/model`,
  quality: isDevelopment ? `${protocol}//${hostname}:5183` : `${protocol}//${hostname}/quality`,
  asset: isDevelopment ? `${protocol}//${hostname}:5184` : `${protocol}//${hostname}/asset`,
  agent: isDevelopment ? `${protocol}//${hostname}:5186` : `${protocol}//${hostname}/agent`,
}

onMounted(async () => {
  // 主动验证 token 有效性（即使已有缓存的 user 信息）
  if (authStore.isAuthenticated) {
    try {
      await authStore.fetchUser()
      console.log('[Console] Token validated successfully')
    } catch (error) {
      console.error('[Console] Token validation failed, logging out:', error)
      authStore.logout()
      ElMessage.warning('登录已过期，请重新登录')
      router.push('/login')
    }
  }
})

// 点击顶部群组 Tab
const handleGroupClick = (group) => {
  if (group.isPortal) {
    openPortal()
    return
  }
  activeGroup.value = group.key
  currentModule.value = 'home'
  iframeUrl.value = ''
  activeMenu.value = '/'
}

// 点击 Logo 回全局首页
const handleLogoClick = () => {
  activeGroup.value = null
  currentModule.value = 'home'
  iframeUrl.value = ''
  activeMenu.value = '/'
}

const handleMenuSelect = (index) => {
  console.log('[Console] Menu selected:', index)
  activeMenu.value = index

  // 安全检查：确保 index 是字符串
  if (!index || typeof index !== 'string' || index === '/') {
    currentModule.value = 'home'
    iframeUrl.value = ''
    console.log('[Console] Navigating to home, clearing iframe')
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

  console.log('[Console] Parsed - module:', module, 'page:', page)
  currentModule.value = module

  if (moduleUrls[module]) {
    const token = authStore.token
    let url = ''

    const managerPageMap = {
      'data-explorer': 'data-explorer',
      'data-retrieval': 'data-retrieval',
      'vectorization-tasks': 'vectorization-tasks',
      '': 'data-explorer'
    }

    const metaPageMap = {
      'scan': 'scan',
      'tasks': 'tasks',
      '': 'scan'
    }

    const transferPageMap = {
      'tasks': 'tasks',
      'executions': 'executions',
      'local-engines': 'local-engines',
      '': 'tasks'
    }

    const orchestratorPageMap = {
      'orchestrations': 'orchestrations',
      'executions': 'executions',
      '': 'orchestrations'
    }

    const developPageMap = {
      'sql': 'sql',
      'notebook': 'notebook',
      'gis-workflow': 'gis-workflow',
      'gis-tasks': 'gis-tasks',
      'gis-executions': 'gis-executions',
      '': 'sql'
    }

    if (module === 'manager') {
      const actualPage = managerPageMap[page] !== undefined ? managerPageMap[page] : 'data-explorer'
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (module === 'meta') {
      const actualPage = metaPageMap[page] !== undefined ? metaPageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'transfer') {
      const actualPage = transferPageMap[page] !== undefined ? transferPageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'orchestrator') {
      const actualPage = orchestratorPageMap[page] !== undefined ? orchestratorPageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'system') {
      url = page ? `${moduleUrls[module]}/${page}` : `${moduleUrls[module]}/`
    } else if (module === 'develop') {
      const actualPage = developPageMap[page] !== undefined ? developPageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'service') {
      const servicePageMap = {
        'services': 'services',
        'query-services': 'query-services',
        'catalog': 'catalog',
        'tile': 'tile',
        '': 'query-services'
      }
      const actualPage = servicePageMap[page] !== undefined ? servicePageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'monitor') {
      const monitorPageMap = {
        'dashboard': 'dashboard',
        'executions': 'executions',
        '': 'dashboard'
      }
      const actualPage = monitorPageMap[page] !== undefined ? monitorPageMap[page] : page
      url = actualPage ? `${moduleUrls[module]}/${actualPage}` : `${moduleUrls[module]}/`
    } else if (module === 'standard') {
      const standardPageMap = {
        'domains': 'standard/domains',
        'glossaries': 'standard/glossaries',
        'elements': 'standard/elements',
        'code-sets': 'standard/code-sets',
        'units': 'standard/units',
        'classifications': 'standard/classifications',
        'metrics': 'standard/metrics',
        'documents': 'standard/documents',
        'dimension-hierarchies': 'standard/dimension-hierarchies',
        '': 'standard/domains'
      }
      const actualPage = standardPageMap[page] !== undefined ? standardPageMap[page] : `standard/${page}`
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (module === 'modeling') {
      const modelingPageMap = {
        'dw-layers': 'modeling/dw-layers',
        'entities': 'modeling/entities',
        'logical-tables': 'modeling/logical-tables',
        'er-diagram': 'modeling/er-diagram',
        'star-schema': 'modeling/star-schema',
        '': 'modeling/dw-layers'
      }
      const actualPage = modelingPageMap[page] !== undefined ? modelingPageMap[page] : `modeling/${page}`
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (module === 'quality') {
      const qualityPageMap = {
        'rule-applications': 'quality/rule-applications',
        'check-tasks': 'quality/check-tasks',
        'executions': 'quality/executions',
        'issues': 'quality/issues',
        '': 'quality/check-tasks'
      }
      const actualPage = qualityPageMap[page] !== undefined ? qualityPageMap[page] : `quality/${page}`
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (module === 'asset') {
      const assetPageMap = {
        'type-definitions': 'asset/type-definitions',
        'categories': 'asset/categories',
        'assets': 'asset/assets',
        'applications': 'asset/applications',
        'dashboard': 'asset/dashboard',
        '': 'asset/assets'
      }
      const actualPage = assetPageMap[page] !== undefined ? assetPageMap[page] : `asset/${page}`
      url = `${moduleUrls[module]}/${actualPage}`
    } else if (page) {
      url = `${moduleUrls[module]}/${page}`
    } else {
      url = moduleUrls[module]
    }

    // 添加认证 token
    if (token) {
      const separator = url.includes('?') ? '&' : '?'
      url = `${url}${separator}token=${encodeURIComponent(token)}`
    }

    iframeUrl.value = url
    console.log('[Console] Setting iframe URL:', iframeUrl.value)
    console.log('[Console] currentModule:', currentModule.value)
  } else {
    console.error('[Console] Module URL not found for:', module)
  }
}

const navigateToModule = (module) => {
  const defaultRoutes = {
    system: '/system/users',
    manager: '/manager/data-explorer',
    meta: '/meta/scan',
    transfer: '/transfer/tasks',
    orchestrator: '/orchestrator/orchestrations',
    develop: '/develop/sql',
    service: '/service/query-services',
    monitor: '/monitor/dashboard',
    standard: '/standard/domains',
    modeling: '/modeling/dw-layers',
    quality: '/quality/check-tasks',
    asset: '/asset/assets',
    agent: '/agent',
  }
  if (defaultRoutes[module]) {
    handleMenuSelect(defaultRoutes[module])
  }
}

const openPortal = () => {
  const portalUrl = isDevelopment
    ? `${protocol}//${hostname}:5185`
    : `${protocol}//${hostname}/portal`
  const token = authStore.token
  const tokenParam = token ? `?token=${encodeURIComponent(token)}` : ''
  window.open(portalUrl + '/portal/home' + tokenParam, '_blank')
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
.console-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.header {
  background: var(--addp-bg-header);
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
}

/* Logo 区域：点击回首页 */
.logo-area {
  display: flex;
  align-items: center;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: background 0.2s;
  margin-right: 16px;
  flex-shrink: 0;
}

.logo-area:hover {
  background: var(--addp-bg-secondary);
}

.logo-area h1 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
  background: var(--addp-primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  white-space: nowrap;
}

/* 顶部群组图标 Tab 栏 */
.group-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}

.group-tabs::-webkit-scrollbar {
  display: none;
}

.group-tab {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--addp-text-secondary);
  cursor: pointer;
  border-radius: 8px;
  transition: background 0.2s, color 0.2s;
  flex-shrink: 0;
}

.group-tab:hover {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
}

.group-tab.is-active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.header-right {
  display: flex;
  align-items: center;
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
  background: var(--addp-bg-sidebar);
  border-right: 1px solid var(--addp-border-color);
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
  background: var(--addp-bg-secondary);
  padding: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  height: auto;
}

/* 首页 */
.home-view {
  padding: 40px;
  overflow-y: auto;
}

.home-welcome {
  margin-bottom: 32px;
}

.home-welcome h2 {
  font-size: 22px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin-bottom: 8px;
}

.home-welcome p {
  color: var(--addp-text-tertiary);
  font-size: 14px;
}

/* 卡片网格（替换 el-row） */
.cards-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
}

.module-card {
  cursor: pointer;
  transition: all 0.3s;
  height: 200px;
}

.module-card:hover {
  transform: var(--addp-card-hover-transform);
  box-shadow: var(--addp-shadow-hover);
}

.card-content {
  text-align: center;
  padding: 20px;
}

.card-content h2 {
  margin: 15px 0 10px 0;
  font-size: 20px;
  color: var(--addp-text-primary);
}

.card-content p {
  color: var(--addp-text-tertiary);
  font-size: 14px;
  margin: 0;
}

/* iframe */
.iframe-wrapper {
  flex: 1;
  display: flex;
  min-height: 0;
  background: var(--addp-bg-secondary);
}

.iframe-container {
  width: 100%;
  flex: 1;
  min-height: 0;
  height: calc(100vh - 60px);
  background: var(--addp-bg-secondary);
}

.module-iframe {
  width: 100%;
  height: 100%;
  border: none;
  background: var(--addp-bg-secondary);
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--addp-text-tertiary);
}

.el-menu-vertical {
  border-right: none;
}

/* 侧边栏菜单分组标题 */
.sidebar :deep(.el-menu-item-group__title) {
  font-size: 14px;
  font-weight: 600;
  padding: 12px 0 8px 20px;
  color: var(--addp-text-secondary);
}

.sidebar.collapsed :deep(.el-menu-item-group__title) {
  padding-left: 0;
  text-align: center;
}
</style>
