<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('meta.login.title') }}</h2>
          <p class="subtitle">Metadata Management</p>
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
            :placeholder="t('meta.login.usernamePlaceholder')"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            :placeholder="t('meta.login.passwordPlaceholder')"
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
            {{ loading ? t('meta.login.loggingIn') : t('meta.login.submit') }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
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

const rules = {
  username: [{ required: true, message: t('meta.login.usernamePlaceholder'), trigger: 'blur' }],
  password: [{ required: true, message: t('meta.login.passwordPlaceholder'), trigger: 'blur' }]
}

const loading = ref(false)

const handleLogin = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async valid => {
    if (!valid) return
    loading.value = true
    try {
      await authStore.login(loginForm.username, loginForm.password)
      ElMessage.success(t('meta.login.success'))

      const redirectPath = typeof route.query.redirect === 'string' ? route.query.redirect : null
      if (redirectPath) {
        router.push(redirectPath)
      } else {
        router.push({ name: 'MetadataScan' })
      }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('meta.login.failed'))
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
