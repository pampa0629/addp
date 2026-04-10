<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="login-header">
          <el-icon :size="32"><Platform /></el-icon>
          <h2>{{ t('console.title') }}</h2>
        </div>
      </template>
      <el-form :model="form" @submit.prevent="handleLogin">
        <el-form-item :label="t('console.login.username')">
          <el-input v-model="form.username" :placeholder="t('console.login.usernamePlaceholder')" size="large">
            <template #prefix>
              <el-icon><User /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('console.login.password')">
          <el-input v-model="form.password" type="password" :placeholder="t('console.login.passwordPlaceholder')" size="large">
            <template #prefix>
              <el-icon><Lock /></el-icon>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" size="large" style="width: 100%">
            {{ loading ? t('console.login.loggingIn') : t('console.login.submit') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()

const form = ref({
  username: '',
  password: ''
})

const loading = ref(false)

const handleLogin = async () => {
  if (!form.value.username || !form.value.password) {
    ElMessage.warning(t('console.login.inputRequired'))
    return
  }

  loading.value = true
  try {
    await authStore.login(form.value.username, form.value.password)
    ElMessage.success(t('console.login.success'))
    router.push('/')
  } catch (error) {
    console.error('Login failed:', error)

    let errorMessage = t('console.login.failed')

    if (error.response) {
      errorMessage = error.response.data?.error || t('console.login.requestFailed', { status: error.response.status })
    } else if (error.request) {
      errorMessage = t('console.login.networkError')
    } else {
      errorMessage = error.message || t('console.login.unknown')
    }

    ElMessage.error(errorMessage)
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
  height: 100vh;
  background: var(--addp-primary-gradient);
}

.login-card {
  width: 450px;
}

.login-header {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}

.login-header h2 {
  text-align: center;
  margin: 0;
  background: var(--addp-primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}
</style>
