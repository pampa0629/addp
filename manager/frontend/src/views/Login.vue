<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <h2>{{ t('manager.login.title') }}</h2>
      </template>
      <el-form :model="form" @submit.prevent="handleLogin">
        <el-form-item :label="t('manager.login.username')">
          <el-input v-model="form.username" :placeholder="t('manager.login.usernamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('manager.login.password')">
          <el-input v-model="form.password" type="password" :placeholder="t('manager.login.passwordPlaceholder')" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">
            {{ t('manager.login.submit') }}
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

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()

const form = ref({
  username: '',
  password: ''
})

const loading = ref(false)

const handleLogin = async () => {
  if (!form.value.username || !form.value.password) {
    ElMessage.warning(t('manager.login.required'))
    return
  }

  loading.value = true
  try {
    await authStore.login(form.value.username, form.value.password)
    ElMessage.success(t('manager.login.success'))
    router.push('/')
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('manager.login.failed'))
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
  width: 400px;
}

h2 {
  text-align: center;
  margin: 0;
}
</style>
