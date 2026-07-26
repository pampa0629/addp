<template>
  <el-container class="console-container">
    <PortalHeader
      :groups="MODULE_GROUPS"
      :active-group="activeGroup"
      :user="user"
      :permissions="authStore.permissions"
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
        :sidebar-menus="visibleSidebarMenus"
        @menu-select="handleMenuSelect"
        @toggle-collapse="toggleSidebar"
      />

      <el-main class="main-content">
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

    </el-container>

    <!-- 右下角魔法棒 + 向左滑出面板（仅首页显示） -->
    <transition name="fab-fade">
      <div v-if="currentModule === 'home'" class="copilot-fab-wrapper">
        <!-- 滑出的输入面板 -->
        <transition name="copilot-slide">
          <div v-if="copilotOpen" class="copilot-inline-panel">
            <div class="copilot-panel-header">
              <span class="copilot-panel-title">{{ t('console.copilot.title') }}</span>
              <el-icon class="copilot-panel-close" @click="copilotOpen = false"><Close /></el-icon>
            </div>
            <el-input
              ref="copilotInputRef"
              v-model="copilotQuery"
              type="textarea"
              :rows="3"
              :placeholder="t('console.copilot.placeholder')"
              :disabled="copilotLoading"
              @keydown.ctrl.enter="askCopilot"
            />
            <!-- 结果区域 -->
            <div v-if="copilotResult" class="copilot-result">
              <p class="copilot-text">{{ copilotResult.text }}</p>
              <div class="copilot-actions">
                <el-button
                  v-for="action in copilotResult.actions"
                  :key="action.route"
                  size="small"
                  type="primary"
                  plain
                  @click="handleCopilotAction(action.route)"
                >
                  {{ action.label }}
                </el-button>
              </div>
            </div>
            <div class="copilot-panel-footer">
              <span class="copilot-panel-hint">Ctrl+Enter {{ t('console.copilot.ask') }}</span>
              <el-button
                type="primary"
                :loading="copilotLoading"
                size="small"
                @click="askCopilot"
              >
                {{ t('console.copilot.ask') }}
              </el-button>
            </div>
          </div>
        </transition>

        <!-- 魔法棒 FAB 按钮 -->
        <div
          class="copilot-fab"
          :class="{ 'copilot-fab--active': copilotOpen }"
          @click="toggleCopilot"
        >
          <el-icon :size="20"><MagicStick /></el-icon>
          <span class="fab-label">{{ t('console.copilot.fab') }}</span>
        </div>
      </div>
    </transition>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { useLangStore } from '../store/lang'
import { ElMessage } from 'element-plus'
import {
  CONSOLE_NAVIGATION_CHANNEL,
  createIframeAuthCoordinator,
  getAccessToken,
  getAccessTokenExpiresAt,
  registerConsoleBridgeHandler
} from '@common-ui'
import { useI18n } from 'vue-i18n'
import { MagicStick, Close } from '@element-plus/icons-vue'
import {
  MODULE_GROUPS, ALL_HOME_CARDS, SIDEBAR_MENUS, DEFAULT_ROUTES,
  MODULE_URLS, PORTAL_URL, buildModuleUrl,
} from '../config/portalConfig'
import PortalHeader from '../components/portal/PortalHeader.vue'
import PortalSidebar from '../components/portal/PortalSidebar.vue'
import PortalHome from '../components/portal/PortalHome.vue'
import { navigateGuide } from '../api/copilot'
import { createManualScanRun, deleteEngineScanTask, getScanTasks, upsertEngineScanTask } from '../api/meta'
import PortalIframe from '../components/portal/PortalIframe.vue'
import ApiDocs from './ApiDocs.vue'

const router = useRouter()
const route = useRoute()
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

const ENGINE_SCAN_POLICY_CHANNEL = 'engine-scan-policy'
let stopEngineScanPolicyBridge = null
let stopConsoleNavigationBridge = null
let iframeAuthCoordinator = null

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

const visibleSidebarMenus = computed(() => Object.fromEntries(
  Object.entries(SIDEBAR_MENUS).map(([module, menu]) => [module, menu.items
    ? { ...menu, items: menu.items.filter(item => !item.permissions?.length || authStore.hasAnyPermission(item.permissions)) }
    : menu])
))

onMounted(async () => {
  iframeAuthCoordinator = createIframeAuthCoordinator({
    allowedOrigins: [...new Set(Object.values(MODULE_URLS).map(url => new URL(url).origin))],
    getToken: getAccessToken,
    getExpiresAt: getAccessTokenExpiresAt,
    refreshToken: options => authStore.refreshAccessToken(options),
    logout: () => authStore.logout()
  })
  stopConsoleNavigationBridge = registerConsoleBridgeHandler(
    CONSOLE_NAVIGATION_CHANNEL,
    handleConsoleNavigationBridge,
    { allowedSources: ['addp-module'] }
  )
  stopEngineScanPolicyBridge = registerConsoleBridgeHandler(
    ENGINE_SCAN_POLICY_CHANNEL,
    handleEngineScanPolicyBridge,
    { allowedSources: ['addp-system'] }
  )
})

onBeforeUnmount(() => {
  stopConsoleNavigationBridge?.()
  stopConsoleNavigationBridge = null
  stopEngineScanPolicyBridge?.()
  stopEngineScanPolicyBridge = null
  iframeAuthCoordinator?.dispose()
  iframeAuthCoordinator = null
})

const handleConsoleNavigationBridge = async (payload = {}) => {
  const targetRoute = typeof payload.route === 'string' ? payload.route.trim() : ''
  if (!targetRoute || !targetRoute.startsWith('/')) {
    throw new Error('route must be an absolute console route')
  }
  await router.push(targetRoute)
  return { route: targetRoute }
}

const normalizeScanConfig = (scanConfig) => {
  if (!scanConfig) return null
  return {
    ...scanConfig,
    enabled: Boolean(scanConfig.immediate_scan || scanConfig.scheduled_scan)
  }
}

const scanConfigFromTask = (task) => {
  if (!task) return null
  const parameters = task.parameters || {}
  return {
    enabled: true,
    immediate_scan: true,
    immediate_depth: 'basic',
    scheduled_scan: true,
    schedule_mode: 'cron',
    cron_expression: task.schedule || '',
    schedule_time: '00:00',
    schedule_value: [],
    scan_depth: parameters.scan_depth || 'deep'
  }
}

const handleEngineScanPolicyBridge = async (payload = {}) => {
  if (payload.action === 'load') {
    const engineId = Number(payload.engineId)
    if (!engineId) throw new Error('engine_id is required')
    const tasks = await getScanTasks(engineId)
    const task = tasks.find(item => item.owner_module === 'system' && item.owner_ref === `engine:${engineId}`) || tasks[0]
    return { scanConfig: scanConfigFromTask(task) }
  }

  if (payload.action === 'sync') {
    const engine = payload.engine || {}
    const engineId = Number(engine.id)
    if (!engineId) throw new Error('engine_id is required')
    const scanConfig = normalizeScanConfig(payload.scanConfig)
    if (!scanConfig || !scanConfig.scheduled_scan) {
      await deleteEngineScanTask(engineId)
    } else {
      await upsertEngineScanTask(engineId, {
        engine_name: engine.name,
        scan_policy: scanConfig
      })
    }

    if (payload.triggerImmediate && scanConfig?.immediate_scan) {
      await createManualScanRun(engineId, {
        scan_depth: scanConfig.immediate_depth || scanConfig.scan_depth || 'basic',
        trigger_type: 'manual',
        source: 'console',
        force: false
      })
    }

    return {}
  }

  throw new Error(`unsupported action: ${payload.action || ''}`)
}

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
  router.push('/')
}

const handleMenuSelect = (index) => {
  if (!index || typeof index !== 'string' || index === '/') {
    router.push('/')
    return
  }
  router.push(index)
}

function syncRouteToPortal(fullPath) {
  const [pathPart, queryPart] = String(fullPath || '/').split('?')
  activeMenu.value = pathPart || '/'

  const parts = pathPart.split('/').filter(Boolean)
  if (parts.length === 0) {
    activeGroup.value = null
    sidebarModules.value = []
    currentModule.value = 'home'
    iframeUrl.value = ''
    return
  }
  const module = parts[0]
  const pagePath = parts.slice(1).join('/')
  const page = queryPart ? `${pagePath}?${queryPart}` : pagePath
  currentModule.value = module

  // 同步 activeGroup 和 sidebarModules（搜索/最近访问跳转时也需要）
  const group = MODULE_GROUPS.find(g => g.modules.includes(module))
  if (group && !group.isPortal && !group.isApiDocs) {
    activeGroup.value = group.key
    sidebarModules.value = group.modules  // 保持显示整个群组的所有模块
  }

  const url = buildModuleUrl(module, page)
  if (url) {
    iframeUrl.value = url
  } else {
    console.error('[Console] Module URL not found for:', module)
  }

  // 记录最近访问
  recordRecentVisit(module, pagePath)
}

watch(
  () => route.fullPath,
  (fullPath) => syncRouteToPortal(fullPath),
  { immediate: true }
)

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
    await router.push(route)
  }
  await nextTick()
  if (sidebarRef.value && group) {
    sidebarRef.value.openModule(module, group.modules)
  }
}

const openPortal = () => {
  window.open(PORTAL_URL + '/portal/home', '_blank')
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

const handleLogout = async () => {
  await authStore.logout()
  ElMessage.success(t('console.logoutSuccess'))
  router.push('/login')
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
}

// ─── AI 助手 ─────────────────────────────────────────────────────────────────

// Copilot 状态
const copilotOpen = ref(false)
const copilotQuery = ref('')
const copilotLoading = ref(false)
const copilotResult = ref(null)
const copilotInputRef = ref(null)

function toggleCopilot() {
  copilotOpen.value = !copilotOpen.value
  if (copilotOpen.value) {
    copilotResult.value = null
    setTimeout(() => copilotInputRef.value?.focus(), 300)
  }
}

async function askCopilot() {
  if (!copilotQuery.value.trim()) return
  copilotLoading.value = true
  copilotResult.value = null
  try {
    const res = await navigateGuide({ query: copilotQuery.value })
    copilotResult.value = res
  } catch (e) {
    ElMessage.error('导航助手暂时不可用')
  } finally {
    copilotLoading.value = false
  }
}

function handleCopilotAction(route) {
  copilotOpen.value = false
  handleMenuSelect(route)
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

/* AI 助手侧边面板 - 已移除 */

/* 右下角魔法棒浮动按钮组 */
.copilot-fab-wrapper {
  position: fixed;
  bottom: 28px;
  right: 28px;
  display: flex;
  align-items: flex-end;
  gap: 10px;
  z-index: 1000;
}

.copilot-fab {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--el-color-primary);
  color: #fff;
  border-radius: 24px;
  padding: 10px 16px;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  flex-shrink: 0;
  transition: background 0.2s, box-shadow 0.2s, transform 0.2s;
  user-select: none;
}

.copilot-fab:hover {
  background: var(--el-color-primary-dark-2);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.3);
}

.copilot-fab--active {
  background: var(--el-color-primary-dark-2);
  transform: scale(0.96);
}

.fab-label {
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

/* FAB 淡入淡出 */
.fab-fade-enter-active,
.fab-fade-leave-active {
  transition: opacity 0.2s, transform 0.2s;
}
.fab-fade-enter-from,
.fab-fade-leave-to {
  opacity: 0;
  transform: scale(0.8);
}

/* Copilot 内联面板 */
.copilot-inline-panel {
  width: 320px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.25);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transform-origin: right bottom;
}

.copilot-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.copilot-panel-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.copilot-panel-close {
  cursor: pointer;
  color: var(--addp-text-secondary);
  font-size: 14px;
  transition: color 0.15s;
}

.copilot-panel-close:hover {
  color: var(--addp-text-primary);
}

.copilot-panel-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.copilot-panel-hint {
  font-size: 11px;
  color: var(--addp-text-secondary);
}

/* 滑动动画 */
.copilot-slide-enter-active {
  transition: all 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.copilot-slide-leave-active {
  transition: all 0.18s ease-in;
}

.copilot-slide-enter-from {
  opacity: 0;
  transform: translateX(20px) scale(0.95);
}

.copilot-slide-leave-to {
  opacity: 0;
  transform: translateX(20px) scale(0.95);
}

/* Copilot 结果区域 */
.copilot-result {
  margin-top: 0;
  padding: 10px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
}

.copilot-text {
  margin: 0 0 10px;
  font-size: 14px;
  color: var(--el-text-color-primary);
  line-height: 1.6;
}

.copilot-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
