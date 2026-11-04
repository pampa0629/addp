<template>
  <AuthLoginFlow
    :title="t('workbench.login.title')"
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
const auth = useAuthStore()

const login = (username, password) => auth.login(username, password)
const verifyMFA = (challengeToken, code) => auth.verifyMFA(challengeToken, code)
const selectContext = (selectionTicket, context) => auth.selectContext(selectionTicket, context)

function handleAuthenticated() {
  const target = resolveLoginRedirect(route.query.redirect, '/applications')
  // The runtime and module entry use different history bases; reload its owner document.
  if (target.startsWith('/data-apps/')) {
    window.location.replace(target)
    return
  }
  router.replace(target)
}
</script>
