<template>
  <el-config-provider :locale="elementLocale">
    <StandaloneLayout v-if="isStandalone" />
    <router-view v-else />
  </el-config-provider>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import enLocale from 'element-plus/es/locale/lang/en'
import StandaloneLayout from '@/components/StandaloneLayout.vue'

const { locale } = useI18n()
const elementLocale = computed(() => locale.value === 'zh-cn' ? zhCn : enLocale)

const isStandalone = ref(false)

if (typeof window !== 'undefined') {
  isStandalone.value = window.self === window.top
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: 'Helvetica Neue', Helvetica, 'PingFang SC', 'Hiragino Sans GB',
    'Microsoft YaHei', '微软雅黑', Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
</style>
