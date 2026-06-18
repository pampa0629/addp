<template>
  <el-container class="layout">
    <!-- 顶部导航 -->
    <el-header class="header">
      <div class="header-left" style="cursor:pointer" @click="$router.push('/portal/home')">
        <el-icon class="logo-icon"><DataBoard /></el-icon>
        <span class="title">{{ t('portal.layout.title') }}</span>
      </div>

      <div class="header-center">
        <el-input
          v-model="searchKeyword"
          :placeholder="t('portal.layout.searchPlaceholder')"
          :prefix-icon="Search"
          class="search-input"
          @keyup.enter="handleSearch"
          clearable
        />
      </div>

      <div class="header-right">
        <el-button text @click="$router.push('/portal/my/applications')">
          <el-icon><Tickets /></el-icon>
          {{ t('portal.layout.myApplications') }}
        </el-button>
        <LangSwitcher style="margin-right: 4px;" />
        <ThemeSwitcher style="margin-right: 8px;" />
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || t('portal.layout.user') }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                {{ t('portal.layout.logout') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <!-- 主内容区 -->
    <el-main class="content">
      <router-view />
    </el-main>
  </el-container>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { ThemeSwitcher, LangSwitcher } from '@common-ui'
import {
  DataBoard, Search, Tickets,
  User, ArrowDown, SwitchButton
} from '@element-plus/icons-vue'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const searchKeyword = ref('')

const handleSearch = () => {
  if (searchKeyword.value.trim()) {
    router.push({ path: '/portal/search', query: { keyword: searchKeyword.value.trim() } })
  }
}

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push('/login')
  }
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
  padding: 0 24px;
  gap: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.logo-icon {
  font-size: 22px;
}

.title {
  font-size: 16px;
  font-weight: 600;
  white-space: nowrap;
}

.header-center {
  flex: 1;
  max-width: 500px;
}

.search-input {
  width: 100%;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  margin-left: 8px;
}

.content {
  background: var(--addp-bg-secondary) !important;
}
</style>
