<template>
  <el-dialog :model-value="modelValue" :title="t('security.enrollment.validity.settings')"
    width="min(560px, calc(100vw - 24px))" :close-on-click-modal="!saving && !reloading" :close-on-press-escape="!saving && !reloading"
    :show-close="!saving && !reloading" @update:model-value="emit('update:modelValue', $event)">
    <p>{{ enrollment?.target_snapshot?.full_name }}</p>
    <el-alert v-if="conflict" type="warning" :closable="false" :title="t('security.enrollment.validity.conflict')" />
    <el-form label-position="top" @submit.prevent="emit('submit')">
      <ProtectionValidityFields :form="form" :disabled="saving || reloading" />
    </el-form>
    <template #footer>
      <el-button :disabled="saving || reloading" @click="emit('update:modelValue', false)">{{ t('security.common.cancel') }}</el-button>
      <el-button v-if="conflict" :loading="reloading" @click="emit('reload')">{{ t('security.enrollment.validity.reload') }}</el-button>
      <el-button type="primary" :loading="saving" :disabled="conflict || reloading" @click="emit('submit')">{{ t('security.common.save') }}</el-button>
    </template>
  </el-dialog>
</template>
<script setup>
import { useI18n } from 'vue-i18n'
import ProtectionValidityFields from './ProtectionValidityFields.vue'
defineProps({ modelValue: Boolean, saving: Boolean, reloading: Boolean, conflict: Boolean, enrollment: Object, form: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue', 'submit', 'reload'])
const { t } = useI18n()
</script>
