<template>
  <AuthLoginFlow
    :title="t('system.login.title')"
    :login="login"
    :verify-mfa="verifyMFA"
    :select-context="selectContext"
    @authenticated="handleAuthenticated"
  />
</template>

<script setup>
import { AuthLoginFlow } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { t } = useI18n()

const login = (username, password) => authStore.login(username, password)
const verifyMFA = (challengeToken, code) => authStore.verifyMFA(challengeToken, code)
const selectContext = (selectionTicket, context) => authStore.selectContext(selectionTicket, context)

function handleAuthenticated() {
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
    ? route.query.redirect
    : '/'
  router.push(redirect)
}
</script>
