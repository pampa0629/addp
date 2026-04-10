<template>
  <div>
    <el-card>
      <template #header>
        <span>{{ t('manager.directories.title') }}</span>
      </template>
      <el-empty :description="t('manager.directories.comingSoon')" />
    </el-card>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
