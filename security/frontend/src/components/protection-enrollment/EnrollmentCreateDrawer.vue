<template>
  <el-drawer
    :model-value="modelValue"
    :title="t('security.enrollment.create')"
    size="760px"
    destroy-on-close
    :before-close="beforeClose"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <div class="create-flow">
      <el-alert type="info" :closable="false" :title="t('security.enrollment.createHint')" />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item class="enrollment-resource-field" :label="t('security.enrollment.resource')" prop="resource" required>
          <ResourceTreePicker
            v-model="form.resource"
            api-base-url="/api/v1/meta"
            mode="item"
            :initial-locator="initialLocator"
            :engine-label="t('security.enrollment.engine')"
            :engine-placeholder="t('security.enrollment.enginePlaceholder')"
            :search-placeholder="t('security.enrollment.searchCurrentEngine')"
            :search-all-engines-placeholder="t('security.enrollment.searchAllEngines')"
            :search-empty-text="t('security.enrollment.resourceNotFound')"
            tree-height="min(52vh, 520px)"
            @update:model-value="emit('resourceModelUpdate', $event)"
            @select="emit('resourceSelect', $event)"
          />
        </el-form-item>
      </el-form>

      <el-skeleton v-if="selectedItemLoading" :rows="3" animated />
      <section v-else-if="selectedItem" class="selection-card">
        <div class="selection-card__title">
          <div>
            <strong>{{ selectedItem.name }}</strong>
            <span>{{ selectedItem.full_name }}</span>
          </div>
          <el-tag effect="plain">{{ itemTypeLabel(selectedItem.item_type) }}</el-tag>
        </div>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item :label="t('security.enrollment.engine')">
            {{ form.resource?.display?.engine_name || engineLabel(selectedItem.engine_id) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.lastScanned')">
            {{ formatDateTime(selectedItem.scanned_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.scope')" :span="2">
            {{ t('security.enrollment.wholeResourceScope') }}
          </el-descriptions-item>
        </el-descriptions>
        <el-alert
          v-if="existingEnrollment"
          class="existing-alert"
          type="warning"
          :closable="false"
          :title="t('security.enrollment.alreadyEnrolled')"
        />
      </section>
    </div>

    <template v-if="canCreate" #footer>
      <el-button :disabled="saving" @click="emit('close')">{{ t('security.common.cancel') }}</el-button>
      <el-button
        type="primary"
        :loading="saving"
        :disabled="selectedItemLoading || Boolean(existingEnrollment)"
        @click="emit('submit')"
      >
        {{ t('security.enrollment.confirmCreate') }}
      </el-button>
    </template>
  </el-drawer>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTreePicker } from '@common-ui'

defineProps({
  modelValue: { type: Boolean, default: false },
  canCreate: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  selectedItemLoading: { type: Boolean, default: false },
  selectedItem: { type: Object, default: null },
  existingEnrollment: { type: Object, default: null },
  initialLocator: { type: String, default: '' },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  itemTypeLabel: { type: Function, required: true },
  engineLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true },
  beforeClose: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'resourceModelUpdate', 'resourceSelect', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function validateField(field) {
  return formRef.value?.validateField(field) ?? Promise.resolve(false)
}

defineExpose({ validate, validateField })
</script>

<style scoped>
.create-flow { display: flex; flex-direction: column; gap: 18px; }
.selection-card { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.selection-card__title { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.selection-card__title div { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
.selection-card__title strong { font-size: 16px; }
.selection-card__title span { overflow: hidden; color: var(--addp-text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.existing-alert { margin-top: 14px; }
:deep(.el-drawer__body) { padding-top: 8px; }
</style>
