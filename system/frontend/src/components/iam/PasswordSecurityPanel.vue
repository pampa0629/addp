<template>
  <div class="password-security">
    <div><strong>{{ t('system.iam.account.changePassword') }}</strong><p>{{ t('system.iam.account.passwordDescription') }}</p></div>
    <el-button @click="visible = true">{{ t('system.iam.account.changePassword') }}</el-button>
    <el-dialog v-model="visible" :title="t('system.iam.account.changePassword')" width="min(520px, calc(100% - 24px))" :close-on-click-modal="false" @closed="reset">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('system.iam.security.currentPassword')" prop="current_password"><el-input v-model="form.current_password" type="password" show-password autocomplete="current-password" /></el-form-item>
        <el-form-item :label="t('system.iam.users.newPassword')" prop="new_password"><el-input v-model="form.new_password" type="password" show-password autocomplete="new-password" /></el-form-item>
        <el-form-item :label="t('system.iam.users.confirmPassword')" prop="confirm_password"><el-input v-model="form.confirm_password" type="password" show-password autocomplete="new-password" @keyup.enter="submit" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="visible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('system.iam.account.changePassword') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()
const visible = ref(false)
const submitting = ref(false)
const formRef = ref()
const form = reactive({ current_password: '', new_password: '', confirm_password: '' })
let disposed = false
const rules = computed(() => ({
  current_password: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  new_password: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  confirm_password: [{ validator: (_rule, value, callback) => value && value === form.new_password ? callback() : callback(new Error(t('system.iam.users.passwordMismatch'))), trigger: 'blur' }]
}))
function reset() { Object.assign(form, { current_password: '', new_password: '', confirm_password: '' }); formRef.value?.clearValidate() }
async function submit() {
  if (submitting.value) return
  submitting.value = true
  try { await formRef.value.validate() } catch { submitting.value = false; return }
  if (disposed || !visible.value) { submitting.value = false; return }
  try {
    await iamAPI.self.changePassword({ current_password: form.current_password, new_password: form.new_password })
    if (disposed) return
    reset()
    ElMessage.success(t('system.iam.account.passwordChanged'))
    await authStore.logout()
  } catch (error) {
    if (!disposed) ElMessage.error(error.response?.data?.error || t('system.iam.account.changePasswordFailed'))
  } finally { submitting.value = false }
}
onBeforeUnmount(() => { disposed = true; reset() })
</script>

<style scoped>
.password-security { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 16px 0; border-bottom: 1px solid var(--addp-border-color); }
.password-security strong { font-size: 14px; }
.password-security p { margin: 4px 0 0; font-size: 13px; color: var(--addp-text-secondary); }
@media (max-width: 620px) { .password-security { align-items: flex-start; flex-direction: column; } }
</style>
