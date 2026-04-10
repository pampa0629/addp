<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('graph.login.title') }}</h2>
          <p class="subtitle">{{ t('graph.login.subtitle') }}</p>
        </div>
      </template>
      <el-form ref="formRef" :model="loginForm" :rules="rules" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="loginForm.username" :placeholder="t('graph.login.username')" :prefix-icon="User" size="large" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="loginForm.password" type="password" :placeholder="t('graph.login.password')"
            :prefix-icon="Lock" size="large" show-password />
        </el-form-item>
        <el-form-item>
          <el-button native-type="submit" type="primary" size="large" style="width:100%" :loading="loading">
            {{ loading ? t('graph.login.loggingIn') : t('graph.login.loginBtn') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const loginForm = ref({ username: '', password: '' })
const rules = computed(() => ({
  username: [{ required: true, message: t('graph.login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('graph.login.passwordRequired'), trigger: 'blur' }]
}))

const handleLogin = async () => {
  try {
    await formRef.value.validate()
    loading.value = true
    await authStore.login(loginForm.value.username, loginForm.value.password)
    router.push('/')
  } catch (err) {
    ElMessage.error(err.message || t('graph.login.loginFailed'))
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
.login-box { width: 400px; }
.card-header { text-align: center; }
.card-header h2 { margin: 0; color: var(--addp-text-primary); }
.subtitle { margin: 8px 0 0; color: var(--addp-text-tertiary); font-size: 14px; }
</style>
