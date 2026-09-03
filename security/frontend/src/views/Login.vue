<template><div class="login"><el-card><h2>{{ t('security.login.title') }}</h2><el-form @submit.prevent="submit"><el-input v-model="username" :placeholder="t('security.login.username')"/><el-input v-model="password" type="password" show-password :placeholder="t('security.login.password')"/><el-button native-type="submit" type="primary" :loading="loading">{{ t('security.login.submit') }}</el-button></el-form></el-card></div></template>
<script setup>
import { ref } from 'vue'; import { useRouter } from 'vue-router'; import { useI18n } from 'vue-i18n'; import { ElMessage } from 'element-plus'; import { useAuthStore } from '../store/auth'
const username=ref(''), password=ref(''), loading=ref(false), auth=useAuthStore(), router=useRouter(); const {t}=useI18n()
async function submit(){loading.value=true;try{await auth.login(username.value,password.value);router.push('/sensitive-data-definitions')}catch(e){ElMessage.error(e.message||t('security.common.failed'))}finally{loading.value=false}}
</script>
<style scoped>.login{min-height:100vh;display:grid;place-items:center;background:var(--addp-bg-secondary)}.el-card{width:380px}.el-form{display:grid;gap:16px}</style>
