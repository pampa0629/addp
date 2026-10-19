<template>
  <iframe class="administrator-recovery-frame" src="/module-ui/system/administrator-recovery" :title="t('console.administratorRecovery')" referrerpolicy="no-referrer" />
</template>
<script setup>
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>
<style scoped>
.administrator-recovery-frame { display: block; width: 100%; height: 100vh; border: 0; }
</style>
