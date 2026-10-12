<template><SharedTransferConfigCheckReport :report="properties.report" /></template>

<script setup>
import { onUnmounted, ref } from 'vue'
import SharedTransferConfigCheckReport from '../../../../basic/src/components/TransferConfigCheckReport.vue'

const props = defineProps({ context: { type: Object, required: true } })
const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})
onUnmounted(() => subscription.unsubscribe())
</script>
