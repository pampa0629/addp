<template>
  <el-config-provider :locale="elementLocale">
    <router-view />
  </el-config-provider>
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
html,
body,
#app {
  width: 100%;
  height: 100%;
  margin: 0;
  font-family: var(--el-font-family);
  font-size: var(--el-font-size-base);
}

#app {
  min-height: 0;
}
</style>
