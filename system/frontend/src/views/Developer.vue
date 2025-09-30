<template>
  <div class="page-container">
    <el-row :gutter="20">
      <el-col :span="24">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>开发中心</span>
              <el-tag type="info">API 文档</el-tag>
            </div>
          </template>

          <el-alert
            title="API 基础信息"
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p><strong>Base URL:</strong> http://localhost:8080</p>
            <p><strong>认证方式:</strong> Bearer Token (JWT)</p>
            <p><strong>Content-Type:</strong> application/json</p>
          </el-alert>

          <el-tabs v-model="activeTab" type="border-card">
            <!-- 认证接口 -->
            <el-tab-pane label="认证接口" name="auth">
              <div class="api-section" v-for="api in authApis" :key="api.path">
                <el-descriptions :title="api.name" :column="1" border>
                  <el-descriptions-item label="请求方法">
                    <el-tag :type="getMethodType(api.method)">{{ api.method }}</el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="接口路径">
                    <el-text type="primary">{{ api.path }}</el-text>
                  </el-descriptions-item>
                  <el-descriptions-item label="功能说明">
                    {{ api.description }}
                  </el-descriptions-item>
                  <el-descriptions-item label="是否需要认证">
                    <el-tag :type="api.auth ? 'danger' : 'success'">
                      {{ api.auth ? '需要' : '不需要' }}
                    </el-tag>
                  </el-descriptions-item>
                </el-descriptions>

                <div class="code-block" v-if="api.request">
                  <div class="code-title">请求参数示例</div>
                  <pre>{{ api.request }}</pre>
                </div>

                <div class="code-block" v-if="api.response">
                  <div class="code-title">响应示例</div>
                  <pre>{{ api.response }}</pre>
                </div>
              </div>
            </el-tab-pane>

            <!-- 用户管理接口 -->
            <el-tab-pane label="用户管理" name="users">
              <div class="api-section" v-for="api in userApis" :key="api.path">
                <el-descriptions :title="api.name" :column="1" border>
                  <el-descriptions-item label="请求方法">
                    <el-tag :type="getMethodType(api.method)">{{ api.method }}</el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="接口路径">
                    <el-text type="primary">{{ api.path }}</el-text>
                  </el-descriptions-item>
                  <el-descriptions-item label="功能说明">
                    {{ api.description }}
                  </el-descriptions-item>
                  <el-descriptions-item label="是否需要认证">
                    <el-tag :type="api.auth ? 'danger' : 'success'">
                      {{ api.auth ? '需要' : '不需要' }}
                    </el-tag>
                  </el-descriptions-item>
                </el-descriptions>

                <div class="code-block" v-if="api.params">
                  <div class="code-title">路径参数</div>
                  <pre>{{ api.params }}</pre>
                </div>

                <div class="code-block" v-if="api.query">
                  <div class="code-title">查询参数</div>
                  <pre>{{ api.query }}</pre>
                </div>

                <div class="code-block" v-if="api.request">
                  <div class="code-title">请求参数示例</div>
                  <pre>{{ api.request }}</pre>
                </div>

                <div class="code-block" v-if="api.response">
                  <div class="code-title">响应示例</div>
                  <pre>{{ api.response }}</pre>
                </div>
              </div>
            </el-tab-pane>

            <!-- 日志管理接口 -->
            <el-tab-pane label="日志管理" name="logs">
              <div class="api-section" v-for="api in logApis" :key="api.path">
                <el-descriptions :title="api.name" :column="1" border>
                  <el-descriptions-item label="请求方法">
                    <el-tag :type="getMethodType(api.method)">{{ api.method }}</el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="接口路径">
                    <el-text type="primary">{{ api.path }}</el-text>
                  </el-descriptions-item>
                  <el-descriptions-item label="功能说明">
                    {{ api.description }}
                  </el-descriptions-item>
                  <el-descriptions-item label="是否需要认证">
                    <el-tag type="danger">需要</el-tag>
                  </el-descriptions-item>
                </el-descriptions>

                <div class="code-block" v-if="api.query">
                  <div class="code-title">查询参数</div>
                  <pre>{{ api.query }}</pre>
                </div>

                <div class="code-block" v-if="api.response">
                  <div class="code-title">响应示例</div>
                  <pre>{{ api.response }}</pre>
                </div>
              </div>
            </el-tab-pane>

            <!-- 资源管理接口 -->
            <el-tab-pane label="资源管理" name="resources">
              <div class="api-section" v-for="api in resourceApis" :key="api.path">
                <el-descriptions :title="api.name" :column="1" border>
                  <el-descriptions-item label="请求方法">
                    <el-tag :type="getMethodType(api.method)">{{ api.method }}</el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item label="接口路径">
                    <el-text type="primary">{{ api.path }}</el-text>
                  </el-descriptions-item>
                  <el-descriptions-item label="功能说明">
                    {{ api.description }}
                  </el-descriptions-item>
                  <el-descriptions-item label="是否需要认证">
                    <el-tag type="danger">需要</el-tag>
                  </el-descriptions-item>
                </el-descriptions>

                <div class="code-block" v-if="api.query">
                  <div class="code-title">查询参数</div>
                  <pre>{{ api.query }}</pre>
                </div>

                <div class="code-block" v-if="api.request">
                  <div class="code-title">请求参数示例</div>
                  <pre>{{ api.request }}</pre>
                </div>

                <div class="code-block" v-if="api.response">
                  <div class="code-title">响应示例</div>
                  <pre>{{ api.response }}</pre>
                </div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const activeTab = ref('auth')

const getMethodType = (method) => {
  const types = {
    'GET': 'success',
    'POST': 'primary',
    'PUT': 'warning',
    'DELETE': 'danger'
  }
  return types[method] || 'info'
}

// 认证接口
const authApis = [
  {
    name: '用户注册',
    method: 'POST',
    path: '/api/auth/register',
    description: '注册新用户账号',
    auth: false,
    request: `{
  "username": "testuser",
  "password": "password123",
  "email": "test@example.com",
  "full_name": "测试用户"
}`,
    response: `{
  "id": 1,
  "username": "testuser",
  "email": "test@example.com",
  "full_name": "测试用户",
  "is_active": true,
  "is_superuser": false,
  "created_at": "2025-09-30T16:54:08.539068+08:00",
  "updated_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '用户登录',
    method: 'POST',
    path: '/api/auth/login',
    description: '用户登录获取访问令牌',
    auth: false,
    request: `{
  "username": "admin",
  "password": "admin123"
}`,
    response: `{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}`
  }
]

// 用户管理接口
const userApis = [
  {
    name: '获取当前用户信息',
    method: 'GET',
    path: '/api/users/me',
    description: '获取当前登录用户的详细信息',
    auth: true,
    response: `{
  "id": 1,
  "username": "admin",
  "email": "admin@test.com",
  "full_name": "管理员",
  "is_active": true,
  "is_superuser": false,
  "created_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '获取用户列表',
    method: 'GET',
    path: '/api/users',
    description: '获取用户列表，支持分页',
    auth: true,
    query: `page: 页码，默认 1
page_size: 每页数量，默认 10`,
    response: `[
  {
    "id": 1,
    "username": "admin",
    "email": "admin@test.com",
    "full_name": "管理员",
    "is_active": true,
    "is_superuser": false,
    "created_at": "2025-09-30T16:54:08.539068+08:00"
  }
]`
  },
  {
    name: '获取指定用户',
    method: 'GET',
    path: '/api/users/:id',
    description: '根据用户 ID 获取用户详细信息',
    auth: true,
    params: ':id - 用户 ID',
    response: `{
  "id": 1,
  "username": "admin",
  "email": "admin@test.com",
  "full_name": "管理员",
  "is_active": true,
  "is_superuser": false,
  "created_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '更新用户信息',
    method: 'PUT',
    path: '/api/users/:id',
    description: '更新指定用户的信息',
    auth: true,
    params: ':id - 用户 ID',
    request: `{
  "email": "newemail@test.com",
  "full_name": "新名字",
  "password": "newpassword123",  // 可选
  "is_active": true
}`,
    response: `{
  "id": 1,
  "username": "admin",
  "email": "newemail@test.com",
  "full_name": "新名字",
  "is_active": true,
  "is_superuser": false,
  "created_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '删除用户',
    method: 'DELETE',
    path: '/api/users/:id',
    description: '删除指定用户',
    auth: true,
    params: ':id - 用户 ID',
    response: `{
  "message": "删除成功"
}`
  }
]

// 日志管理接口
const logApis = [
  {
    name: '获取日志列表',
    method: 'GET',
    path: '/api/logs',
    description: '获取审计日志列表，支持分页和用户过滤',
    auth: true,
    query: `page: 页码，默认 1
page_size: 每页数量，默认 20
user_id: 用户 ID（可选，用于过滤特定用户的日志）`,
    response: `[
  {
    "id": 1,
    "user_id": 1,
    "username": "admin",
    "action": "POST /api/auth/register",
    "resource_type": "",
    "resource_id": "",
    "details": "",
    "ip_address": "127.0.0.1",
    "created_at": "2025-09-30T16:54:08.539068+08:00"
  }
]`
  },
  {
    name: '获取指定日志',
    method: 'GET',
    path: '/api/logs/:id',
    description: '根据日志 ID 获取日志详细信息',
    auth: true,
    response: `{
  "id": 1,
  "user_id": 1,
  "username": "admin",
  "action": "POST /api/auth/register",
  "resource_type": "",
  "resource_id": "",
  "details": "",
  "ip_address": "127.0.0.1",
  "created_at": "2025-09-30T16:54:08.539068+08:00"
}`
  }
]

// 资源管理接口
const resourceApis = [
  {
    name: '创建资源',
    method: 'POST',
    path: '/api/resources',
    description: '创建新的资源配置',
    auth: true,
    request: `{
  "name": "MySQL主库",
  "resource_type": "database",
  "connection_info": {
    "host": "localhost",
    "port": 3306,
    "database": "mydb",
    "username": "root"
  },
  "description": "生产环境MySQL数据库"
}`,
    response: `{
  "id": 1,
  "name": "MySQL主库",
  "resource_type": "database",
  "connection_info": {
    "host": "localhost",
    "port": 3306,
    "database": "mydb",
    "username": "root"
  },
  "description": "生产环境MySQL数据库",
  "created_by": 1,
  "is_active": true,
  "created_at": "2025-09-30T16:54:08.539068+08:00",
  "updated_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '获取资源列表',
    method: 'GET',
    path: '/api/resources',
    description: '获取资源列表，支持分页和类型过滤',
    auth: true,
    query: `page: 页码，默认 1
page_size: 每页数量，默认 10
resource_type: 资源类型（可选，如 database、compute_engine）`,
    response: `[
  {
    "id": 1,
    "name": "MySQL主库",
    "resource_type": "database",
    "connection_info": {...},
    "description": "生产环境MySQL数据库",
    "created_by": 1,
    "is_active": true,
    "created_at": "2025-09-30T16:54:08.539068+08:00"
  }
]`
  },
  {
    name: '获取指定资源',
    method: 'GET',
    path: '/api/resources/:id',
    description: '根据资源 ID 获取资源详细信息',
    auth: true,
    response: `{
  "id": 1,
  "name": "MySQL主库",
  "resource_type": "database",
  "connection_info": {...},
  "description": "生产环境MySQL数据库",
  "created_by": 1,
  "is_active": true,
  "created_at": "2025-09-30T16:54:08.539068+08:00"
}`
  },
  {
    name: '更新资源',
    method: 'PUT',
    path: '/api/resources/:id',
    description: '更新指定资源的配置',
    auth: true,
    request: `{
  "name": "MySQL主库-更新",
  "connection_info": {
    "host": "newhost",
    "port": 3306
  },
  "description": "更新后的描述",
  "is_active": true
}`,
    response: `{
  "id": 1,
  "name": "MySQL主库-更新",
  "resource_type": "database",
  "connection_info": {...},
  "description": "更新后的描述",
  "is_active": true
}`
  },
  {
    name: '删除资源',
    method: 'DELETE',
    path: '/api/resources/:id',
    description: '删除指定资源',
    auth: true,
    response: `{
  "message": "删除成功"
}`
  }
]
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.api-section {
  margin-bottom: 30px;
  padding-bottom: 20px;
  border-bottom: 1px solid #e8e8e8;
}

.api-section:last-child {
  border-bottom: none;
}

.code-block {
  margin-top: 15px;
  background: #f5f7fa;
  border-radius: 4px;
  overflow: hidden;
}

.code-title {
  background: #e8e8e8;
  padding: 8px 15px;
  font-weight: 600;
  font-size: 14px;
  color: #606266;
}

.code-block pre {
  margin: 0;
  padding: 15px;
  background: #282c34;
  color: #abb2bf;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.6;
  overflow-x: auto;
}

:deep(.el-descriptions__label) {
  width: 120px;
  font-weight: 600;
}

:deep(.el-alert p) {
  margin: 5px 0;
}
</style>