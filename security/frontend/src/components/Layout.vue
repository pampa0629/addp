<template>
  <div v-if="embedded" class="content-only"><router-view /></div>
  <el-container v-else class="layout">
    <el-header class="header"><span class="title"><el-icon><Lock /></el-icon>{{ t('security.layout.title') }}</span><el-button link @click="logout">{{ t('security.layout.logout') }}</el-button></el-header>
    <el-container><el-aside width="220px"><el-menu router :default-active="route.path">
      <el-menu-item index="/sensitive-data-types"><el-icon><Key /></el-icon>{{ t('security.resources.sensitiveDataType') }}</el-menu-item>
      <el-menu-item index="/classifications"><el-icon><CollectionTag /></el-icon>{{ t('security.resources.classification') }}</el-menu-item>
      <el-menu-item index="/grades"><el-icon><Histogram /></el-icon>{{ t('security.resources.grade') }}</el-menu-item>
      <el-menu-item index="/protection-baselines"><el-icon><SetUp /></el-icon>{{ t('security.resources.protectionBaseline') }}</el-menu-item>
      <el-menu-item index="/protection-enrollments"><el-icon><CircleCheck /></el-icon>{{ t('security.resources.protectionEnrollment') }}</el-menu-item>
    </el-menu></el-aside><el-main><router-view /></el-main></el-container>
  </el-container>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Lock, Key, CollectionTag, Histogram, SetUp, CircleCheck } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'
const embedded = ref(false); const route = useRoute(); const router = useRouter(); const auth = useAuthStore(); const { t } = useI18n()
onMounted(() => { embedded.value = window.self !== window.top })
function logout() { auth.logout(); router.push('/login') }
</script>
<style scoped>
.layout{height:100vh;background:var(--addp-bg-secondary)}.header{display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--addp-border-color);background:var(--addp-bg-primary)}.title{display:flex;align-items:center;gap:8px;font-weight:600}.content-only{min-height:100vh;background:var(--addp-bg-secondary)}:deep(.el-aside){border-right:1px solid var(--addp-border-color);background:var(--addp-bg-primary)}
</style>
