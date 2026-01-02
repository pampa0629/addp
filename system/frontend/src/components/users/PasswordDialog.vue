<template>
  <el-dialog
    :model-value="visible"
    title="修改密码"
    width="500px"
    @update:model-value="$emit('update:visible', $event)"
  >
    <el-form
      ref="passwordFormRef"
      :model="passwordForm"
      :rules="passwordRules"
      label-width="100px"
    >
      <el-form-item label="旧密码" prop="old_password">
        <el-input
          v-model="passwordForm.old_password"
          type="password"
          show-password
          placeholder="请输入旧密码"
        />
      </el-form-item>

      <el-form-item label="新密码" prop="new_password">
        <el-input
          v-model="passwordForm.new_password"
          type="password"
          show-password
          placeholder="请输入新密码（至少6位）"
        />
      </el-form-item>

      <el-form-item label="确认密码" prop="confirm_password">
        <el-input
          v-model="passwordForm.confirm_password"
          type="password"
          show-password
          placeholder="请再次输入新密码"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleClose">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitLoading">
          确定
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup>
import { watch, nextTick } from 'vue'

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
