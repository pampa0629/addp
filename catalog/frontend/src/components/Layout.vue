<template>
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <el-container v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <el-icon class="logo-icon"><Collection /></el-icon>
        <span class="title">{{ t('catalog.layout.title') }}</span>
      </div>
      <el-dropdown @command="handleCommand">
        <span class="user-dropdown">
          <el-icon><User /></el-icon>
          {{ authStore.user?.username || t('catalog.layout.user') }}
          <el-icon><ArrowDown /></el-icon>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="logout">
              <el-icon><SwitchButton /></el-icon>
              {{ t('catalog.layout.logout') }}
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </el-header>
    <el-container class="main-container">
      <el-aside class="sidebar" width="220px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-menu-item index="/entries">
            <el-icon><List /></el-icon>
            <span>{{ t('catalog.layout.entries') }}</span>
          </el-menu-item>
          <el-menu-item index="/me/entries">
            <el-icon><UserFilled /></el-icon>
            <span>{{ t('catalog.layout.myCatalog') }}</span>
          </el-menu-item>
          <el-menu-item v-if="canReadCollections" index="/collections">
            <el-icon><FolderOpened /></el-icon>
            <span>{{ t('catalog.layout.collections') }}</span>
          </el-menu-item>
          <el-menu-item v-if="canManageGovernance" index="/governance/tasks">
            <el-icon><WarningFilled /></el-icon>
            <span>{{ t('catalog.layout.governanceTasks') }}</span>
          </el-menu-item>
        </el-menu>
      </el-aside>
      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowDown, Collection, FolderOpened, List, SwitchButton, User, UserFilled, WarningFilled } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()
const isInIframe = window.self !== window.top
const activeMenu = computed(() => route.path.startsWith('/entries') ? '/entries' : route.path)
const canManageGovernance = computed(() => authStore.hasPermission('catalog.entry.update'))
const canReadCollections = computed(() => authStore.hasPermission('catalog.collection.read'))

async function handleCommand(command) {
  if (command !== 'logout') return
  await authStore.logout()
  await router.push('/login')
}
</script>

<style scoped>
.layout { height: 100vh; }
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
  padding: 0 20px;
}
.header-left, .user-dropdown { display: flex; align-items: center; gap: 8px; }
.logo-icon { font-size: 22px; color: var(--addp-module-catalog); }
.title { color: var(--addp-text-primary); font-size: 16px; font-weight: 600; }
.user-dropdown { color: var(--addp-text-primary); cursor: pointer; }
.main-container { height: calc(100vh - 60px); }
.sidebar { background: var(--addp-bg-primary); border-right: 1px solid var(--addp-border-color); }
.sidebar-menu { border-right: none; height: 100%; }
.content, .content-only { background: var(--addp-bg-secondary); }
.content { overflow-y: auto; }
.content-only { min-height: 100vh; box-sizing: border-box; }
</style>
