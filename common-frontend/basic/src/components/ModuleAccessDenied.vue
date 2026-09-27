<template>
  <div class="module-access-denied">
    <el-result icon="warning" :title="t('moduleAccessDenied.title')" :sub-title="t('moduleAccessDenied.description')" />
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<style scoped>
.module-access-denied {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: var(--addp-bg-secondary);
}
</style>
