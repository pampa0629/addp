<template>
  <!-- 当在 iframe 中时，只显示内容区域 -->
  <div v-if="isInIframe" class="content-only">
    <slot></slot>
  </div>

  <!-- 独立访问时，显示完整布局 -->
  <el-container v-else class="layout-container">
    <el-header class="header">
      <div class="header-left">
        <el-icon :size="24" style="margin-right: 10px">
          <Platform />
        </el-icon>
        <h1>{{ t('system.layout.title') }}</h1>
      </div>
      <div class="header-right">
        <el-dropdown>
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ userDisplayName }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="handleLogout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('system.layout.logout') }}
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
          :default-openeds="['system']"
          router
          class="el-menu-vertical"
        >
          <el-menu-item index="/">
            <el-icon><HomeFilled /></el-icon>
            <span>{{ t('system.layout.overview') }}</span>
          </el-menu-item>

          <el-sub-menu index="system">
            <template #title>
              <el-icon><Setting /></el-icon>
              <span>{{ t('system.layout.systemMgmt') }}</span>
            </template>
            <el-menu-item v-if="showIAM" index="/iam">
              <el-icon><Lock /></el-icon>
              <span>{{ t('system.layout.iam') }}</span>
            </el-menu-item>
            <el-menu-item v-if="authStore.hasPermission('system.engine.read')" index="/engines">
              <el-icon><Connection /></el-icon>
              <span>{{ t('system.layout.engineMgmt') }}</span>
            </el-menu-item>
            <el-menu-item v-if="authStore.hasPermission('system.application.read')" index="/applications">
              <el-icon><Key /></el-icon>
              <span>{{ t('system.layout.appMgmt') }}</span>
            </el-menu-item>
            <el-menu-item v-if="authStore.hasPermission('system.cleanup.read')" index="/cleanup">
              <el-icon><Refresh /></el-icon>
              <span>{{ t('system.layout.cleanup') }}</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <slot></slot>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import {
  Platform,
  User,
  ArrowDown,
  SwitchButton,
  Setting,
  Lock,
  Connection,
  HomeFilled,
  Key,
  Refresh
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

// 检测是否在 iframe 中
const isInIframe = ref(false)

// 使用 onMounted 确保在 DOM 挂载后检测
onMounted(() => {
  isInIframe.value = window.self !== window.top
})

const user = computed(() => authStore.user)
const userDisplayName = computed(() =>
  user.value?.display_name || user.value?.local_account?.username || ''
)
const activeMenu = computed(() => route.path)
const iamPermissions = [
  'platform.tenant.read', 'iam.user.read', 'iam.platform_identity_change.read', 'audit.event.read',
  'iam.tenant_membership.read', 'iam.tenant_invitation.read', 'audit.tenant_event.read'
]
const showIAM = computed(() => authStore.hasAnyPermission(iamPermissions))

const handleLogout = () => {
  authStore.logout()
  ElMessage.success(t('system.layout.logoutSuccess'))
  router.push('/login')
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
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
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  color: var(--addp-text-secondary);
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s;
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
  height: 100%;
}

.main-content {
  background: var(--addp-bg-secondary) !important;
  padding: 20px;
}

/* iframe 模式样式 */
.content-only {
  width: 100%;
  height: auto;
  min-height: 0;
  padding: 20px;
  margin: 0;
  background: var(--addp-bg-secondary) !important;
  overflow: visible;
  box-sizing: border-box;
}
</style>
