<template>
  <div v-if="embedded" class="content"><router-view /></div>
  <el-container v-else class="layout">
    <el-header class="header">
      <strong>{{ t('ontology.title') }}</strong>
      <span>
        <LangSwitcher />
        <el-button text @click="logout">{{ t('ontology.logout') }}</el-button>
      </span>
    </el-header>
    <el-container>
      <el-aside class="sidebar" width="176px">
        <el-menu default-active="/ontologies" @select="navigate">
          <el-menu-item index="/ontologies">
            {{ t('ontology.list') }}
          </el-menu-item>
        </el-menu>
      </el-aside>
      <el-main class="content"><router-view /></el-main>
    </el-container>
  </el-container>
</template>
<script setup>
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { LangSwitcher, navigateConsoleModuleRoute } from '@common-ui'
import { useAuthStore } from '../store/auth'
const { t } = useI18n()
const router = useRouter(),
  auth = useAuthStore(),
  embedded = window.self !== window.top
const navigate = (path) => navigateConsoleModuleRoute(router, 'ontology', path)
async function logout() {
  // Run the existing editor leave guard before revoking the session.
  if (router.currentRoute.value.path !== '/ontologies') {
    const cancelled = await navigate('/ontologies')
    if (cancelled) return
  }
  await auth.logout()
  await router.push('/login')
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
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
}
.header span {
  display: flex;
  align-items: center;
  gap: 16px;
}
.sidebar {
  background: var(--addp-bg-primary);
}
.content {
  min-width: 0;
  min-height: 100vh;
  padding: 0;
  background: var(--addp-bg-secondary);
}
@media (max-width: 768px) {
  .sidebar {
    display: none;
  }
}
</style>
