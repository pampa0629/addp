# ADDP CI 与交付规范

[返回测试与验收总入口](../addp测试与验收规范.md)

## CI 编排与登记

按路径选择门禁的托管工作流（Platform CI、Release/T2、Quality Smoke）使用独立 Runner 和 Job 独占的 disposable 资源，不设置 workflow 或 job 的 `concurrency` 分组。每次运行保留自己的提交范围与选择结果，由 GitHub Runner 配额排队；后续无关提交不能取消正在执行的门禁，也不能替换待执行门禁。仅将 `cancel-in-progress` 改为 `false` 不足以满足此要求：GitHub 默认仍以新运行替换同组已有的 pending 运行。专用 T4 主机的资源互斥遵循其既有 Job 锁，不套用托管门禁的调度规则。

Push 使用事件的 `before...sha`，PR 使用 base 与本次 SHA 的 merge-base 差异；共享依赖和 owner 声明的测试输入继续由 `changed-gate.py` 统一扩散。文档运行跳过测试只表示该提交未命中路径，不能替代前序代码提交的验收证据。`make test-platform` 的 CI 登记与选择回归约束上述调度，并验证连续相关提交后追加文档、共享依赖、取消/失败/意外跳过的判定；原有超时、owner 清理、零残留核验及 required check 汇总必须保留。

验证调度修复时，应在相关门禁执行和排队期间追加无关文档提交，再核对前序 Job 继续完成、排队 Job 实际启动，以及文档运行只跳过自身未命中的门禁。

产品镜像构建继续由 `make build-images` 唯一入口负责。构建调度最多同时执行两个任务，基础镜像预热先于任务启动；单路和两路使用同一实现。前端 Node 版本来自根 `.node-version`，仅使用锁文件安装依赖。`make test-platform` 校验这些契约，并验证并发上限、失败汇总、日志隔离及中断时的进程和临时目录清理。现有 Platform CI 产品构建 Job 复用该入口验证实际镜像；Node 版本变更必须通过影响选择命中全部前端镜像，不缩减受影响服务范围。

发布工作流安全审计统一由 `make test-workflow-security` 执行，并纳入本地 `test-platform` 与 CI 现有 System IAM required Job。唯一脚本固定 zizmor 1.28.0，只扫描 `.github/workflows/release-and-t2-gates.yml`，使用 auditor、medium 最低严重级别、关闭在线审计，不读取自定义配置或忽略标记；安装、扫描或输入收集失败必须阻断。首次运行需联网取得固定版本的二进制 wheel，使用临时虚拟环境且退出时清理；不依赖 ADDP 服务或数据库。

Manager Hosted 的 Infra MinIO 构建准备复用固定输入缓存：继续使用原 Dockerfile 与 Infra Compose 构建最终镜像，缓存同时保存带 BuildKit inline 元数据的编译阶段和最终镜像，不改变 Server / mc 源码修订或验收断言。缓存键包含 Linux amd64、构建脚本及全部已跟踪 Infra 构建上下文；只恢复精确键，加载前核对归档摘要，加载后核对镜像身份和架构。热构建必须复用 Dockerfile RUN，并保持两个二进制摘要一致；缓存损坏、缺失的必需热验或构建失败均阻断。独立 Infra MinIO build cache 工作流在全新 Runner 核验复用，原样 Compose 构建仍实际执行。转移归档在产品部署前删除以释放磁盘，仅保存公开固定源码构建层，不保存身份、配置、业务数据或测试结果；构建缓存通过不代表业务 T4 通过。

Workflow 只负责：

- 触发条件与 owner 影响选择。
- Runner、语言版本和 disposable Service 准备。
- 调用唯一 Make / script 入口。
- 超时、并发、Artifact、Step Summary 和 required check 名称。

不得在 workflow 中复制 SQL、业务夹具、测试选择表达式、模块启动逻辑或清理逻辑。能通过 Git 和依赖声明自动发现的事实不维护手写清单；必须手工登记的门禁由 `make test-platform` 的一致性检查验证完整性。凡通过 `ADDP_T2_SERVICES` 声明 PostgreSQL、MongoDB、MySQL、OceanBase、TiDB 或后续数据库 Service 的 T2 门禁，都必须同时登记根 Make 入口、`test-integration` 串行聚合、共享模块变更选择和带摘要的 CI Job；每个声明的 Service 必须存在，并使用显式版本 tag 与镜像 sha256 digest。T5 suite 如声明 `workflow_job`，登记检查必须通用验证该 Job 只经 `test-release` 分发、配置仓库外证据目录并挂接统一摘要，不按产品或数据库类型增加检查分支。

新增或修改模块、测试入口、基础设施依赖、构建方式或 suite 时，必须在同一次变更中同步：

1. owner 测试与安全检查。
2. 根 `Makefile` 标准入口。
3. 自动发现、影响选择或显式 suite 登记。
4. `.github/workflows/` 编排与报告。
5. 对应规范、模块说明或运行指南。

不得提交一个永久排队、连接开发环境或始终 Skip 的占位 workflow。

### 辅助 macOS 定时巡检

日常使用的 macOS 可以在独立 checkout 中定时运行 `make local-ci`，作为 GitHub Actions 之外的辅助反馈机制。该入口只复用已有 T0-T3 标准门禁，不产生第二套测试事实，也不替代 GitHub required checks。

- checkout 必须专用于自动巡检，处于 `main`，且不得包含已跟踪或未跟踪改动；脚本只使用 `git fetch` 与 fast-forward，不使用 reset 或 clean 覆盖现场。
- 首次运行执行全部确定性门禁和已登记基础设施集成门禁；之后以上次成功 SHA 为 `BASE_REF` 运行 `make test-changed`。每个新 SHA 同时执行根 `make build BUILD_ARGS=--force`，复验全部 Linux 产品二进制。失败不推进成功基线，后续调度必须重试同一提交。
- 测试范围与远端同步是两个正交选择：默认执行“同步 `origin/main` + 增量”，`--full` 强制全量，`--no-fetch` 跳过 `fetch` 与 fast-forward；专用机需要在当前干净 `main` 上执行不拉取的完整巡检时，统一使用 `make local-ci LOCAL_CI_ARGS="--no-fetch --full"`。`--check-only` 是独立的就绪检查，不与其他选项组合。运行日志和 `latest-summary.txt` 必须分别记录实际 `scope` 与 `remote_sync`，不能把“不拉取”误报成“增量”。
- Python 与前端依赖准备只是环境编排；真实断言仍由根 `Makefile` 与 owner 门禁拥有。
- 本地 PostgreSQL 继续只使用 `addp_test` 与 `addp_iam_test`，集成门禁严格串行。持久基础设施只启停 `addp-infra` Compose 项目；巡检专属 MySQL 和 OceanBase 由唯一的 `addp-local-macos-ci` Compose 项目管理，不操作其他 Compose 项目。脚本不执行 Docker 全局 prune，也不删除 `addp-infra` 数据卷。
- MySQL owner 门禁使用本地巡检专属、固定版本且无数据卷的 MySQL 8 服务。该服务只绑定 `127.0.0.1:13306`，必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，真实连接参数只在 `test-common-mysql-data-protection` 进程内生成，不能写入仓库根 `.env`、传给其他 owner 门禁或连接 Business/生产 MySQL。
- OceanBase owner 门禁使用本地巡检专属、固定 digest 且无数据卷的 OceanBase CE 4.4.2 LTS 服务。该服务只绑定 `127.0.0.1:12881`，并且必须在确定性门禁和编译前通过健康检查；外层聚合门禁只携带本地作用域标记，`common-oceanbase-gate.sh` 在该标记下固定生成 `addp_oceanbase_disposable` 连接参数。巡检启动前必须清除继承的 `ADDP_TEST_OCEANBASE_*`，不读取根 `.env`，不连接 Business/生产 OceanBase。
- 两个巡检专属数据库服务都必须在巡检成功、失败和中断的统一退出清理中删除。
- 辅助巡检不运行 T4 Online 或 T5 发布认证；两者继续遵循各自的专用环境、身份与报告协议。
- 日志、上次成功 SHA 与运行锁位于 checkout 的 Git 内部状态目录，不进入工作树；日志不得包含 Token 或可复用凭据。

## 交付与完成标准

实施前识别受影响的 T0-T5 层级。交付前优先运行：

```bash
make test-changed
```

验证指定 owner 或已提交区间时分别使用 `make test-module MODULE=<module>`、`make test-changed BASE_REF=<ref>`。受环境限制无法运行时，必须列出未验证项、原因以及负责验证的现有 CI 门禁。

测试体系变更完成必须满足：

1. 新逻辑只有一条标准入口，旧入口、变量和旁路已删除。
2. owner、测试层级、数据与身份边界明确。
3. 本地标准入口、CI 编排、登记检查和文档同步。
4. 最小充分门禁真实执行，未以 Skip、旧进程或错误环境冒充通过。
5. 运行产物位于操作系统临时目录或 CI artifact，仓库无残留。
