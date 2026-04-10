<template>
  <el-dialog
    :model-value="visible"
    :title="t('system.password.title')"
    width="500px"
    @update:model-value="$emit('update:visible', $event)"
  >
    <el-form
      ref="passwordFormRef"
      :model="passwordForm"
      :rules="passwordRules"
      label-width="100px"
    >
      <el-form-item :label="t('system.password.old')" prop="old_password">
        <el-input
          v-model="passwordForm.old_password"
          type="password"
          show-password
          :placeholder="t('system.password.oldPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('system.password.new')" prop="new_password">
        <el-input
          v-model="passwordForm.new_password"
          type="password"
          show-password
          :placeholder="t('system.password.newPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('system.password.confirm')" prop="confirm_password">
        <el-input
          v-model="passwordForm.confirm_password"
          type="password"
          show-password
          :placeholder="t('system.password.confirmPlaceholder')"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleClose">{{ t('system.engine.actions.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitLoading">
          OK
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup>
import { watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  visible: {
    type: Boolean,
    default: false
  },
  passwordForm: {
    type: Object,
    required: true
  },
  passwordRules: {
    type: Object,
    required: true
  },
  passwordFormRef: {
    type: Object,
    default: null
  },
  submitLoading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:visible', 'submit'])

const handleClose = () => {
  emit('update:visible', false)
}

const handleSubmit = async () => {
  if (!props.passwordFormRef) return

  await props.passwordFormRef.validate(async (valid) => {
    if (valid) {
      emit('submit')
    }
  })
}

// 清除验证（对话框打开时）
watch(() => props.visible, (newVal) => {
  if (newVal) {
    nextTick(() => {
      props.passwordFormRef?.clearValidate()
    })
  }
})
</script>
