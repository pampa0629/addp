<template>
  <AuthLoginFlow
    :title="t('ontology.title')"
    :login="auth.login"
    :verify-mfa="auth.verifyMFA"
    :select-context="auth.selectContext"
    @authenticated="authenticated"
  />
</template>
<script setup>
import { useI18n } from 'vue-i18n'
import { useRouter, useRoute } from 'vue-router'
import { AuthLoginFlow, resolveLoginRedirect } from '@common-ui'
import { useAuthStore } from '../store/auth'
const { t } = useI18n(),
  router = useRouter(),
  route = useRoute(),
  auth = useAuthStore()
const authenticated = () =>
  router.replace(resolveLoginRedirect(route.query.redirect, '/ontologies'))
</script>
