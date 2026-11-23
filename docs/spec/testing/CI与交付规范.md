# ADDP CI 与交付规范

[返回测试与验收总入口](../addp测试与验收规范.md)

## CI 编排与登记

### 发布、路径选择与调度

共享main续办遵循[统一发布入口](../../../AGENTS.md)的单写锁／快进规则；`make test-main-publication` 以隔离临时Git仓库／远端核显式续办、基线变化、命令失败及锁竞争，由 `make test-platform` 进入Platform CI。

Platform、Release/T2及Quality Smoke路径门禁各自独占Runner资源，不设workflow／job `concurrency` 分组：保留每轮提交范围与选择，由Runner配额排队，无关后续提交不得取消运行或替换pending。仅 `cancel-in-progress: false` 仍会替换同组pending，不能采用；专用T4继续用原Job资源锁。

Push按 `before...sha`，PR按base与本次SHA的merge-base差异，`changed-gate.py` 扩散共享依赖／Owner输入。文档未命中只跳过该轮，不替前序代码验收。`make test-platform` 选择／登记回归须核连续相关提交再追加无关文档、共享依赖、取消／失败／意外跳过；证明前序继续、排队实启、文档只跳自身未命中项。超时、清理、零残留及required check汇总不变。

### 独立作业与构建登记

下列独立Job不依赖聚合Job；入口／环境／摘要须由登记检查证明实际调用。独立成功仅证明本项，聚合在其他Owner提前失败仍是失败，不能推定本项已执行。路径门禁分别核 `selected`、标准命令和真实终态；文档跳过不补前序证据。

| Platform CI Job／入口 | 环境、预算与选择 |
| --- | --- |
| `System Go CI runner`／`make test-module MODULE=system GO_ONLY=1`；`Transfer Go CI runner`／`make test-module MODULE=transfer GO_ONLY=1` | 各Ubuntu24.04／20分钟，按Owner、Common及标准入口依赖选择；Go取各backend/go.mod，Node取根 `.node-version`（根Make仍须校验），输入登记不绕过。不启动DB／个人服务／T4，不算完整模块；Go workspace聚合仍验证组合依赖。 |
| `Common frontend contracts`／`make test-common-frontend` | Ubuntu24.04／10分钟，根Node版本；所有Platform事件无条件执行。 |
| `Frontend CI registration`／`make test-frontend-ci-registration` | Ubuntu24.04／10分钟，Runner Python3；所有Platform事件无路径或矩阵跳过。 |
| `Execution online CI runner`／`make test-execution-online-ci-runner` | 独立确定性Graph/Cleanup编排、准入／报告／恢复／登记T0/T1，不启动产品／DB／T4；输入闭包、push/PR及schedule/manual选择沿统一检查，跳过须 `selected:false`。 |

Platform consistency 检出提交并检查空白错误后，立即执行 `make test-platform-preflight`，核验 main 发布事务、构建登记和变更影响选择。这部分只需要系统自带的 Git、Bash、Make 和 Python。前置检查通过后，才准备 Go、Node、Swagger 和前端依赖，再运行 `make test-platform-checks`。本地完整入口 `make test-platform` 依次调用同样的两个目标，每阶段只执行一次。前置检查失败时，完整检查未执行，作业摘要的失败阶段为 `preflight`。

共享前端两Job复用统一摘要，不装共享库node_modules或启浏览器／产品／DB；`make test-platform` 继续聚合同入口，独立性及无条件调用由前端登记回归约束。System Cleanup PG实际消费Hosted唯一观察器，须经 `ADDP_T2_INPUT_FILES` 登记观察器／支持／机器契约；改名同步真消费者，无旧别名。

共享门禁摘要在 Job 日志写入一行以 `ADDP_CI_GATE_RESULT ` 开头的 JSON。记录 workflow、触发事件、workflow 提交 SHA、run、attempt、Job 与 gate 名称；`selected` 为选择结果，`job_status` 为摘要步骤观察到的 Job 状态，`gate_outcome` 为调用方接入的实际门禁步骤结果，`failure_stage` 为已识别的失败阶段。Platform consistency 和通用前端矩阵接入实际步骤和失败阶段。未知选择、未接入的步骤结果或未知失败阶段用 JSON `null`。最终 Job 状态、各步骤时间和摘要未执行的取消从 Actions API 读取。固定产品源码的 T4 继续以场景报告中的产品 SHA 为准。

构建／安全门禁：

- `make build-images` 唯一调度，最多两任务、基础镜像预热先行，单／双路同实现；前端仅锁文件安装、Node取根版本，版本变化须选全部前端镜像。Platform核并发上限／失败汇总／日志隔离／中断进程与目录清理，既有产品Job实际构建。
- `make test-workflow-security` 纳入本地test-platform及System IAM required Job：固定zizmor1.28.0，只扫 `.github/workflows/release-and-t2-gates.yml`，auditor／medium、禁在线审计／自定义配置／忽略。首次下载固定wheel，临时venv退出删除；安装／收集／扫描失败阻断，不依赖ADDP或DB。
- Manager Hosted Infra MinIO缓存不替代构建：原Dockerfile／Infra Compose、Server/mc修订与断言不变，缓存含编译层inline及最终镜像。键绑定Linux amd64、脚本和全部已跟踪Infra上下文，仅精确命中；先核归档摘要，再核镜像身份架构，热构建复用RUN且双二进制摘要相同，损坏／缺必需热验／构建失败阻断。独立cache workflow在全新Runner实测，Compose仍实际构建；部署前删转移归档，只存公开固定源码构建层，不存身份／配置／数据／结果，不代业务T4。

### 作业预算与聚合复用

<a id="frontend-deterministic-job-budget"></a>

`frontend-deterministic` 默认20分钟，例外沿当前workflow矩阵：

| 门禁 | 总预算／并行边界 |
| --- | --- |
| Monitor `make test-monitor-frontend` | 40分钟，单Worker。 |
| Console `make test-console-frontend` | 30分钟，两Worker按文件并行，`fullyParallel: false` 保文件内顺序。 |
| GeoPython `make test-geopython-workflow` | 60分钟，四隔离pytest Worker／worksteal，见下述约束。 |
| `Platform consistency`／`make test-platform` | 60分钟，含冷Swagger准备、完整门禁与退出清理。 |

预算均含准备／全测试／构建／清理。Context／HTTP夹具隔离，Vite只服务源码，端口／缓存／证据由标准入口拥有；不得删例、放宽单例期限或增重试。Monitor20／30分钟取消、Console三次20分钟超时（`40254270886`、`40254922673`、`40255039253`）仍是历史失败，不因改预算改判；Linux完整Job、局部分项和文档跳过分别认定，独立专项成功也不代原聚合／review失败。

Online聚合只沿Make依赖图串行复用分发／选择／栅格血缘观察／登记，同进程共享目标执行一次、独立Owner仍完整可达；禁跨运行结果缓存、环境分支跳过或重复实现。登记负例先验有效基线再移除普通依赖边，`.PHONY`／`.NOTPARALLEL` 不是执行依赖；原JavaScript语法预检仍为首个串行依赖，保持失败阶段。独立Job／聚合各按原期限与重试语义。

GeoPython使用现有公开 `ubuntu-24.04` 四CPU Runner（[规格](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)），不升规格或预算。完整pytest收集集合与串行相同、每例执行一次，NetCDF前置子集仍独立；pytest-xdist只为测试依赖，Worker独立临时目录／动态端口，无共享可变文件或产品服务。`--max-worker-restart=0` 禁崩溃重启，失败／不完整均阻断，不放宽资源／像元／耗时／清理；产品Gunicorn仍单Worker，本地优化与Linux完整Job证据分开。

### Workflow 的职责与变更闭包

Workflow仅管触发／Owner选择、Runner／工具链／disposable Service、唯一Make/script调用、超时并发／Artifact／摘要／required check。禁止复制SQL、业务夹具、测试选择表达式、模块启停或清理；可自动发现的Git／依赖事实不另维护手写清单，显式登记由 `make test-platform` 核完整性。

模块、门禁、依赖、构建或suite变化须同次闭合：**Owner断言与安全→根Make→发现／影响选择或suite登记→workflow／报告→规范／模块或运行文档**。永久排队、接开发环境或始终Skip的占位workflow不能交付。

- T2：凡 `ADDP_T2_SERVICES` 声明（包括后续新增数据库），须同时根Make、`test-integration` 串行聚合、共享影响选择与带摘要Job；实际Service存在、版本tag及sha256固定，不按数据库种类分支登记。
- T5：声明 `workflow_job` 时，统一检查只经 `test-release` 分发、仓外证据及共享摘要，不按产品类型增加检查路线。
- NFS：`make test-common-nfs-unit` 为确定性，`make test-common-nfs-publication` 为独占Hosted Linux amd64原生export协议T2。`ADDP_T2_HOSTED_ONLY`／`ADDP_T2_NATIVE_LIFECYCLE` 必须声明，后者是 `ADDP_T2_INPUT_FILES` 中真生命周期源码；不冒充Docker、不进本地集成。Release/T2按Common选择实跑／摘要／原证据且拒Skip；准备未知保留业务残留报告，夹具最终销毁另记，不代授权或T4。

### 辅助 macOS 定时巡检

`make local-ci` 仅在日常macOS的独立巡检checkout复用已有T0–T3，辅助GitHub反馈，不替required checks、不跑T4/T5。依赖准备是编排，断言仍归根Make／Owner。

| 边界 | 必需行为 |
| --- | --- |
| 源码／基线 | 干净main，无tracked／untracked改动；只fetch／fast-forward，不reset/clean。首轮完整确定性＋已登记集成，之后以上次成功SHA为 `BASE_REF` 跑 `make test-changed`；每新SHA另 `make build BUILD_ARGS=--force` 验全部Linux二进制。失败不推进基线，下轮重试同提交。 |
| 范围／同步 | 默认同步origin/main＋增量；`--full` 全量、`--no-fetch` 不fetch/FF，可组合 `make local-ci LOCAL_CI_ARGS="--no-fetch --full"`；`--check-only` 独立且禁组合。日志与 `latest-summary.txt` 分列 `scope`／`remote_sync`，不将“不拉取”记作增量。 |
| 项目／PG | 只启停持久 `addp-infra`，不删其卷／全局prune／操作他项目；PG仅 `addp_test`／`addp_iam_test`、集成串行。巡检MySQL／OceanBase唯一项目 `addp-local-macos-ci`，成败中断均删除两服务。 |
| MySQL | 固定MySQL8、无卷、`127.0.0.1:13306`，确定性／编译前健康；外层只传本地作用域标记，连接只在 `test-common-mysql-data-protection` 内生成，不入根.env／传其他Owner或接Business／生产。 |
| OceanBase | 固定digest的CE4.4.2 LTS、无卷、`127.0.0.1:12881`，同样预先健康；先清继承 `ADDP_TEST_OCEANBASE_*`，不读根.env。`common-oceanbase-gate.sh` 仅按本地标记生 `addp_oceanbase_disposable` 参数，不接Business／生产。 |

日志／成功SHA／锁存Git内部状态目录，不入工作树或泄漏Token／可复用凭据。

## 交付与完成标准

实施前识别受影响T0–T5及入口／CI闭包。交付前运行最小充分标准门禁：

```bash
make test-changed
```

指定Owner用 `make test-module MODULE=<module>`，已提交区间用 `make test-changed BASE_REF=<ref>`。环境受限必须列未验证项／原因／负责的已有CI，不能把未跑或Skip记通过。

完成须同时满足：唯一标准入口且旧变量／旁路删除；Owner／层级／数据身份边界明确；本地／CI／登记／文档同步；真实执行、不借旧进程或错误环境；产物只在系统临时目录／CI Artifact，仓库无残留。
