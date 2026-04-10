<template>
  <el-container class="console-container">
    <PortalHeader
      :groups="MODULE_GROUPS"
      :active-group="activeGroup"
      :user="user"
      @group-click="handleGroupClick"
      @logo-click="handleLogoClick"
      @logout="handleLogout"
      @navigate="handleMenuSelect"
    />

    <el-container class="content-container">
      <PortalSidebar
        v-if="showSidebar"
        ref="sidebarRef"
        :active-group-modules="sidebarModules"
        :active-group-key="activeGroup"
        :active-menu="activeMenu"
        :is-collapsed="isCollapsed"
        :sidebar-menus="SIDEBAR_MENUS"
        @menu-select="handleMenuSelect"
        @toggle-collapse="toggleSidebar"
      />

      <el-main class="main-content" :class="{ 'with-agent': agentOpen }">
        <div v-if="currentModule === 'api-docs'" class="api-docs-view">
          <ApiDocs />
        </div>

        <PortalHome
          v-else-if="currentModule === 'home'"
          :active-group="activeGroup"
          :home-cards="homeCards"
          :user="user"
          @card-click="navigateToModule"
          @portal-click="openPortal"
          @navigate="handleMenuSelect"
        />

        <PortalIframe
          v-else
          :iframe-url="iframeUrl"
          @load="handleIframeLoad"
        />
      </el-main>

      <!-- AI 助手侧边抽屉 -->
      <transition name="agent-slide">
        <div v-if="agentOpen" class="agent-panel">
          <div class="agent-panel-header">
            <span>{{ t('console.agent.title') }}</span>
            <el-icon class="agent-close" @click="agentOpen = false"><Close /></el-icon>
          </div>
          <iframe
            class="agent-iframe"
            :src="agentIframeUrl"
            frameborder="0"
            allow="microphone"
          />
        </div>
      </transition>
    </el-container>

    <!-- 右下角浮动按钮 -->
    <div class="agent-fab" :class="{ 'is-open': agentOpen }" @click="toggleAgent">
      <el-icon :size="22"><component :is="agentOpen ? Close : ChatDotRound" /></el-icon>
      <span v-if="!agentOpen" class="fab-label">{{ t('console.agent.fab') }}</span>
    </div>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { useLangStore } from '../store/lang'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { ChatDotRound, Close } from '@element-plus/icons-vue'
import {
  MODULE_GROUPS, ALL_HOME_CARDS, SIDEBAR_MENUS, DEFAULT_ROUTES,
  PORTAL_URL, buildModuleUrl, MODULE_URLS,
} from '../config/portalConfig'
import PortalHeader from '../components/portal/PortalHeader.vue'
import PortalSidebar from '../components/portal/PortalSidebar.vue'
import PortalHome from '../components/portal/PortalHome.vue'
import PortalIframe from '../components/portal/PortalIframe.vue'
import ApiDocs from './ApiDocs.vue'

const router = useRouter()
const authStore = useAuthStore()
const langStore = useLangStore()
const { t } = useI18n()

const user = computed(() => authStore.user)
const activeMenu = ref('/')
const currentModule = ref('home')
const iframeUrl = ref('')
const isCollapsed = ref(false)
const activeGroup = ref(null)  // null = 全局首页
const sidebarRef = ref(null)
const sidebarModules = ref([])  // 侧边栏实际显示的模块（点卡片时只显示单个）

const currentGroupConfig = computed(() =>
  MODULE_GROUPS.find(g => g.key === activeGroup.value) || null
)

const showSidebar = computed(() =>
  !!activeGroup.value &&
  !!currentGroupConfig.value &&
  !currentGroupConfig.value.isPortal &&
  !currentGroupConfig.value.isApiDocs
)

const activeGroupModules = computed(() =>
  currentGroupConfig.value?.modules || []
)

const homeCards = computed(() => {
  if (!activeGroup.value) return ALL_HOME_CARDS
  return ALL_HOME_CARDS.filter(c => activeGroupModules.value.includes(c.module))
})

onMounted(async () => {
  if (authStore.isAuthenticated) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('[Console] Token validation failed:', error)
      authStore.logout()
      ElMessage.warning(t('console.sessionExpired'))
      router.push('/login')
    }
  }
})

const handleGroupClick = (group) => {
  if (group.isPortal) {
    openPortal()
    return
  }
  if (group.isApiDocs) {
    activeGroup.value = group.key
    currentModule.value = 'api-docs'
    iframeUrl.value = ''
    activeMenu.value = '/'
    return
  }
  activeGroup.value = group.key
  sidebarModules.value = group.modules  // 点群组Tab：显示全部模块
  currentModule.value = 'home'
  iframeUrl.value = ''
  activeMenu.value = '/'
}

const handleLogoClick = () => {
  activeGroup.value = null
  currentModule.value = 'home'
  iframeUrl.value = ''
  activeMenu.value = '/'
}

const handleMenuSelect = (index) => {
  activeMenu.value = index
  if (!index || typeof index !== 'string' || index === '/') {
    currentModule.value = 'home'
    iframeUrl.value = ''
    return
  }
  const parts = index.split('/').filter(Boolean)
  if (parts.length === 0) {
    currentModule.value = 'home'
    iframeUrl.value = ''
    return
  }
  const module = parts[0]
  const page = parts[1] || ''
  currentModule.value = module

  // 同步 activeGroup 和 sidebarModules（搜索/最近访问跳转时也需要）
  const group = MODULE_GROUPS.find(g => g.modules.includes(module))
  if (group && !group.isPortal && !group.isApiDocs) {
    activeGroup.value = group.key
    sidebarModules.value = group.modules  // 保持显示整个群组的所有模块
  }

  const url = buildModuleUrl(module, page, authStore.token)
  if (url) {
    iframeUrl.value = url
  } else {
    console.error('[Console] Module URL not found for:', module)
  }

  // 记录最近访问
  recordRecentVisit(module, page)
}

const RECENT_KEY = 'addp_recent_visits'
function recordRecentVisit(module, page) {
  const menuConfig = SIDEBAR_MENUS[module]
  if (!menuConfig) return
  const route = page ? `/${module}/${page}` : `/${module}`
  let label = menuConfig.label
  if (page && menuConfig.items) {
    const item = menuConfig.items.find(i => i.index === route)
    if (item) label = item.label
  }
  const entry = { key: route, route, label, module, icon: module }
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    let list = raw ? JSON.parse(raw) : []
    list = list.filter(i => i.key !== entry.key)
    list.unshift(entry)
    list = list.slice(0, 5)
    localStorage.setItem(RECENT_KEY, JSON.stringify(list))
  } catch { /* ignore */ }
}

const navigateToModule = async (module) => {
  const group = MODULE_GROUPS.find(g => g.modules.includes(module))
  if (group) {
    activeGroup.value = group.key
    sidebarModules.value = group.modules  // 显示整个群组的所有模块
  }
  const route = DEFAULT_ROUTES[module]
  if (route) {
    handleMenuSelect(route)
  }
  await nextTick()
  if (sidebarRef.value && group) {
    sidebarRef.value.openModule(module, group.modules)
  }
}

const openPortal = () => {
  const token = authStore.token
  const tokenParam = token ? `?token=${encodeURIComponent(token)}` : ''
  window.open(PORTAL_URL + '/portal/home' + tokenParam, '_blank')
}

const handleIframeLoad = () => {
  console.log('[Console] Iframe loaded:', iframeUrl.value)
  // 向刚加载的 iframe 发送当前语言（解决跨 origin localStorage 隔离问题）
  const iframe = document.querySelector('iframe.module-iframe')
  if (!iframe || !iframeUrl.value) return
  try {
    const url = new URL(iframeUrl.value)
    const targetOrigin = `${url.protocol}//${url.host}`
    iframe.contentWindow?.postMessage({
      type: 'lang-change',
      source: 'addp-console',
      lang: langStore.lang,
      timestamp: Date.now()
    }, targetOrigin)
    console.log('[Console] 已向新 iframe 发送当前语言:', langStore.lang)
  } catch (e) {
    console.warn('[Console] 发送语言消息到 iframe 失败:', e)
  }
}

const handleLogout = () => {
  authStore.logout()
  ElMessage.success(t('console.logoutSuccess'))
  router.push('/login')
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
}

// ─── AI 助手 ─────────────────────────────────────────────────────────────────

const AGENT_OPEN_KEY = 'addp_agent_panel_open'
const agentOpen = ref(localStorage.getItem(AGENT_OPEN_KEY) === 'true')

const agentIframeUrl = computed(() => {
  const base = MODULE_URLS.agent
  const token = authStore.token
  return token ? `${base}?token=${encodeURIComponent(token)}` : base
})

function toggleAgent() {
  agentOpen.value = !agentOpen.value
  localStorage.setItem(AGENT_OPEN_KEY, String(agentOpen.value))
}
</script>

<style scoped>
.console-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
}

.content-container {
  flex: 1;
  min-height: 0;
  height: auto;
  position: relative;
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
  transition: margin-right 0.3s;
}

.api-docs-view {
  width: 100%;
  height: 100%;
  overflow: hidden;
}

/* AI 助手侧边面板 */
.agent-panel {
  width: 380px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
  border-left: 1px solid var(--addp-border-color);
  height: 100%;
}

.agent-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--addp-border-color);
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  flex-shrink: 0;
}

.agent-close {
  cursor: pointer;
  color: var(--addp-text-tertiary);
  transition: color 0.15s;
}

.agent-close:hover {
  color: var(--addp-text-primary);
}

.agent-iframe {
  flex: 1;
  width: 100%;
  border: none;
}

/* 滑入动画 */
.agent-slide-enter-active,
.agent-slide-leave-active {
  transition: width 0.3s ease, opacity 0.3s ease;
  overflow: hidden;
}

.agent-slide-enter-from,
.agent-slide-leave-to {
  width: 0;
  opacity: 0;
}

.agent-slide-enter-to,
.agent-slide-leave-from {
  width: 380px;
  opacity: 1;
}

/* 右下角浮动按钮 */
.agent-fab {
  position: fixed;
  bottom: 28px;
  right: 28px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--el-color-primary);
  color: #fff;
  border-radius: 24px;
  padding: 10px 16px;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  z-index: 1000;
  transition: background 0.2s, box-shadow 0.2s, padding 0.2s;
  user-select: none;
}

.agent-fab:hover {
  background: var(--el-color-primary-dark-2);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.3);
}

.agent-fab.is-open {
  padding: 10px;
  border-radius: 50%;
}

.fab-label {
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}
</style>
