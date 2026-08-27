<template>
  <div v-if="isInIframe" class="content-only"><router-view /></div>
  <el-container v-else class="layout">
    <el-header class="header"><div class="brand"><el-icon><DataAnalysis /></el-icon><span>{{ t('workbench.title') }}</span></div><el-button text @click="logout">{{ t('workbench.logout') }}</el-button></el-header>
    <el-container class="main"><el-aside width="220px" class="sidebar"><el-menu router :default-active="active"><el-menu-item index="/views"><el-icon><View /></el-icon><span>{{ t('workbench.views') }}</span></el-menu-item><el-menu-item index="/applications"><el-icon><Grid /></el-icon><span>{{ t('workbench.dataApplications') }}</span></el-menu-item></el-menu></el-aside><el-main class="content"><router-view /></el-main></el-container>
  </el-container>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { DataAnalysis, Grid, View } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'
const { t } = useI18n(); const route = useRoute(); const router = useRouter(); const auth = useAuthStore()
const isInIframe = window.self !== window.top
const active = computed(() => route.path.startsWith('/views') ? '/views' : route.path.startsWith('/applications') ? '/applications' : route.path)
async function logout() { await auth.logout(); await router.push('/login') }
</script>
<style scoped>
.layout { height: 100vh; }.header,.brand { display:flex;align-items:center }.header { justify-content:space-between;background:var(--addp-bg-primary);border-bottom:1px solid var(--addp-border-color) }.brand{gap:8px;color:var(--addp-text-primary);font-weight:600}.main{height:calc(100vh - 60px)}.sidebar{background:var(--addp-bg-primary);border-right:1px solid var(--addp-border-color)}.sidebar :deep(.el-menu){border-right:0}.content,.content-only{background:var(--addp-bg-secondary)}.content{overflow:auto}.content-only{min-height:100vh}
</style>
