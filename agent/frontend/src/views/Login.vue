<template>
  <div class="login-container">
    <div class="login-card">
      <div class="logo">
        <el-icon size="40"><ChatDotRound /></el-icon>
        <h2>{{ t('agent.login.title') }}</h2>
      </div>
      <el-form @submit.prevent="handleLogin" label-position="top">
        <el-form-item :label="t('agent.login.username')">
          <el-input v-model="form.username" :placeholder="t('agent.login.usernamePlaceholder')" size="large" />
        </el-form-item>
        <el-form-item :label="t('agent.login.password')">
          <el-input v-model="form.password" type="password" :placeholder="t('agent.login.passwordPlaceholder')" size="large" />
        </el-form-item>
        <el-button
          type="primary"
          size="large"
          style="width: 100%"
          :loading="loading"
          @click="handleLogin"
        >
          {{ t('agent.login.submit') }}
        </el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChatDotRound } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()

const form = ref({ username: '', password: '' })
const loading = ref(false)

async function handleLogin() {
  if (!form.value.username || !form.value.password) {
    ElMessage.warning(t('agent.login.emptyWarning'))
    return
  }
  loading.value = true
  try {
    await authStore.login(form.value.username, form.value.password)
    router.push('/agent')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || t('agent.login.failed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--addp-bg-secondary);
}
.login-card {
  width: 380px;
  padding: 40px;
  background: var(--addp-bg-primary);
  border-radius: 12px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.1);
}
.logo {
  text-align: center;
  margin-bottom: 32px;
  color: var(--el-color-primary);
}
.logo h2 {
  margin-top: 8px;
  font-size: 20px;
  color: var(--el-text-color-primary);
}
</style>
