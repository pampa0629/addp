<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ $t('standard.login.title') }}</h2>
          <p class="subtitle">{{ $t('standard.login.subtitle') }}</p>
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
            :placeholder="$t('standard.login.usernamePlaceholder')"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            :placeholder="$t('standard.login.passwordPlaceholder')"
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
            {{ loading ? $t('standard.login.loggingIn') : $t('standard.login.login') }}
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
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const formRef = ref(null)

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = computed(() => ({
  username: [{ required: true, message: t('standard.login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('standard.login.passwordRequired'), trigger: 'blur' }]
}))

const loading = ref(false)

const handleLogin = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
  } catch {
    return
  }

  loading.value = true
  try {
    await authStore.login(loginForm.username, loginForm.password)
    ElMessage.success(t('standard.login.loginSuccess'))
    const redirect = route.query.redirect || '/domains'
    router.push(redirect)
  } catch (error) {
    ElMessage.error(error.message || t('standard.login.loginFailed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: var(--el-bg-color-page);
}

.login-box {
  width: 400px;
  box-shadow: var(--el-box-shadow-light);
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
