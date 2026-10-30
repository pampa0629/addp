# Utils 脚本说明

本目录包含 ADDP 项目的通用工具脚本，用于批量操作、验证检查和开发辅助。

## 📋 脚本清单

### Go 依赖辅助工具

版本目标只维护在[技术栈规约](../../docs/spec/addp技术栈规约.md#go-依赖版本规范)。规约检查从仓库根目录使用现有标准入口：

```bash
make test-go-dependency-policy
```

该入口先运行检查器的隔离临时夹具回归，再核对当前仓库的声明，不执行依赖同步、下载或升级。各工具的实际分工如下：

| 工具 | 读取与操作范围 | 结果与限制 |
| --- | --- | --- |
| [check-deps-version.sh](check-deps-version.sh) | 从规约提取唯一目标版本；用 Git 枚举已跟踪及未忽略的未跟踪 `go.mod`，比较已有 `require` 声明，包括单行、块形式及 indirect 声明。 | 不改写依赖文件；目标冲突、输入缺失／读取失败或已有声明不符时非零退出。未声明的依赖会跳过，不校验规约外依赖、实际构建选择或运行兼容性。 |
| [check-deps.sh](check-deps.sh) | 先在当前 workspace 执行 `go work sync`，可能改写模块依赖文件；之后只汇总脚本 `MODULES` 固定清单的声明差异。同步范围与报告范围不同。 | 版本差异只打印报告，不显式置为失败；本地替换占位版本还有过滤规则。它不按技术栈规约判定目标版本，零退出不代表规约合规，也不保证所有模块都被报告。 |
| [sync-deps.sh](sync-deps.sh) | 按脚本固定清单执行条件 `go get` 和 `go mod tidy`，可能联网并改写各模块依赖文件。 | 仍使用硬编码 MySQL 驱动版本及未锁定的 GORM `latest`，不读取规约，不能作为按规约统一升级的入口；`set -e` 中途退出不会回滚已改写的模块。 |
| [detect-common.sh](detect-common.sh) | 被 source 后提供时间戳比较函数；只比较 `common` 的 Go 文件与清单内已有 `.dev-bins` 二进制的修改时间，不执行编译。 | 二进制缺失、源码缺失或时间戳无法取得时可能不列入提示；不覆盖完整构建输入，不能据此判断产物最新。当前标准构建使用[内容指纹与原子发布](../../docs/spec/addp开发服务生命周期与构建身份规范.md#二原子构建)。 |

显式依赖变更和 `modtidy.sh` 的清单、停服顺序及差异核对统一见[开发依赖维护](../dev/README.md#modtidysh)与[添加新依赖](../dev/README.md#添加新依赖)。上表说明源码现状，不为辅助脚本提供第二套升级或构建路线；规约检查通过也不能替代模块测试、集成或运行验收。

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

**用途**：交互式诊断 Manager Quick View 的一次 MVT 请求，输出 HTTP 状态、文件大小、`file` 识别结果、部分十六进制字节及 gzip 魔数检查。脚本实现见 [test-tile-api.sh](test-tile-api.sh)。

从仓库根目录运行：

```bash
bash scripts/utils/test-tile-api.sh
```

输入与请求范围：

- 在隐藏提示中输入当前 opaque User Access Token，先请求 System `GET /api/v1/system/auth/context`，再以同一 Bearer 请求 Manager `GET /api/v1/manager/quick-view/tiles/:z/:x/:y.mvt`；身份规则见[认证规范](../../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)。AuthContext 预检成功不代表已取得 Manager 的 Tenant Context、`manager.derived_artifact.read` 或实际源读取权限。
- 交互输入 ResourceLocator、`z/x/y` 和几何字段；内置 `engine/2`、`public/dltb`、`7/102/72` 只是示例，须按当前租户的真实资源和覆盖范围选择。几何字段留空仍使用脚本缺省 `smgeometry`，并非自动发现；字段应以实际结构为准。脚本会对 `locator`、`geometry_column` 做 URL 编码。
- 当前地址固定为 System `http://localhost:8180`、Manager `http://localhost:8081`，不读取 `.dev-state/ports.env`，也未提供环境变量或命令行参数覆盖入口。先按[端口规范](../../docs/spec/addp端口分配.md#端口分配)核对实际端口；动态端口环境应沿实际 Manager 入口诊断，不能把固定地址失败直接判为瓦片故障。该链路不经过 Gateway 或公开 HTTPS 反向代理。

结果判定与现行契约见 [MVT 瓦片数据](../../manager/docs/quick-view/矢量快显与物化视图.md#mvt-瓦片数据)：脚本不读取响应头，不解码 MVT，也不重复请求或测量耗时，不能证明 Content-Type、要素内容、MinIO 写入、缓存命中率或性能。HTTP 200 和空 body 仍须结合实际响应头区分正常、空瓦片、超时和降级；脚本中的 gzip 成功提示只依据魔数与展示片段，不构成完整格式验证。部分非 200 分支仅打印响应，未显式非零退出，不能以脚本退出码单独判定通过。

响应体写入固定 `/tmp/tile-output.mvt`，再次执行会覆盖；下载、错误响应和截断展示不能替代完整结果验收。虽然输入隐藏，脚本末尾仍会回显 Token 和含该凭据的命令，终端输出不得直接分享或归档为验收证据。正式跨模块验收沿[Manager 场景规范](../../docs/spec/testing/scenarios/Manager与空间成果.md)，按固定源码的实际结果分别认定。

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
# 交互式诊断一次 MVT 请求（范围与令牌输出见上文）
bash scripts/utils/test-tile-api.sh

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
   - 固定地址、当前用户权限、资源参数、令牌回显及结果判定统一见[工具说明](#test-tile-apish)；单次下载不作为缓存或性能验收。

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

按[工具说明](#test-tile-apish)先区分固定地址不可达、Token 失效、Tenant Context／权限不符与资源参数错误，再核对原始 HTTP 状态和响应体；脚本未单列的 403 等结果会进入通用失败分支。已有服务的状态和归属按[开发排障](../docs/启动脚本使用指南.md#排查失败)核对，需要启停时使用该指南的标准入口。MVT 空响应、超时及降级按[正式响应契约](../../manager/docs/quick-view/矢量快显与物化视图.md#mvt-瓦片数据)排查，不能仅以“下载成功”或脚本零退出排除问题。
