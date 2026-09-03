<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h2>{{ t('security.resources.sensitiveDataDefinition') }}</h2>
        <p>{{ t('security.descriptions.sensitiveDataDefinition') }}</p>
      </div>
    </header>

    <el-card class="definition-workspace">
      <SensitiveDataTypeList />
    </el-card>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import SensitiveDataTypeList from './SensitiveDataTypeList.vue'

const { t } = useI18n()
</script>

<style scoped>
.page { min-height: 100%; padding: 20px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.page-header { margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.definition-workspace { background: var(--addp-bg-primary); border-color: var(--addp-border-color); }
:deep(.definition-workspace > .el-card__body) { padding: 20px; }
</style>
