<template>
  <div>
    <el-table :data="users" v-loading="loading" stripe>
      <el-table-column prop="id" :label="t('system.user.columns.id')" width="80" />
      <el-table-column prop="username" :label="t('system.user.columns.username')" />
      <el-table-column prop="email" :label="t('system.user.columns.email')" />
      <el-table-column prop="full_name" :label="t('system.user.columns.fullName')" />
      <el-table-column :label="t('system.user.columns.userType')" width="120">
        <template #default="{ row }">
          <el-tag :type="getUserTypeTag(row.user_type)">
            {{ getUserTypeText(row.user_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.user.columns.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'danger'">
            {{ row.is_active ? t('system.user.status.active') : t('system.user.status.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.user.columns.createdAt')" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('system.user.columns.actions')" width="250" fixed="right">
        <template #default="{ row }">
          <!-- 普通用户只能编辑自己，可以修改自己的密码 -->
          <template v-if="currentUser?.user_type === 'user'">
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="primary"
              :icon="Edit"
              @click="$emit('edit', row)"
            >{{ t('system.user.actions.edit') }}</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >{{ t('system.user.actions.changePassword') }}</el-button>
          </template>
          <!-- 租户管理员可以编辑所有用户，可以修改自己的密码，可以删除普通用户 -->
          <template v-else-if="currentUser?.user_type === 'tenant_admin'">
            <el-button size="small" type="primary" :icon="Edit" @click="$emit('edit', row)">{{ t('system.user.actions.edit') }}</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >{{ t('system.user.actions.changePassword') }}</el-button>
            <el-button
              v-if="row.user_type === 'user'"
              size="small"
              type="danger"
              :icon="Delete"
              @click="$emit('delete', row)"
            >{{ t('system.user.actions.delete') }}</el-button>
          </template>
          <!-- 超级管理员可以编辑自己，可以修改自己的密码 -->
          <template v-else-if="currentUser?.user_type === 'super_admin'">
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="primary"
              :icon="Edit"
              @click="$emit('edit', row)"
            >{{ t('system.user.actions.edit') }}</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >{{ t('system.user.actions.changePassword') }}</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      :current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      layout="total, prev, pager, next"
      style="margin-top: 20px; justify-content: flex-end"
      @current-change="$emit('page-change', $event)"
    />
  </div>
</template>

<script setup>
import { Edit, Delete, Key } from '@element-plus/icons-vue'
import { formatDate } from '@common-ui/utils/formatters'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

defineProps({
  users: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  currentPage: {
    type: Number,
    default: 1
  },
  pageSize: {
    type: Number,
    default: 10
  },
  total: {
    type: Number,
    default: 0
  },
  currentUser: {
    type: Object,
    default: null
  },
  getUserTypeText: {
    type: Function,
    required: true
  },
  getUserTypeTag: {
    type: Function,
    required: true
  }
})

defineEmits(['edit', 'delete', 'change-password', 'page-change'])
</script>
