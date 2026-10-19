<template>
  <div class="totp-details">
    <canvas ref="canvas" :aria-label="t('system.iam.security.qrCode')" />
    <span>{{ t('system.iam.security.manualKey') }}</span>
    <code>{{ secret }}</code>
  </div>
</template>
<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
const props = defineProps({ secret: { type: String, required: true }, uri: { type: String, required: true } })
const { t } = useI18n()
const canvas = ref()
let disposed = false
async function draw() { if (!disposed && canvas.value && props.uri) await QRCode.toCanvas(canvas.value, props.uri, { width: 208, margin: 1 }) }
onMounted(draw)
watch(() => props.uri, draw)
onBeforeUnmount(() => { disposed = true; if (canvas.value) canvas.value.getContext('2d')?.clearRect(0, 0, canvas.value.width, canvas.value.height) })
</script>
<style scoped>
.totp-details { display: flex; flex-direction: column; align-items: center; gap: 8px; min-width: 0; color: var(--addp-text-secondary); }
.totp-details canvas { width: 208px; height: 208px; border: 1px solid var(--addp-border-color); }
.totp-details code { max-width: 100%; overflow-wrap: anywhere; color: var(--addp-text-primary); }
</style>
