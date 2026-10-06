<template>
  <el-config-provider :locale="elementLocale"><router-view /></el-config-provider>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
const { locale } = useI18n()
const elementLocale = computed(() => locale.value === 'zh-cn' ? zhCn : en)
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}

#app {
  background: var(--addp-bg-secondary);
}
</style>
