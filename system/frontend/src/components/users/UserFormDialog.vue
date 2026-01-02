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
      <el-form-item label="用户名" prop="username">
        <el-input v-model="userForm.username" :disabled="isEdit" placeholder="请输入用户名" />
      </el-form-item>

      <!-- 密码字段只在创建新用户时显示 -->
      <el-form-item label="密码" prop="password" v-if="!isEdit">
        <el-input
          v-model="userForm.password"
          type="password"
          show-password
          placeholder="请输入密码"
        />
      </el-form-item>

      <el-form-item label="邮箱" prop="email">
        <el-input v-model="userForm.email" placeholder="请输入邮箱" />
      </el-form-item>

      <el-form-item label="姓名" prop="full_name">
        <el-input v-model="userForm.full_name" placeholder="请输入姓名" />
      </el-form-item>

      <!-- 租户管理员创建用户时显示用户类型（固定为普通用户） -->
      <el-form-item label="用户类型" prop="user_type" v-if="currentUser?.user_type === 'tenant_admin'">
        <el-select v-model="userForm.user_type" placeholder="请选择用户类型" disabled>
          <el-option label="普通用户" value="user" />
        </el-select>
      </el-form-item>

      <!-- 只有租户管理员可以修改用户状态 -->
      <el-form-item label="状态" v-if="isEdit && currentUser?.user_type === 'tenant_admin'">
        <el-switch v-model="userForm.is_active" active-text="激活" inactive-text="禁用" />
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
import { ref, computed, watch, nextTick } from 'vue'

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

const dialogTitle = computed(() => props.isEdit ? '编辑用户' : '新增用户')

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
