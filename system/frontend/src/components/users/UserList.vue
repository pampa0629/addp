<template>
  <div>
    <el-table :data="users" v-loading="loading" stripe>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="username" label="用户名" />
      <el-table-column prop="email" label="邮箱" />
      <el-table-column prop="full_name" label="姓名" />
      <el-table-column label="用户类型" width="120">
        <template #default="{ row }">
          <el-tag :type="getUserTypeTag(row.user_type)">
            {{ getUserTypeText(row.user_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'danger'">
            {{ row.is_active ? '激活' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="250" fixed="right">
        <template #default="{ row }">
          <!-- 普通用户只能编辑自己，可以修改自己的密码 -->
          <template v-if="currentUser?.user_type === 'user'">
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="primary"
              :icon="Edit"
              @click="$emit('edit', row)"
            >编辑</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >修改密码</el-button>
          </template>
          <!-- 租户管理员可以编辑所有用户，可以修改自己的密码，可以删除普通用户 -->
          <template v-else-if="currentUser?.user_type === 'tenant_admin'">
            <el-button size="small" type="primary" :icon="Edit" @click="$emit('edit', row)">编辑</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >修改密码</el-button>
            <el-button
              v-if="row.user_type === 'user'"
              size="small"
              type="danger"
              :icon="Delete"
              @click="$emit('delete', row)"
            >删除</el-button>
          </template>
          <!-- 超级管理员可以编辑自己，可以修改自己的密码 -->
          <template v-else-if="currentUser?.user_type === 'super_admin'">
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="primary"
              :icon="Edit"
              @click="$emit('edit', row)"
            >编辑</el-button>
            <el-button
              v-if="row.id === currentUser?.id"
              size="small"
              type="warning"
              :icon="Key"
              @click="$emit('change-password', row)"
            >修改密码</el-button>
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
