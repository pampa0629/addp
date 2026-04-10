<template>
  <el-dialog
    :model-value="visible"
    :title="dialogTitle"
    width="500px"
    @update:model-value="$emit('update:visible', $event)"
  >
    <el-form
      ref="formRef"
      :model="userForm"
      :rules="rules"
      label-width="80px"
    >
      <el-form-item :label="t('system.user.form.username')" prop="username">
        <el-input v-model="userForm.username" :disabled="isEdit" :placeholder="t('system.user.form.usernamePlaceholder')" />
      </el-form-item>

      <!-- 密码字段只在创建新用户时显示 -->
      <el-form-item :label="t('system.user.form.password')" prop="password" v-if="!isEdit">
        <el-input
          v-model="userForm.password"
          type="password"
          show-password
          :placeholder="t('system.user.form.passwordPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('system.user.form.email')" prop="email">
        <el-input v-model="userForm.email" :placeholder="t('system.user.form.emailPlaceholder')" />
      </el-form-item>

      <el-form-item :label="t('system.user.form.fullName')" prop="full_name">
        <el-input v-model="userForm.full_name" :placeholder="t('system.user.form.fullNamePlaceholder')" />
      </el-form-item>

      <!-- 租户管理员创建用户时显示用户类型（固定为普通用户） -->
      <el-form-item :label="t('system.user.form.userType')" prop="user_type" v-if="currentUser?.user_type === 'tenant_admin'">
        <el-select v-model="userForm.user_type" :placeholder="t('system.user.form.userTypePlaceholder')" disabled>
          <el-option :label="t('system.user.form.normalUser')" value="user" />
        </el-select>
      </el-form-item>

      <!-- 只有租户管理员可以修改用户状态 -->
      <el-form-item :label="t('system.user.form.activeStatus')" v-if="isEdit && currentUser?.user_type === 'tenant_admin'">
        <el-switch v-model="userForm.is_active" :active-text="t('system.user.form.active')" :inactive-text="t('system.user.form.inactive')" />
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
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  visible: {
    type: Boolean,
    default: false
  },
  isEdit: {
    type: Boolean,
    default: false
  },
  editingUserId: {
    type: Number,
    default: null
  },
  userForm: {
    type: Object,
    required: true
  },
  rules: {
    type: Object,
    required: true
  },
  formRef: {
    type: Object,
    default: null
  },
  submitLoading: {
    type: Boolean,
    default: false
  },
  currentUser: {
    type: Object,
    default: null
  }
})

const emit = defineEmits(['update:visible', 'submit'])

const dialogTitle = computed(() => props.isEdit ? t('system.user.dialog.edit') : t('system.user.dialog.add'))

const handleClose = () => {
  emit('update:visible', false)
}

const handleSubmit = async () => {
  if (!props.formRef) return

  await props.formRef.validate(async (valid) => {
    if (valid) {
      emit('submit')
    }
  })
}

// 清除验证（对话框打开时）
watch(() => props.visible, (newVal) => {
  if (newVal) {
    nextTick(() => {
      props.formRef?.clearValidate()
    })
  }
})
</script>
