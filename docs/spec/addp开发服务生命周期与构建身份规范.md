# ADDP 开发服务生命周期与构建身份规范

本文定义本地开发环境中服务启动、停止、重启、依赖安装、编译和运行版本识别的统一规则。适用于 `scripts/dev/start.sh`、`restart.sh`、`stop.sh`、`keepalive.sh` 以及由这些脚本启动的 Go 服务、Python Runtime 和 Node 单元。

## 一、生命周期互斥

一次生命周期操作从读取 PID、停止进程、清理构建产物、编译、启动进程到写入新 PID 的全过程必须作为单个工作区级临界区执行。同一仓库工作区在任意时刻只允许一个 `start`、`restart`、`stop` 或 `keepalive` 清理操作改变服务状态。

互斥锁固定保存在 `.dev-state/lifecycle.lock`。锁元数据至少包含操作名称、参数、持有进程 PID、开始时间和工作区路径。无法获取锁时必须立即失败，并展示当前持有者信息；不得等待、抢占或继续执行部分清理。

`restart` 内部调用 `stop` 和 `start` 时必须继承同一个锁所有者，不能释放后重新竞争。继承只允许由锁元数据记录的持有进程或其后代进程使用，不能通过普通环境变量绕过锁。

普通 `start`、`restart` 和 `stop` 只在生命周期变更过程中持锁，服务成功启动后必须释放。`keepalive` 是唯一例外：它对后台服务承担持续生命周期所有权，必须在整个托管周期持锁，并在退出清理完成后释放；其他生命周期操作必须拒绝干预该托管环境。PID 文件和端口检查继续用于验证真实进程状态，但不能替代生命周期锁。

正常停止必须先停止依赖 System 注册服务的进程及 Runtime 容器，等待其完成注销和退出后，最后停止 System Backend。System 的 HTTP、鉴权和数据库能力必须在前一阶段保持可用，不能同时向 System 和其他模块发送退出信号。两阶段各有 5 秒优雅退出窗口，超时后才强制终止；未完成注销的实例（包括强制终止和注销失败）仍按租约超时观测，不由停止脚本改写注册记录。PID 文件登记及工作区残留监听者使用同一停止顺序；launchd 托管的 System 作业必须留到最后卸载，防止 KeepAlive 重启。Runtime 容器先执行限时 `docker stop` 再删除容器，不直接强制删除运行中的容器。

全局重启的停止阶段只调用 `stop.sh`，Python 服务与 Go 服务遵循同一注销顺序。`restart.sh` 不得在此之前按进程名强杀 Python、Uvicorn 或其他运行时进程，否则会绕过注销并影响其他工作区。

`restart.sh` 无参数或 `-all` 执行全量重启；指定一个或多个模块时，只停止并重启所选模块所属的 Backend、Worker、Frontend 和显式 Runtime。局部停止统一由 `stop.sh -<模块名> ...` 执行，不扩散到模块依赖，不删除其他模块的 PID、前端缓存或 Runtime 容器，不卸载其他模块的 launchd 作业。进程身份须核验工作区归属，前端启动器及其子进程一并停止；残留监听者仅查询所选模块的端口。

局部重启先完成所选 Go 模块的 Swagger 同步和覆盖检查，再停止所选进程；预检失败保留现有服务。随后由 `start.sh -<模块名> ...` 按所选模块的依赖并集启动，运行中的依赖复用，缺失的依赖补齐。`-all` 不得与模块参数混用，重复模块参数去重；`start.sh` 不替代重启，已运行的进程不会因产物重新编译而自动更新。所选模块停机期间，消费其 API 的功能仍可能短暂不可用，重启 System 或 Gateway 尤其会影响公共能力。

应用启动入口只在核心 Infra 未就绪时调用 `scripts/infra/up.sh`。核心容器健康时必须复用，不因日志或指标能力关闭、配置预检失败、观测容器未就绪而构建镜像或执行 Compose 更新。所选观测能力异常时单独报告、继续启动业务并返回非零；设施修复和部署配置应用由显式 `scripts/infra/up.sh` 管理。

单独选择 Asset 时，启动清单为 Asset Backend/Frontend、System Backend、Gateway 和 Console，不包含 Meta Backend/Worker。Catalog 与 Workbench 为业务操作的运行时软依赖，不随 Asset 隐式启动。多模块选择取各自依赖并集；同时选择 Meta 或其他需要 Meta 的模块时，仍保留其 Meta Backend/Worker。前端启动和就绪输出只描述本次清单，不表示全平台前端被重启。

System 自身的注册、心跳和租约回收任务必须使用进程信号 Context；注册成功后，在退出时取消心跳、完成自身实例注销并等待任务结束，再关闭 HTTP 和数据库资源。取消与注册失败不能遗留后台注册任务。

停止阶段的残留端口检查必须一次批量查询目标 TCP 端口的 LISTEN socket，不得逐端口扫描整张进程文件表，也不得把客户端连接视为端口监听。查询结果按 PID 去重，逐个核验进程身份；非 ADDP 进程只报告、不终止，查询后已退出的进程直接跳过。

Go 后端批量启动和前端批量启动各在阶段入口查询一次 TCP LISTEN 快照，阶段结束后立即丢弃，不得跨编译、依赖安装或 Runtime 启动阶段长期缓存。快照命中的占用端口在判定冲突前实时复核；其他单项启动实时查询。仍在运行的受管进程保留 PID 检查，未受管监听者必须阻止启动，不得把端口冲突当成服务已运行。扫描失败不能当成端口空闲。

端口快照不替代操作系统的 bind 检查。前端必须禁止自动切换端口；就绪检查成功后还必须确认本次启动的进程仍然存活，防止快照后发生的端口抢占被其他进程的 HTTP 成功响应掩盖。

Go 后端通过既定健康检查后，所选的独立 Runtime、Copilot 和 Agent 启动任务并行执行，包括各自的准备、启动和就绪等待。每个任务在独立子 shell 中运行，隔离工作目录和环境变量；父进程等待所有任务结束，任一任务失败即阻止 Gateway 和前端启动。成功后从既有 `.dev-pids` 文件回收进程或容器标识用于启动摘要，不把启动任务的 PID 当成服务 PID。

Python 环境的检查和服务启动可以并行，但依赖安装通过 `.dev-state/python-dependencies.lock` 文件锁互斥，避免多个 editable 安装同时改写 `common-python` 的构建元数据。锁由操作系统在安装命令退出时释放，锁文件保留，不通过删除锁文件解锁。该锁只协调内部依赖安装，不替代工作区生命周期锁。Docker 构建具有独立的构建上下文，不参与宿主机 Python 安装锁。

## 模块迁移与 Worker 启动顺序

模块 schema 由所属 Backend 独占迁移，Worker 初始化不执行平台 schema DDL，也不通过数据库连接器或存储构造函数隐式执行迁移。Worker 启动时只读验证模块及公共 schema 的版本；缺失或不匹配即明确失败，不注册为可领取任务的实例，不自动修复数据库。已初始化的数据库允许 Worker 独立重启和扩容。

Backend 使用 `common/schema` 的模块级事务 advisory lock 串行迁移，同一 schema 多实例同时启动只应用一次相同版本。`<schema>.startup_schema_revision` 记录成功提交的版本，迁移失败回滚结构和版本；旧程序不得覆盖较新的版本。模块持有自己的 `SchemaVersion`，任何数据库结构或初始化迁移变化必须同步递增；版本记录不代替已有模块 SQL 迁移目录，只作为该版本初始化完成的证明。SQLite 领域单测继续由测试夹具建立表，不作为正式启动路径。

`common` schema 的执行记录及运行心跳由 System Backend 在就绪前统一初始化，领域实现仍归 `common/execution` 和 `common/runtimehealth`；业务 Backend、Worker 只校验公共 schema。连接数据库与执行迁移分离。

开发脚本和 Compose 按模块执行 `Backend /health/ready 成功 → 对应 Worker 启动`。各模块之间保持并行，不等待全平台 Backend 就绪才启动 Worker。等待必须有超时并确认 Backend 进程存活；Backend 失败不得启动其 Worker。Backend readiness 不依赖 Worker，避免循环依赖。Worker 运行后不因所属 Backend 的短暂不可用被强制停止；涉及结构升级时，必须先停止相关 Worker 领取任务并妥善结束在途任务，再迁移和恢复，禁止新旧 schema 程序混跑。

保护投影存储的构造函数只打开并验证已存在的存储，迁移由 owner Backend 显式调用。Meta 外键由已有版本化 SQL 迁移拥有，不得在每次启动时重复追加约束。

## 二、原子构建

Go 二进制不得直接编译到 `.dev-bins/addp-*` 正式路径。统一流程为：

1. 在 `.dev-bins/.tmp/` 生成当前构建专属临时文件；
2. 构建成功后以同一文件系统内的原子 `mv` 替换正式二进制；
3. 构建失败或进程退出时清理临时文件；
4. 只有正式路径替换成功后才允许启动服务。

源码指纹必须在构建前后各计算一次。如果构建期间输入源码发生变化，本次产物必须废弃并返回失败，不能发布混合快照二进制。

增量构建必须比较当前真实构建输入指纹与上次成功产物旁的 `.fingerprint` 记录。不得仅通过 `.go` 文件修改时间判断二进制是否最新，因为 `//go:embed` 资源、模块依赖文件和工作区配置同样会改变产物。缺失指纹记录的已有二进制必须重新构建，不能推断为最新。

开发重启必须保留 Go 包编译缓存及已有二进制。`restart.sh -all` 和指定 Go 模块参数都先同步 Swagger，再由 `start.sh` 的统一构建指纹校验决定是否重新编译；不得无条件删除产物、执行 `go clean -cache` 或遍历源码执行 `touch`。源码、共享依赖、嵌入资源、模块声明、Go 工具链、目标平台或构建参数变化时重新构建，未变化则复用产物。所有选定服务仍重新启动，复用二进制不会复用旧进程。

复用产物时保留其 `build_id` 和 `built_at`；`started_at` 由新进程重新生成。构建身份描述产物，启动时间描述进程，不能为了标记一次重启而重新链接未变化的程序。已有产物缺少指纹或指纹格式升级时按正常失效处理，不保留旧格式兼容判断。

重启所选模块的 Swagger/OpenAPI 同步遵循 Swagger 集成指南的内容指纹复用规则，需要生成的模块统一批量并行执行，全部生成任务结束后才能进入 Go 编译阶段。`docs.go` 是 Go 构建输入，同一模块的文档写入不得与源码指纹计算或编译并行。生成失败、覆盖不一致或检查无法执行均阻止后续编译和启动，不提供继续启动或告警降级的开关。

## 三、运行时依赖构建输入

Model3D 和 Spark Workflow 的宿主机 Python Runtime 在每次启动和局部重启时，必须复用同一依赖同步函数，通过既有 Python 安装互斥锁，按各自的 `requirements.txt` 和 editable `common-python` 同步依赖，再执行 `pip check`。已有虚拟环境或少量模块可导入不能作为跳过同步的依据；依赖新增、版本调整和共享包依赖变化必须在实际启动环境生效。任一步失败立即阻止 Runtime 启动，不进入服务就绪等待。

所有由开发生命周期启动的 Node 单元必须提交 `package-lock.json`。锁文件是不可变构建输入，启动和重启统一通过 `scripts/dev/node-dependencies.sh` 执行 `npm ci`；缺少锁文件必须立即失败，不得在生命周期内退回 `npm install`、生成锁文件或静默采用未锁定依赖。

`npm install` 只允许用于显式的依赖维护流程，由开发者审查并提交 `package.json` 与 `package-lock.json` 的一致变更。Hosted Online 等要求干净构建身份的门禁在安装前后必须保持仓库状态不变，不能通过忽略锁文件改动、关闭仓库清洁检查或在预检前恢复文件来掩盖生命周期污染。

Spark Workflow 本地开发统一使用宿主机 Python 3.11/3.12 虚拟环境和 OpenJDK 11，与 Business Spark Worker 保持 JVM 主版本一致。`start.sh -spark-workflow` 与 `restart.sh -spark-workflow` 共用唯一原生启动入口，按完整 requirements 和 editable `common-python` 同步依赖并执行 `pip check`。全套重启在停止已有服务前完成 Java、Python、依赖和共享地址预检；失败保留已有服务。HTTP 仅绑定 `127.0.0.1`，就绪必须同时验证原生 PID、监听归属和 HTTP 健康检查；`stop.sh` 按原生 PID 停止服务。macOS 使用 `host.docker.internal` 公布 Driver 与回环数据端点，需要在本机 `/etc/hosts` 配置 `127.0.0.1 host.docker.internal`；Docker Worker 保留 Docker 内置解析。该方式不依赖 Docker Desktop host networking。生产 Compose 和 Hosted 产品验收使用独立容器入口，仍验证同一应用的镜像默认启动命令。

GeoPython Workflow 本地开发统一使用不继承系统包的 Python 3.12 虚拟环境与原生 GDAL，Python 绑定版本必须与 `gdal-config --version` 一致。PGeo 依赖 MDBTools 与 unixODBC，驱动配置限于 Runtime 自身。GDAL/PROJ 资源目录从原生依赖派生，仅注入该 Runtime；不得继承 Anaconda 的资源目录或插件目录。start/restart 共用原生入口，停止前完成依赖、PGeo/FileGDB/COG 驱动、坐标系和 HTTP 端口归属预检；失败保留已有服务。原生开发与产品镜像统一使用单 Worker、四线程 Gunicorn，监听就绪后在同一 Worker 中异步注册。开发入口不构建镜像，栅格 Hosted T4 使用根产品构建入口及独立 Runtime 所有权。

Manager Raster Mosaic Runtime 属于 Manager，随 `stop.sh -manager` 停止、随普通 `start/restart -manager` 启动，独立进程验收入口不隐式启动它。本地开发统一使用 Python 3.12 独立虚拟环境，禁止继承系统 site-packages。停止后的准备阶段自动重建不符合该约束的环境；正在运行或端口被占用时不得改写环境。GDAL 来源选择、资源目录派生和失效绑定源码重建由 `scripts/dev/gdal-env.sh` 唯一实现，GeoPython 与 Raster Mosaic 共同调用；各自检查业务所需驱动，Raster Mosaic 仅要求 GTiff/COG、GDAL NumPy 数组和坐标系能力，不依赖 PGeo/MDBTools。每次启动按完整 requirements 同步依赖并执行 pip check，安装使用既有 Python 依赖锁。保留 Manager 主服务的既定启动策略：Raster Mosaic 准备失败明确报告并跳过该 Runtime，不将其计为就绪；启动后须核验 PID、监听归属和健康响应。

## 四、构建身份

所有由开发脚本构建的 Go 服务必须通过链接参数嵌入以下构建身份：

- `build_id`：本次构建的唯一标识；
- `git_commit`：构建时仓库 HEAD，无法取得时为 `unknown`；
- `source_fingerprint`：该服务构建输入的 SHA-256 指纹；
- `built_at`：UTC RFC3339 构建时间。

`source_fingerprint` 必须通过 Go 构建图覆盖当前目标使用的全部工作区源码、CGo/汇编文件、`//go:embed` 资源、模块依赖文件以及 `go.work/go.work.sum`。同时纳入实际 Go 版本、目标架构、CGo 开关及编译选项、实验开关、GOFLAGS 和目标构建参数。不得按少量文件扩展名猜测构建输入。第三方依赖内容由 `go.mod/go.sum` 锁定，仓库内的 `.gomodcache`、`.gopath` 和其他运行缓存不得进入指纹，否则同一源码会因缓存位置不同产生不同身份。脏工作区不能只使用 Git commit 作为运行版本身份。

## 五、健康响应

所有 ADDP HTTP Backend 的公开 `/health/live` 使用 `common/buildinfo` 生成统一构建身份；`/health/ready` 复用同一身份并增加模块就绪事实。完整响应与状态码契约以 `addp-API设计规范.md` 为准。`/health/live` 至少包含：

```json
{
  "status": "live",
  "module": "model",
  "build_id": "20260813T152509Z-model-39417-21871",
  "git_commit": "a81f3c...",
  "source_fingerprint": "sha256:...",
  "built_at": "2026-08-13T07:25:09Z",
  "started_at": "2026-08-13T07:25:18Z"
}
```

`status` 和 `module` 必须存在。构建身份字段在非开发脚本构建场景下允许为 `unknown`，但字段不能缺失。模块不得维护另一套构建身份字段或改变统一字段语义；旧 `/health` 不保留。

健康检查不纳入 Swagger 公开业务路由覆盖，不要求认证，也不得返回密钥、环境变量或主机敏感信息。

## 六、验证要求

变更开发生命周期和构建链路时至少验证：

- 模块、Worker、残留监听者和 Runtime 在 System 停止前完成退出；launchd 按同一顺序卸载，外部工作区进程保留；
- System 自身收到退出信号后记录 `graceful`，心跳和清理协程结束；
- 同一工作区的第二个生命周期操作会立即失败并显示锁持有者；
- `restart -> stop -> start` 能继承同一锁；
- 构建失败不会覆盖现有正式二进制；
- 构建期间源码变化会拒绝发布产物；
- 全量及多模块重启不提前强杀 Python 服务，保留 Go 缓存和源码时间戳，Swagger 生成失败时不启动服务；
- 单模块及多模块重启只更新所选进程，其他模块和公共依赖的 PID、HTTP 服务、缓存及 Runtime 保持；局部 Swagger 失败保留服务，非法参数在停止前拒绝，所选 Worker、前端子进程和残留监听者全部退出；
- Swagger 对相同内容复用，源码、共享类型、本地 replace、工具、Go 环境和产物变化均失效；生成失败、输入或产物在校验期间变化不能发布有效缓存，并发命令互斥；
- 所选 Runtime 启动任务实际并发，工作目录和环境变量相互隔离，失败时仍等待其他任务结束且不进入下一阶段；
- Python 依赖安装互斥，失败退出后可再次获取安装锁；
- Model3D 已有虚拟环境按完整依赖声明同步，新环境、声明变化及安装失败均有回归覆盖；
- Go/前端批次各扫描一次监听端口，正确处理 IPv4/IPv6、重复记录和已退出监听者；扫描失败及端口冲突必须阻断启动；
- 其他监听者返回 HTTP 200 时，本次启动进程已退出仍必须判定失败；
- 有锁文件的 Node 单元只执行 `npm ci`，安装前后锁文件内容不变；
- 缺少锁文件的 Node 单元在启动依赖安装前失败；
- Go 服务 `/health/live` 返回运行中进程的构建身份，`/health/ready` 反映真实就绪状态；
- `started_at` 来自进程启动，不由脚本或文件时间推断。

确定性生命周期与构建回归入口为 `make test-dev-lifecycle`，由 `make test-platform` 纳入 Platform CI；测试在临时工作区使用生命周期夹具，不接管正在运行的开发服务。

## 进程身份与运行日志

标准启动边界在执行模块进程前产生 `ADDP_PROCESS_INSTANCE_ID`，共享 Go/Python 入口消费同一身份，登记、健康及日志不得各自生成不同 ID。启动器建立受控日志通道后直接 exec 业务进程，业务 PID 和信号语义保持不变；独立接收器不进入模块登记。真正重启产生新身份，同进程重新登记保持身份。源日志按实例追加和分段，禁止固定模块文件截断或应用文件与 stdout 双写。日志接收器使用有界队列并记录丢弃，日志故障不得无限缓存或阻塞业务。
