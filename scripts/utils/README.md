# Utils 脚本说明

本目录包含 ADDP 项目的通用工具脚本，用于批量操作、验证检查和开发辅助。

## 📋 脚本清单

### node-runtime.sh

读取仓库根 `.node-version` 并检查当前 PATH 中 `node --version` 的稳定主版本。开发启动与重启、锁定依赖安装、前端 Make 门禁、浏览器测试启动器、本地 CI 和镜像构建共用该实现；CI 先通过 `setup-node` 选择同一版本，再执行校验。版本错误、预发布版本、解释器缺失或版本文件非法均返回非零，不安装或切换工具，不提供跳过开关。

新机器安装与多版本机器的单次命令选择沿[工具链准备流程](../../docs/guide/addp部署和开发步骤.md#node-工具链准备与诊断)，Node/npm 路径与版本诊断使用 `make dev-doctor`。手动核对 Node 使用 `make check-node-runtime`，策略和副作用边界回归使用 `make test-node-runtime`，该回归只用临时目录与受控命令，不启停个人服务。实际测试与前端构建沿根 Make 标准入口执行；直接进入模块运行 npm 不能替代标准门禁。根 `.node-version` 声明的是主版本，并未锁定补丁版本。

### ports-validate.sh
**用途**: 展示 System／Business 指定端口信息并检查有限策略。输入来源、失败条件、查询缺失与未覆盖项统一见[端口规范：快速校验](../../docs/spec/addp端口分配.md#快速校验)；报告不等于端口占用扫描或运行就绪验收。

**标准入口**（从仓库根目录运行）：

```bash
make ports-validate
```

---

### register-business.sh
**用途**: 将本机已启动的 Business PostgreSQL、MySQL、MinIO、NFS 和可选业务引擎注册到 System 引擎管理

**注意**:
- 脚本用于本机开发拓扑：ADDP 模块作为宿主机进程访问 Business。PostgreSQL、MySQL 和 MinIO 从本工作区 Compose 容器读取实际宿主机端口；NFS 使用当前导出路径。其他已启动的可选引擎使用 `business/.env` 中的宿主机端口。
- 运行前必须先启动 Business PostgreSQL 和 MinIO；MySQL、NFS、Oracle 等未启动时跳过。Oracle 第一期只提供普通表 Catalog/查询/只读快照，不启用 CDC 或 ArcGIS SDE。
- 统一调用 System 创建接口；System 按 Tenant、引擎类型和物理端点幂等复用原 ID。端点变化创建新 Engine Instance，脚本不会按名称修改旧记录或自动搬迁其他模块绑定。新实例名称附带宿主机端口，便于与历史实例区分。
- 脚本不接收用户名、密码或环境变量 Token；运行时在隐藏的交互式提示中输入当前 User Access Token。

**命令**:
```bash
./scripts/utils/register-business.sh
```

---

### test-tile-api.sh
**用途**: 测试 Manager 模块的 MVT 瓦片生成和缓存

**功能**:
- 请求指定图层的 MVT 瓦片
- 验证瓦片格式正确性
- 测试缓存命中率
- 支持自定义表名、缩放级别

**命令**:
```bash
# 交互式运行（会提示输入参数）
./scripts/utils/test-tile-api.sh

# 指定参数（计划支持）
# export TOKEN="your-jwt-token"
# export TABLE="dltb"
# export ZOOM=10
# ./scripts/utils/test-tile-api.sh
```

**预期输出**:
```
[✓] 瓦片生成成功 (200 OK)
[✓] Content-Type: application/x-protobuf
[✓] 缓存到 MinIO 成功
[✓] 第二次请求命中缓存 (<10ms)
```

**注意**: 此脚本当前为交互式，需要手动输入 JWT token 和参数。

**使用场景**: MVT 瓦片功能测试、缓存性能验证

---

## 🔄 典型使用场景

### 场景 1: 部署前验证

```bash
# 1. 输出端口报告并检查限定策略
make ports-validate

# 2. 检查平台构建登记和前端 CI 登记
make test-platform
```

### 场景 2: 开发调试

```bash
# 测试 MVT 瓦片功能
./scripts/utils/test-tile-api.sh

# 查看指定容器端口报告（不代表运行就绪）
make ports-validate
```

### 场景 3: CI/CD 集成

```bash
# 平台登记门禁
make test-platform
# 端口诊断报告仅作辅助，不替代集成或运行验收
make ports-validate
```

---

## ⚠️ 注意事项

1. **ports-validate.sh**
   - 输入、退出判定和未覆盖项统一见[端口规范的覆盖边界](../../docs/spec/addp端口分配.md#快速校验)；零退出不代表全部容器可用或业务验收通过。

2. **test-tile-api.sh**
   - 需要 Manager backend 和基础设施正在运行
   - 需要有效的 JWT token
   - 测试表名需要在数据库中存在

## 🔗 相关文档

- [基础设施管理](../infra/README.md) - 启动 PostgreSQL、Redis、MinIO
- [开发环境](../dev/README.md) - 本地开发启动流程
- [Go 依赖整理](../dev/README.md#modtidysh)与[添加新依赖](../dev/README.md#添加新依赖) - 显式维护流程、脚本清单范围及 `go.mod`／`go.sum` 差异核对
- [构建脚本](../build/README.md) - 编译和镜像构建

---

## 📞 故障排查

### 问题 1: ports-validate.sh 报告策略不一致

`Policy mismatch detected` 按[快速校验的两类判定](../../docs/spec/addp端口分配.md#快速校验)定位；未出现该标记的非零退出需先查看原始读取或执行错误。容器未运行和查询失败也不能按零退出计为通过。

实际 Infra 映射用标准 `bash scripts/infra/status.sh` 核对；Business 固定端口冲突及引擎地址变更按[端口规范的使用建议](../../docs/spec/addp端口分配.md#使用建议)处理。

### 问题 2: test-tile-api.sh 无法运行

**原因**: Backend 服务未启动或 User Access Token 过期

**解决**:
```bash
# 1. 启动服务
./scripts/dev/start.sh

# 2. 在 Console 完成登录和 Context 选择，再运行脚本
# 3. 在隐藏提示中输入当前 User Access Token
./scripts/utils/test-tile-api.sh
```
