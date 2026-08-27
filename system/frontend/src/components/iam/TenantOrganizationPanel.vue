<template>
  <section class="iam-panel">
    <el-tabs v-model="activeSection" type="card">
      <el-tab-pane v-if="canReadDepartments" name="departments" :label="t('system.iam.organization.departments.title')">
        <DepartmentsPanel v-if="activeSection === 'departments'" />
      </el-tab-pane>
      <el-tab-pane v-if="canReadProjectGroups" name="project-groups" :label="t('system.iam.organization.projectGroups.title')">
        <ProjectGroupsPanel v-if="activeSection === 'project-groups'" />
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../store/auth'
import DepartmentsPanel from './DepartmentsPanel.vue'
import ProjectGroupsPanel from './ProjectGroupsPanel.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const canReadDepartments = computed(() => authStore.hasPermission('iam.department.read'))
const canReadProjectGroups = computed(() => authStore.hasPermission('iam.project_group.read'))
const activeSection = ref('departments')

watchEffect(() => {
  if (activeSection.value === 'departments' && canReadDepartments.value) return
  if (activeSection.value === 'project-groups' && canReadProjectGroups.value) return
  activeSection.value = canReadDepartments.value ? 'departments' : 'project-groups'
})
</script>
