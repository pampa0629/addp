<template>
  <el-dialog :model-value="modelValue" :title="t('agent.workspace.preferences')" width="min(480px, 90vw)" @update:model-value="emit('update:modelValue', $event)">
    <p>{{ t('agent.workspace.preferenceScope') }}</p>
    <el-form label-position="top">
      <el-form-item :label="t('agent.workspace.replyDetail')">
        <el-select v-model="draft.replyDetail" :aria-label="t('agent.workspace.replyDetail')">
          <el-option v-for="value in ['standard', 'concise', 'detailed']" :key="value" :value="value" :label="t(`agent.workspace.replyDetails.${value}`)" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('agent.workspace.sendShortcut')">
        <el-select v-model="draft.sendShortcut" :aria-label="t('agent.workspace.sendShortcut')">
          <el-option value="ctrl-enter" label="Ctrl+Enter" /><el-option value="enter" label="Enter" />
        </el-select>
      </el-form-item>
      <el-form-item><el-checkbox v-model="draft.expandDetails">{{ t('agent.workspace.expandDetails') }}</el-checkbox></el-form-item>
    </el-form>
    <template #footer><el-button @click="emit('update:modelValue', false)">{{ t('agent.workspace.close') }}</el-button><el-button type="primary" @click="emit('save', draft)">{{ t('agent.workspace.save') }}</el-button></template>
  </el-dialog>
</template>
<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps({ modelValue: Boolean, preferences: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue', 'save'])
const { t } = useI18n()
const draft = ref({ ...props.preferences })
watch(() => props.modelValue, visible => { if (visible) draft.value = { ...props.preferences } })
</script>
