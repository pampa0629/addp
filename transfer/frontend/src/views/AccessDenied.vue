<template>
  <el-result
    icon="warning"
    :title="t('transfer.accessDenied.title')"
    :sub-title="t('transfer.accessDenied.description')"
  />
</template>

<script setup>
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
