<template>
  <el-config-provider :locale="elementLocale">
    <router-view />
  </el-config-provider>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import enLocale from 'element-plus/es/locale/lang/en'
import { useAuthStore } from './store/auth'

const authStore = useAuthStore()
const { locale } = useI18n()
const elementLocale = computed(() => locale.value === 'zh-cn' ? zhCn : enLocale)

onMounted(async () => {
  // 如果有 token，尝试获取用户信息
  if (authStore.token && !authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('获取用户信息失败:', error)
      authStore.logout()
    }
  }
})
</script>

<style>
#app {
  font-family: Avenir, Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  margin: 0;
  padding: 0;
  background: var(--addp-bg-secondary) !important;
}

body {
  margin: 0;
  padding: 0;
}
</style>
