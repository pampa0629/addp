# Jupyter 租户隔离 - 开发环境实施方案

## 概述

在开发环境下，通过**共享基础虚拟环境 + 租户独立扩展虚拟环境**的方案实现租户隔离，无需使用 Docker。

## 核心架构

```
宿主机直接运行
├─ 全局基础虚拟环境 (共享,只读)
│   └─ engines/jupyter/venv/
│       └─ 预装基础库 (pandas, geopandas, psycopg2, ...)
│
├─ 单一 Jupyter Lab Server (端口 8088)
│   └─ 使用 MinIOContentsManager 按 tenant_id 隔离文件
│
└─ 租户独立虚拟环境 (持久化,读写)
    └─ engines/jupyter/tenants/tenant_{id}/
        ├─ venv/ (继承基础环境 + 租户自己安装的库)
        └─ .jupyter/kernels/tenant_{id}/
            └─ kernel.json (环境变量: TENANT_ID)
```

## 已完成的后端工作

### 1. 初始化脚本
**文件**: `engines/jupyter/init_tenant_venv.sh`

**功能**:
- 为租户创建虚拟环境 (使用 `--system-site-packages` 继承基础库)
- 注册 Jupyter Kernel (kernel 名称: `tenant_{id}`)
- 设置环境变量 `TENANT_ID` 和 `ADDP_API_BASE`
- 部署 IPython startup 脚本到租户虚拟环境
- 创建软链接到 Jupyter 全局 kernels 目录

**测试**:
```bash
cd /Users/pampa/code/addp/engines/jupyter
./init_tenant_venv.sh 1
```

### 2. IPython Startup 脚本
**文件**: `engines/jupyter/ipython_startup_00_addp_datasources.py`

**改动**:
- 从环境变量读取 `TENANT_ID` (兼容 `ADDP_TENANT_ID`)
- 默认 API 地址改为 `http://localhost:8000`

**功能**:
- Kernel 启动时自动调用 `/api/system/engines?tenant_id=X`
- 注入 `ds_8`, `ds_9`, ... 到全局命名空间
- 美化输出显示可用数据源

### 3. Go 后端服务
**文件**: `develop/backend/internal/service/jupyter_venv_service.go`

**功能**:
- `InitTenantVenv(tenantID)` - 初始化租户虚拟环境
- `GetTenantVenvInfo(tenantID)` - 获取虚拟环境状态
- `DeleteTenantVenv(tenantID)` - 删除虚拟环境
- `ListTenantVenvs()` - 列出所有虚拟环境 (管理员)

### 4. API 端点
**文件**: `develop/backend/internal/api/jupyter_venv_handler.go`

**端点**:
- `GET /api/develop/jupyter/venv/status` - 获取租户虚拟环境状态
- `POST /api/develop/jupyter/venv/init` - 初始化租户虚拟环境
- `DELETE /api/develop/jupyter/venv` - 删除租户虚拟环境
- `GET /api/develop/jupyter/venvs` - 列出所有虚拟环境 (管理员)
- `GET /api/develop/jupyter/server/status` - Jupyter Server 状态

### 5. 路由集成
**文件**: `develop/backend/internal/api/router.go`, `develop/backend/cmd/server/main.go`

**状态**: ✅ 已完成，编译成功

---

## 待完成的前端工作

### 1. API 调用封装
**文件**: `develop/frontend/src/api/jupyter.js`

**状态**: ✅ 已创建

### 2. NotebookEditor.vue 改造

**需要添加的功能**:

#### (1) 状态检查
组件加载时检查租户虚拟环境是否存在:
```javascript
const checkVenvStatus = async () => {
  const { data } = await getVenvStatus()
  if (!data.exists) {
    // 显示初始化提示
  }
}
```

#### (2) 初始化界面
在 Jupyter iframe 上方显示初始化状态:
```vue
<div v-if="!venvReady" class="venv-init-card">
  <el-alert type="warning" :closable="false">
    <template #title>
      首次使用需要初始化开发环境 (约 30 秒)
    </template>
  </el-alert>
  <el-button @click="initVenv" :loading="initLoading">
    立即初始化
  </el-button>
</div>

<iframe v-else :src="jupyterUrl" ... />
```

#### (3) Jupyter URL 改造
添加 `kernel` 参数，自动选择租户的 Kernel:
```javascript
const jupyterUrl = computed(() => {
  if (!currentNotebook.value || !venvInfo.value) return ''

  const minioPath = currentNotebook.value.content?.minio_path
  const kernelName = venvInfo.value.kernel_name // tenant_1

  return `${jupyterBaseUrl.value}/tree/${minioPath}?kernel=${kernelName}`
})
```

---

## 用户体验流程

### 首次访问
```
用户点击 "Notebook" 菜单
    ↓
前端检测虚拟环境不存在
    ↓
显示提示卡片: "首次使用需要初始化环境 (约 30 秒)"
    ↓
用户点击 "立即初始化"
    ↓
调用 POST /api/develop/jupyter/venv/init
    ↓
显示加载动画 + 进度提示
    ↓
初始化完成 → 自动打开 Jupyter Lab
    ↓
Kernel 启动 → 自动注入数据源 (ds_8, ds_9, ...)
    ↓
用户直接使用: df = pd.read_sql("SELECT * FROM ...", ds_8)
```

### 后续访问
```
用户点击 "Notebook" 菜单
    ↓
前端检测虚拟环境已存在
    ↓
直接打开 Jupyter Lab (< 1 秒)
    ↓
Kernel 启动 → 自动注入数据源
    ↓
用户直接使用
```

### ADDP 重启后
```
用户点击 "Notebook" 菜单
    ↓
Develop 后端调用 init_tenant_venv.sh
    ↓
脚本检测到虚拟环境已存在 → 跳过初始化
    ↓
Jupyter Server 启动 (startup.sh)
    ↓
自动扫描并注册所有租户的 Kernel
    ↓
用户访问 → 虚拟环境、已安装的库全部保留 ✅
```

---

## 关键优势

### 1. 共享基础库
- ✅ 所有租户共享 `engines/jupyter/venv/` 的基础库
- ✅ 节省磁盘空间 (10 个租户约 700MB vs Docker 40GB)
- ✅ 节省安装时间 (无需每个租户重复安装基础库)

### 2. 租户独立扩展
- ✅ 每个租户可以自己安装库 (`pip install xxx`)
- ✅ 租户之间完全隔离 (不同的虚拟环境)
- ✅ 租户 A 安装的库，租户 B 看不到

### 3. 持久化
- ✅ `engines/jupyter/tenants/` 目录持久化保存
- ✅ ADDP 重启后，虚拟环境和已安装的库全部保留
- ✅ 无需重新初始化

### 4. 引擎自动注入
- ✅ Kernel 启动时自动调用 System API 获取数据源
- ✅ 用户无需手动配置，开箱即用
- ✅ 安全：密码不暴露在 Notebook 中

---

## 资源占用

| 项目 | 磁盘占用 | 内存占用 |
|------|---------|---------|
| 全局基础环境 | 500MB | - |
| 租户初始环境 | 20MB/租户 | - |
| Jupyter Server | - | 500MB |
| 每个活跃 Kernel | - | 50-100MB |
| **10 个租户总计** | **700MB** | **500MB + 活跃 Kernel×100MB** |

对比 Docker 方案: 40GB 磁盘 + 10×4GB = 40GB 内存

---

## 测试步骤

### 1. 启动开发环境
```bash
cd /Users/pampa/code/addp
bash scripts/infra/up.sh
bash scripts/dev/start.sh
```

### 2. 重启 Develop 模块
```bash
bash scripts/dev/restart.sh -develop
```

### 3. 测试初始化脚本
```bash
cd engines/jupyter
./init_tenant_venv.sh 1
```

验证:
- [ ] 虚拟环境创建成功: `engines/jupyter/tenants/tenant_1/venv/`
- [ ] Kernel 注册成功: `~/.local/share/jupyter/kernels/tenant_1/`
- [ ] 可以列出继承的基础库: `engines/jupyter/tenants/tenant_1/venv/bin/pip list`

### 4. 测试后端 API
```bash
# 获取状态
curl -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/develop/jupyter/venv/status

# 初始化虚拟环境
curl -X POST -H "Authorization: Bearer <token>" \
  http://localhost:8185/api/develop/jupyter/venv/init
```

### 5. 测试前端界面
1. 登录 Portal: http://localhost:5170
2. 进入 Develop 模块 → Notebook
3. 首次访问应显示初始化提示
4. 点击"初始化" → 等待 30 秒
5. 初始化完成后，Jupyter Lab 自动打开
6. 创建新 Notebook → Kernel 自动选择 `tenant_1`
7. 验证数据源自动注入: 第一个 Cell 应显示数据源列表

### 6. 测试数据源注入
在 Jupyter Lab 中创建新 Notebook，运行:
```python
# 应该自动显示可用数据源
# ds_8, ds_9, ... 应该已经注入

# 测试使用数据源
import pandas as pd
df = pd.read_sql("SELECT 1 as test", ds_8['connection_string'])
print(df)
```

### 7. 测试租户隔离
```bash
# 初始化租户 2
./engines/jupyter/init_tenant_venv.sh 2

# 租户 1 安装库
engines/jupyter/tenants/tenant_1/venv/bin/pip install scikit-learn

# 验证租户 2 看不到
engines/jupyter/tenants/tenant_2/venv/bin/pip list | grep scikit-learn
# 应该为空
```

### 8. 测试持久化
```bash
# 重启 ADDP
bash scripts/dev/restart.sh -develop

# 验证虚拟环境仍然存在
ls engines/jupyter/tenants/tenant_1/venv/

# 验证 Kernel 仍然可用
jupyter kernelspec list | grep tenant_1
```

---

## 下一步

1. **前端改造** - 修改 `NotebookEditor.vue` 添加虚拟环境初始化界面
2. **端到端测试** - 按照上述测试步骤验证完整流程
3. **文档更新** - 更新 `develop/CLAUDE.md` 和 `docs/addp-Notebook开发.md`

---

## 相关文件

### 后端
- `engines/jupyter/init_tenant_venv.sh` - 初始化脚本
- `engines/jupyter/ipython_startup_00_addp_datasources.py` - Startup 脚本
- `develop/backend/internal/service/jupyter_venv_service.go` - 服务层
- `develop/backend/internal/api/jupyter_venv_handler.go` - API Handler
- `develop/backend/internal/api/router.go` - 路由
- `develop/backend/cmd/server/main.go` - 主程序

### 前端
- `develop/frontend/src/api/jupyter.js` - API 调用
- `develop/frontend/src/views/NotebookEditor.vue` - 界面 (待改造)

### 数据
- `engines/jupyter/tenants/` - 租户虚拟环境数据 (持久化)
