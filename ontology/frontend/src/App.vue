<template>
  <el-config-provider :locale="elementLocale">
    <router-view />
  </el-config-provider>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zh from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
const { locale } = useI18n()
const elementLocale = computed(() => (locale.value === 'en' ? en : zh))
</script>
<style scoped>
:global(body) {
  margin: 0;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
}
</style>
