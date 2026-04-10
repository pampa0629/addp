<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('system.login.title') }}</h2>
          <p class="subtitle">{{ t('system.login.subtitle') }}</p>
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
            :placeholder="t('system.login.usernamePlaceholder')"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            :placeholder="t('system.login.passwordPlaceholder')"
            :prefix-icon="Lock"
            size="large"
            show-password
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            size="large"
            style="width: 100%"
            :loading="loading"
            @click="handleLogin"
          >
            {{ loading ? t('system.login.loggingIn') : t('system.login.submit') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const formRef = ref(null)

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = computed(() => ({
  username: [
    { required: true, message: t('system.login.usernameRequired'), trigger: 'blur' }
  ],
  password: [
    { required: true, message: t('system.login.passwordRequired'), trigger: 'blur' },
    { min: 6, message: t('system.login.passwordMinLength'), trigger: 'blur' }
  ]
}))

const loading = ref(false)

const handleLogin = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      loading.value = true
      try {
        await authStore.login(loginForm.username, loginForm.password)
        ElMessage.success(t('system.login.success'))
        router.push('/')
      } catch (error) {
        console.error('Login failed:', error)

        let errorMessage = t('system.login.failed')

        if (error.response) {
          errorMessage = error.response.data?.error || t('system.login.requestFailed', { status: error.response.status })
        } else if (error.request) {
          errorMessage = t('system.login.networkError')
        } else {
          errorMessage = error.message || t('system.login.unknown')
        }

        ElMessage.error(errorMessage)
      } finally {
        loading.value = false
      }
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
  background: var(--addp-primary-gradient);
}

.login-box {
  width: 100%;
  max-width: 400px;
}

.card-header {
  text-align: center;
}

.card-header h2 {
  margin: 0;
  color: var(--addp-text-primary);
  font-size: 24px;
}

.card-header .subtitle {
  margin: 5px 0 0 0;
  color: var(--addp-text-tertiary);
  font-size: 14px;
}
</style>
