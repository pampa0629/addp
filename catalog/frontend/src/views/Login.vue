<template>
  <AuthLoginFlow
    :title="t('catalog.login.title')"
    :login="login"
    :verify-mfa="verifyMFA"
    :select-context="selectContext"
    @authenticated="handleAuthenticated"
  />
</template>

<script setup>
import { AuthLoginFlow, resolveLoginRedirect } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const login = (username, password) => authStore.login(username, password)
const verifyMFA = (challengeToken, code) => authStore.verifyMFA(challengeToken, code)
const selectContext = (selectionTicket, context) => authStore.selectContext(selectionTicket, context)

function handleAuthenticated() {
  router.replace(resolveLoginRedirect(route.query.redirect, '/entries'))
}
</script>
