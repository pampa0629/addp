<template>
  <el-form-item :label="t('security.enrollment.validity.settings')">
    <el-radio-group v-model="form.validity_mode" :disabled="disabled">
      <el-radio value="permanent">{{ t('security.enrollment.validity.permanent') }}</el-radio>
      <el-radio value="until">{{ t('security.enrollment.validity.specified') }}</el-radio>
    </el-radio-group>
  </el-form-item>
  <template v-if="form.validity_mode === 'until'">
    <el-form-item :label="t('security.enrollment.validity.deadline')" required>
      <el-date-picker v-model="form.expires_at" type="datetime" :disabled="disabled" />
    </el-form-item>
    <el-alert type="warning" :closable="false" :title="t('security.enrollment.validity.deadlineHint')" />
  </template>
</template>
<script setup>
import { useI18n } from 'vue-i18n'
defineProps({ form: { type: Object, required: true }, disabled: Boolean })
const { t } = useI18n()
</script>
