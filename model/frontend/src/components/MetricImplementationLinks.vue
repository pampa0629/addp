<template>
  <el-card shadow="never" v-loading="loading">
    <template #header
      ><div class="metric-links-header">
        <span>{{ t("model.metric_workspace.title") }}</span
        ><el-button
          v-if="can('create')"
          link
          type="primary"
          @click="open('new')"
          >{{ t("model.metric_workspace.create") }}</el-button
        >
      </div></template
    >
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-empty
      v-else-if="!items.length"
      :description="t('model.metric_workspace.empty')"
      :image-size="48"
    />
    <div v-for="item in items" :key="item.id">
      <el-button link type="primary" @click="open(item.id)">{{
        item.name
      }}</el-button>
    </div>
  </el-card>
</template>
<script setup>
import { ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { metricImplementationAPI } from "../api/model";
import { useAuthStore } from "../store/auth";
import { navigateModelRoute } from "../utils/moduleNavigation";
import { getModelErrorMessage } from "../utils/apiError";
const props = defineProps({ tableId: { type: Number, required: true } });
const { t } = useI18n(),
  router = useRouter(),
  auth = useAuthStore();
const items = ref([]),
  loading = ref(false),
  error = ref("");
const can = (action) =>
  auth.hasPermission(`model.metric_implementation.${action}`);
const open = (id) =>
  navigateModelRoute(router, {
    path: `/metric-implementations/${id}`,
    query: { fact_table_id: props.tableId },
  });
let generation = 0;
watch(
  () => props.tableId,
  async (id) => {
    const request = ++generation;
    items.value = [];
    error.value = "";
    if (!can("read")) {
      error.value = t("model.metric_workspace.unavailable");
      return;
    }
    loading.value = true;
    try {
      const result = await metricImplementationAPI.list({ fact_table_id: id });
      if (request === generation) items.value = result;
    } catch (err) {
      if (request === generation)
        error.value = getModelErrorMessage(
          err,
          t,
          "model.metric_workspace.load_failed",
        );
    } finally {
      if (request === generation) loading.value = false;
    }
  },
  { immediate: true },
);
</script>
<style scoped>
.metric-links-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
</style>
