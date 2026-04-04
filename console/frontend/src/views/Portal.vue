<template>
  <el-container class="console-container">
    <PortalHeader
      :groups="MODULE_GROUPS"
      :active-group="activeGroup"
      :user="user"
      @group-click="handleGroupClick"
      @logo-click="handleLogoClick"
      @logout="handleLogout"
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

      <el-main class="main-content">
        <div v-if="currentModule === 'api-docs'" class="api-docs-view">
          <ApiDocs />
        </div>

        <PortalHome
          v-else-if="currentModule === 'home'"
          :active-group="activeGroup"
          :home-cards="homeCards"
          @card-click="navigateToModule"
          @portal-click="openPortal"
        />

        <PortalIframe
          v-else
          :iframe-url="iframeUrl"
          @load="handleIframeLoad"
        />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { ElMessage } from 'element-plus'
import {
  MODULE_GROUPS, ALL_HOME_CARDS, SIDEBAR_MENUS, DEFAULT_ROUTES,
  PORTAL_URL, buildModuleUrl,
} from '../config/portalConfig'
import PortalHeader from '../components/portal/PortalHeader.vue'
import PortalSidebar from '../components/portal/PortalSidebar.vue'
import PortalHome from '../components/portal/PortalHome.vue'
import PortalIframe from '../components/portal/PortalIframe.vue'
import ApiDocs from './ApiDocs.vue'

const router = useRouter()
const authStore = useAuthStore()

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
      ElMessage.warning('登录已过期，请重新登录')
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

  const url = buildModuleUrl(module, page, authStore.token)
  if (url) {
    iframeUrl.value = url
  } else {
    console.error('[Console] Module URL not found for:', module)
  }
}

const navigateToModule = async (module) => {
  const group = MODULE_GROUPS.find(g => g.modules.includes(module))
  if (group) {
    activeGroup.value = group.key
  }
  sidebarModules.value = [module]  // 点卡片：只显示这一个模块
  const route = DEFAULT_ROUTES[module]
  if (route) {
    handleMenuSelect(route)
  }
  await nextTick()
  if (sidebarRef.value && group) {
    sidebarRef.value.openModule(module, [module])
  }
}

const openPortal = () => {
  const token = authStore.token
  const tokenParam = token ? `?token=${encodeURIComponent(token)}` : ''
  window.open(PORTAL_URL + '/portal/home' + tokenParam, '_blank')
}

const handleIframeLoad = () => {
  console.log('[Console] Iframe loaded:', iframeUrl.value)
}

const handleLogout = () => {
  authStore.logout()
  ElMessage.success('已退出登录')
  router.push('/login')
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
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

.api-docs-view {
  width: 100%;
  height: 100%;
  overflow: hidden;
}
</style>
