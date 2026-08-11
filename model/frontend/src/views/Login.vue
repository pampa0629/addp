<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('model.login.title') }}</h2>
          <p class="subtitle">Model</p>
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
            :placeholder="t('model.login.username_placeholder')"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            :placeholder="t('model.login.password_placeholder')"
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
            {{ loading ? t('model.login.logging_in') : t('model.login.login') }}
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
import { useAuthStore } from '../store/auth'
import { useI18n } from 'vue-i18n'

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
  username: [{ required: true, message: t('model.login.username_required'), trigger: 'blur' }],
  password: [{ required: true, message: t('model.login.password_required'), trigger: 'blur' }]
}

const loading = ref(false)

const handleLogin = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async valid => {
    if (!valid) return

    loading.value = true
    try {
      await authStore.login(loginForm.username, loginForm.password)
      ElMessage.success(t('model.login.login_success'))
      const redirect = route.query.redirect || '/dw-layers'
      router.push(redirect)
    } catch (error) {
      ElMessage.error(error.message || t('model.login.login_failed'))
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
  width: 400px;
  box-shadow: var(--addp-shadow-card);
}

.card-header {
  text-align: center;
}

.card-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.card-header .subtitle {
  margin: 8px 0 0 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
}
</style>
