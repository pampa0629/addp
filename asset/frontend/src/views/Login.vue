<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('asset.login.title') }}</h2>
          <p class="subtitle">Asset</p>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="loginForm"
        :rules="rules"
        @submit.prevent="handleLogin"
      >
        <el-form-item prop="username">
          <el-input
            v-model="loginForm.username"
            :placeholder="t('asset.login.usernamePlaceholder')"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            :placeholder="t('asset.login.passwordPlaceholder')"
            :prefix-icon="Lock"
            size="large"
            show-password
          />
        </el-form-item>

        <el-form-item>
          <el-button
            native-type="submit"
            type="primary"
            size="large"
            style="width: 100%"
            :loading="loading"
          >
            {{ loading ? t('asset.login.loggingIn') : t('asset.login.submit') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { reactive, ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { t } = useI18n()
const formRef = ref(null)

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = computed(() => ({
  username: [{ required: true, message: t('asset.login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('asset.login.passwordRequired'), trigger: 'blur' }]
}))

const loading = ref(false)

const handleLogin = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async valid => {
    if (!valid) return
    loading.value = true
    try {
      await authStore.login(loginForm.username, loginForm.password)
      ElMessage.success(t('asset.login.success'))
      const redirect = route.query.redirect || '/assets'
      router.push(redirect)
    } catch (error) {
      ElMessage.error(error.message || t('asset.login.failed'))
    } finally {
      loading.value = false
    }
  })
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, var(--el-color-primary) 0%, var(--el-color-primary-dark-2) 100%);
}

.login-box {
  width: 400px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.1);
}

.card-header {
  text-align: center;
}

.card-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--el-text-color-primary);
}

.card-header .subtitle {
  margin: 8px 0 0 0;
  font-size: 14px;
  color: var(--el-text-color-secondary);
}
</style>
