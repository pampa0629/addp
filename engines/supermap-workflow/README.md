# SuperMap Workflow

`supermap_workflow` 是 ADDP 面向超图 iObjects C++ 的工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。运行时独立校验 `workflow_def`，按 `depends_on` 做稳定拓扑排序，并在单次执行上下文中串行执行 DAG；DAG 内部通过类型化 C++ 句柄传递 SuperMap 内存对象，HTTP 边界只返回 JSON 摘要、最终资产引用和血缘事件。

SuperMap C++ SDK、native `.so` 和许可文件不进入 ADDP 代码仓库。完整 SDK 作为外部只读母版长期保存，通过 `SUPERMAP_CPP_SDK_PATH` 作为 Docker build context 构建稳定基础镜像；许可作为受控制品单独注入。最终运行镜像从该基础镜像复制编译产物和 SDK `bin/bin`，不携带 SDK 的头文件、帮助、样例、支持库目录和构建工具；任何裁剪都不得删除或修改完整母版。

## 第一阶段范围

当前实现提供完整 `addp.workflow/v1` 外壳：

- `GET /health`
- `GET /api/operators`
- `POST /api/workflow`
- `POST /api/operators/{name}/invoke`
- `GET /api/executions/{execution_id}`

当前已内置真实 SuperMap 算子：

- `datasource.open`
- `datasource.open_postgis`
- `datasource.enable_postgis`
- `datasource.upgrade_udbx`
- `datasource.create`
- `dataset.select`
- `dataset.info`
- `dataset.project`
- `vector.filter`
- `vector.spatial_filter`
- `vector.buffer`
- `vector.dissolve`
- `vector.merge`
- `vector.feature_envelope`
- `vector.inner_point`
- `overlay.intersect`
- `overlay.clip`
- `overlay.erase`
- `overlay.union`
- `vector.query`
- `dataset.save`
- `osgb_scene_to_s3m`
- `sgm_to_osgb`

这些算子运行在同一个 C++ 进程和同一次执行上下文内，DAG 边上传递 `UGDataSource`、`UGDatasetVector` 等对象的类型化共享句柄；输出到外部时再写入 UDBX 数据源。

HTTP 层使用独立线程池，确保长时间 SuperMap 算子执行期间，`/health`、算子发现和执行状态接口仍可响应。SuperMap 算子执行通过进程内统一锁串行化，DAG 节点也按稳定拓扑顺序串行执行，避免并发访问非线程安全的 Workspace、Datasource 或 Map 对象；HTTP 并发不等于 SuperMap 计算并发。

算子元数据使用 `param_type=input` 标识由工作流连线传入的参数，并用 `supermap.datasource`、`supermap.dataset`、`supermap.query_result` 等细分类型描述 DAG 内部对象，避免多个通用 `object` 输入只能按连线顺序推断。

`datasource.open_postgis` 只打开已有 SuperMap SDX+ for PostGIS 空间表所在数据源，不调用 SuperMap `create`，因此不会主动创建 SuperMap `sm*` 系统表。`datasource.enable_postgis` 是 direct-only 高危算子，用于 System 引擎管理入口显式启用 SuperMap SDX+ for PostGIS 空间工作区，可能在目标 PostgreSQL 数据库中创建 SuperMap 系统表，不进入 Develop 工作流画布。`datasource.open_postgresql` 与 `datasource.enable_postgresql` 对应实现完全不同的 SuperMap SDX+ for PostgreSQL，二者不得在同一 PostgreSQL 实例并存；SDK 固定使用 `sdx` schema，table session 对 Point 的物理 `SmX/SmY` 与其他类型的私有 Geometry 存储统一输出为 `SmGeometry` EWKB。`datasource.upgrade_udbx` 同样是 direct-only 高危算子：它先以 SQLite 只读检查 UDBX 的 SuperMap 系统表与 `SmRegister` 关键字段，再以可写方式打开原文件，由当前 iObjects C++ SDK 完成原位 schema 升级，关闭后再次检查并返回是否发生变更。它不得在 `datasource.open` 或其他普通读取链路中隐式执行；调用方必须先备份文件并记录审计信息。`locator` 只属于 Develop/UI 的资源选择契约；调用 runtime 前，Develop Backend 必须把它派生为 `connection_info`、`schema` 和 `table` 并移除，SuperMap runtime 不解析 ADDP locator。

`osgb_scene_to_s3m` 同时支持 workflow 和 direct 模式，输入输出统一使用 `addp.workflow.access-plan/v1`。源当前接受 NFS `mounted_path`，运行时按访问计划中的 `server + export_path + nfs_version` 动态挂载；源目录必须包含 `metadata.xml` 与 `Data/Tile_*/Tile_*.osgb`。目标支持 NFS `mounted_path` 与 MinIO/S3 `object_store`：对象存储目标先在本地临时目录完成转换，再递归发布。算子在可写临时目录中镜像 OSGB 场景层级，先调用 `UGOSGBCacheBuilder::GenerateOSGBConfigFile` 生成源 SCP，再使用 `UGObliquePhotogrammetryBuilder::ProcessOSGB` 构建 S3M 3.01：纹理压缩固定为 DXT，几何压缩固定为 Draco，文件类型固定为 S3MB，存储类型固定为 `PURE_FILES`。Builder 保留瓦片局部坐标，运行时随后用 SuperMap `UGRefTranslator` 将 JSON manifest 的 `position` 与 `geoBounds` 从源 EPSG 规范化到 EPSG:4326。发布前必须验证 `version=3.01`、`crs=epsg:4326`、`position.unit=Degree`、位置经纬度范围、`s3m:TextureCompressionType=DXT`、`s3m:VertexCompressionType=DRACO` 及所有根瓦片存在。当前输出为 `config/scene.scp + config/Data/**/*.s3mb`，manifest 中的 `./Data/...` 相对 `config/scene.scp` 解析。Develop workflow 结果是业务存储中的 `format=s3m + layout=whole` 数据集并触发 Meta scan；Manager direct 结果写入 Manager infra MinIO 并由 Manager 维护生命周期。

`sgm_to_osgb` 同时支持 workflow 与 direct：访问计划接受单文件 `file/sgm` 源、`file/osgb` 目标，两端复用 NFS `mounted_path` 和 MinIO/S3 `object_store` 访问能力，目标只允许 `create/replace`。算子先把主文件置于私有目录，通过 SDK 原始材质模式读取非空网格实际绑定材质（含按面子材质）的贴图引用，忽略未使用的材质表条目，依次精确准备同目录图片和 `Textures/` 内的同名图片；首期只接受这两种已验证的扁平布局及 SDK `UGMaterial` 源材质封装，其他材质封装明确拒绝，不列举或递归下载 S3 目录。只在本地文件不存在或 S3 返回 404 时尝试下一候选；403、网络错误、越界符号链接及损坏图片均拒绝生成。随后统一将外部图片复制为私有编号文件及小写扩展名，由 SDK 保存带对应引用的临时 SGM，再读取完整骨架、核对源几何数量并导出 OSGB，并重读核验顶点、三角面数量及非空贴图像素，验证成功后才发布；`create` 不覆盖已有对象。Common 的 SGM 插件仅声明格式身份，不加载 SDK。Manager 依次调用本算子及 Model3D `osgb_to_glb`，中间 OSGB 不形成业务 item；Develop 单独使用时发布业务 OSGB。当前 SDK 的厂商样例通过真实转换回归，其他版本、复杂材质及动画需另行验证，不将其计为全部支持。

### SGM 本机隔离集成测试（T2）

`make test-engines-supermap-sgm` 在本机 Docker 的 `linux/arm64` 环境人工执行真实集成测试。它使用已有私有基础镜像 `addp-supermap-workflow-base:local`，经根 `make build-images IMAGE_BUILD_ARGS="--services supermap-workflow-engine,model3d-workflow-engine --verify --jobs 1 --tag <本轮唯一标签>"` 从当前工作区构建独立标签的 SuperMap、Model3D 产品镜像；MinIO 从仓库固定 Dockerfile 构建。SDK 自带 compass 样例仅在私有临时目录和本轮容器中准备，不复制到仓库或报告。

测试使用独立内部网络、MinIO 卷及随机凭据，不发布宿主机端口、不读取 `.env`、不加入开发网络、不向个人 System 注册，也不重启开发服务。受限 MinIO 用户仅能读取本轮源对象和中间 OSGB、写入本轮目标；另外显式拒绝一张贴图。为验证 404 后的 `Textures/` 精确候选，测试桶授予该用户 `ListBucket`，但通过真实请求追踪核对转换过程中没有目录列举；这不改变产品授权要求。断言范围包括两种贴图布局、真实 403、缺图、坏图、create 冲突与失败保留旧产物、源文件字节保持，以及 SGM → OSGB → GLB 的几何、UV、内嵌贴图像素；入口本身不代表这些断言已经完成真实运行。

贴图参照由本轮同一生产镜像、同一 SDK 样例的容器内 `mounted_path` 源生成，不使用个人 NFS 服务或历史产物。两种 S3 布局的 GLB 解码 RGBA 必须与该参照逐字节一致，并核对 PNG、透明范围、BLEND 材质及有效 UV。SDK 中间模型可能进行有损纹理处理，不能将源 PNG 与 GLB 全像素相等作为既有产品承诺；报告分别记录原 PNG 和派生纹理的哈希，不宣称压缩前像素无损。转换器对已知 DXT／原始像素的独立数值回归继续由产品镜像既有构建自检覆盖。

所有退出路径删除本轮容器、网络、卷、生成镜像标签、凭据、私有样例和原始日志；不清除共享构建缓存或其他镜像引用，清理不完整即失败。可通过绝对路径 `SGM_T2_REPORT=/tmp/sgm-t2-report.json` 保存不含凭据或厂商数据的摘要，只有功能及清理均通过才登记通过。摘要中的 `run_id` 与 `owned_image_tags` 用于 Docker 中断后的精确残留核查；`MEDIA_SHA256` 表示已有私有 SDK 基础镜像的内容身份，不冒充官方安装介质归档摘要。SDK 基础镜像由用户管理，测试不删除。缺少 Docker、基础镜像、许可或架构不符均失败，不计为跳过或通过。

这是用户批准的 macOS ARM64 Docker Desktop / Linux ARM64 本机技术 T2，验证真实存储权限与两个 Runtime 的生产入口；没有覆盖 System User/Grant、Gateway、Manager 页面及业务任务生命周期，不计为正式跨模块 T4，也不自动套用 DM8 的 T4 例外。真实入口登记到人工 `test-integration-owner-managed`，不进入公共 Hosted；生命周期、配置和报告边界的确定性测试随既有 `make test-supermap-workflow` 进入平台 CI。

2026-10-16 首次真实通过：`make test-engines-supermap-sgm` 的运行编号为 `addp-sgm-t2-78505770f0944b588e257c966c46c645`。相关产品构建输入 SHA-256 为 `6e1e3cdb76d6d4ca7a281b4b0600ed2f66c6c2573a20b1ae4e4701f492e03f15`；SuperMap／Model3D 最终镜像分别为 `sha256:afe939e74b09beac015663597ee73cf31b1c1ad89ea4cb64d3f73ddbc68f6923`、`sha256:2963d4196436c03a8ac1934fcdc29249d6bd01b02d79e89ee40a16b80c959d60`，均从本轮源码经标准入口构建，只在本机加载。

SDK 指南针与原配 PNG 分别匹配既有 SHA-256 `4ee43ceb4d9d0752a1567bc7a6f01a63be8ece90bb32d13a74e61db59606e188`、`2b41b171d8f529f7d262fccfb9302e98ab17f93ede6e658a69e8abc853977439`。本地文件参照和两种 S3 布局均为 136 顶点、94 三角面，保留 UV、一处实际贴图引用、RGBA PNG、alpha 0–255 及 BLEND；解码纹理哈希均为 `58a36001c39e50073891d69000112e5b301d9e8bfa9903c0f18c752a85895942`。该值与原 PNG 解码像素不同，未登记为源图片无损。真实 MinIO 的 14 条源 GET 按顺序核对：404 才读取 `Textures/`，403／坏图不回退，转换未列举目录。越权读、源写入／删除、缺图／坏图、create 冲突和非法 OSGB 均拒绝；旧产物和所有源字节保持。

报告 SHA-256 为 `8ab0291e404ba3e6b25e9273f1a648221d11c141cd8b125e8e0bc670acc8c791`。独立复核确认本轮容器、网络、卷和四个镜像标签均无残留，私有临时目录已删除，SDK 基础镜像保留。确定性门禁 `make test-supermap-workflow` 为 15 项脚本回归加 4 项 C++ 测试通过；本次只修正测试的响应读取、MinIO 多行 JSON 和合法默认材质处理，未修改产品转换逻辑或开发服务。此前中断及断言失败记录不计通过；本结果仍不替代正式 T4 或新增厂商模型认证。

UDBX 升级调试示例：

```bash
curl -s http://localhost:8103/api/operators/datasource.upgrade_udbx/invoke \
  -H 'Content-Type: application/json' \
  -d '{
    "params": {
      "connection_info": {},
      "path": "/tmp/supermap-out/legacy.udbx",
      "alias": "legacy_upgrade"
    }
  }'
```

NFS 正式调用时，`connection_info.engine_type` 必须为 `nfs`，`path` 必须是该 NFS export 内的相对路径。响应中 `schema_current=true` 表示当前 SDK 要求的系统表和关键字段齐备，`changed=true` 表示本次调用将旧 schema 升级到当前 schema。已是当前 schema 时算子保持幂等并返回 `changed=false`。

本地开发时，如果 System 中的 PostgreSQL 引擎登记为 `localhost` 或 `127.0.0.1`，SuperMap runtime 容器会通过 `SUPERMAP_RESOURCE_LOCALHOST_ALIAS` 将其映射为容器可访问的宿主机地址。`scripts/dev/start.sh` 和 `scripts/dev/restart.sh` 默认传入 `host.docker.internal`；生产部署应登记容器网络或集群内可直接访问的主机名。

## 本地依赖与两层镜像

SuperMap Workflow 只保留一条本地开发路线：稳定基础镜像承载完整 C++ SDK 和系统依赖，代码镜像在构建阶段编译 ADDP C++ 源码。不得恢复 Java/GPA 回退、胖镜像/瘦镜像判断、源码 bind mount、运行时编译或 `SUPERMAP_WORKFLOW_REBUILD` 等并行路径。

### 本机隔离技术页面验收

用户批准的 `make test-manager-sgm-local` 在同一台 macOS/Linux ARM64 主机的本地 Docker 中启动本轮独占平台与双 Runtime，验证 SGM 的普通 User、正式生成入口、任务页面、GLB 加载、缓存复用、原文件下载及领域／物理清理。它从已提交的 `main` 建立仓库外临时副本，调用根 `make build`、`make build-images --verify`，按正式 Compose 定义派生独立容器名、内部数据网络和卷。独立入口网络只连接需要宿主机访问的 Nginx、System、Gateway 与 PostgreSQL，只发布随机回环端口；Runtime 和对象存储仅连接内部数据网络。Docker Desktop 的纯 internal 网络不建立宿主机端口映射，不能用服务健康代替入口可达。入口先以公开 Nginx 基础镜像验证同样的双网络与回环 HTTP，再进行商业 Runtime 构建；不读取个人 `.env`，不重启或连接开发服务。测试身份沿用 Manager 现有十项权限，由 System 自有夹具在本轮独占 `addp_online` 创建，不使用开发管理员。

```bash
make test-manager-sgm-local-runner
make test-manager-sgm-local SGM_LOCAL_ARGS=--check-network
SGM_LOCAL_REPORT=/tmp/sgm-local-report.json make test-manager-sgm-local
```

`--check-network` 只验证实际 Docker 网络与回环入口，不运行 SDK、身份或页面验收，不计作页面通过。

SuperMap 通过 System 现有扩展工作流登记契约提交完整能力声明；System 验证声明并探测真实健康与算子接口，测试不新增登记路径。

仓库外临时目录保持 `0700`，内部 Git 检出使用标准文件权限（普通文件 `0644`、跟踪的可执行文件 `0755`）。不能把保密文件使用的 `umask 077` 传播到源码检出，否则 Docker COPY／Vite 会保留公开插件的 `0600` 权限，导致 Nginx 非 root 用户读取清单时返回 403；SDK、凭据和原始日志仍受外层私有目录与各自的保密权限保护。

两阶段转换审计通过普通 User 现有权限读取 GLB 成果详情，核对其任务、执行、来源和 Runtime 身份；执行查询仍保持 Owner 声明的摘要投影，不为验收增加审计字段或权限。执行前须选择 `.node-version` 声明的 Node 主版本。

SDK、许可、厂商 SGM／PNG、浏览器日志及失败截图仅保存在本轮私有容器或临时目录，退出统一删除；安全报告只包含构建身份、许可与样例哈希、页面断言及清理摘要。它不归档 SDK、不推送私有镜像、不进入 GitHub Actions 或 schedule，也不计作正式 Linux Runner T4。缺少许可、架构不符、转换失败或清理无法核验均失败。确定性准入、部署隔离、产物检查与清理回归通过既有 `make test-manager-online-runner` 纳入平台 CI；本机真实结果须另行记录，不以入口存在或单元测试通过代替。

### 许可边界

当前开发与验证环境接受 `supermap_any_2026.lic12` 试用许可。该许可可被 iObjects C++ Runtime 正常识别，二维空间分析和 OSGB Scene 转 S3M 均可执行；不增加正式许可与试用许可双轨判断。

Runtime 启动时会验证 iObjects C++ Core 许可可用，`/health` 只表达当前许可足以启动和执行已验证算子，不承诺无试用水印。现有试用许可有效期截至 2026-12-31；到期前应更新 `vendor/license` 中的受控许可并重建完整基础镜像，不能只替换已经构建完成的代码镜像。

本地目录固定为：

```text
engines/supermap-workflow/
├── vendor/
│   └── license/
├── Dockerfile.base
├── Dockerfile
├── CMakeLists.txt
├── src/
└── run.sh
```

`vendor/` 被仓库全局 `.gitignore` 忽略，并被本目录 `.dockerignore` 排除出日常代码镜像上下文。SuperMap SDK、native 库和许可文件不得提交到 Git，也不得推送到公共镜像仓库。完整 SDK 母版保存在仓库之外，并在内部制品库保留原始压缩包、版本号和 SHA-256。

首次安装或 SuperMap 组件升级时，显式指定完整 SDK 母版路径并构建稳定基础镜像：

```bash
SUPERMAP_CPP_SDK_PATH=/path/to/supermap-iobjectscpp-12.1.0-linux-arm64-all \
  bash scripts/build/build-supermap-workflow-base.sh
```

基础镜像默认名为 `addp-supermap-workflow-base:local`，包含编译工具、完整 SuperMap C++ SDK、许可和 NFS/SQLite/Qt 系统依赖。它不包含 ADDP Runtime 源码，因此日常业务代码修改不需要重建基础镜像。完整 SDK 基础镜像用于编译和未来扩展算子；最终运行镜像只承载运行期文件。

代码镜像的编译层和运行层统一通过 Ubuntu 官方 HTTPS 软件源安装依赖。最小 Ubuntu 运行层在安装 `ca-certificates` 前，从受信任编译层复制系统根证书包完成 HTTPS 引导；随后由系统包维护根证书，不关闭 TLS 校验或 APT 签名，也不保留 HTTP 回退。

完整 SDK 自带的 FreeType 版本与 Ubuntu 24.04 Qt/Harfbuzz 不兼容。完整 SDK 母版和基础镜像仍原样保留该库；代码镜像的构建阶段和最终运行层只删除 SDK 副本中的 `libfreetype.so*`，统一使用系统 `libfreetype.so.6`。构建阶段不得全局设置 SDK `LD_LIBRARY_PATH`，否则 CMake 会误加载 SDK 自带 `libcurl/jsoncpp`；运行阶段只允许把 `/opt/supermap/bin/bin` 加入 `LD_LIBRARY_PATH`，不得加入 SDK `support/libs`。

### 镜像裁剪结果

以下是 2026-08-04 在 Linux ARM64 镜像上的实测结果。`压缩内容` 取自 `docker image inspect .Size`，`本地展开` 取自 Docker Desktop 的镜像列表；不同基础镜像补丁版本可能产生小幅变化。

| 镜像 | 压缩内容 | 本地展开 | 说明 |
| --- | ---: | ---: | --- |
| 原 Java/GPA 参考镜像 | 1.515 GB | 6.16 GB | ObjectsJava、GPA、JRE 和系统运行库 |
| 完整 C++ 候选 | 1.510 GB | 6.66 GB | 直接继承完整 SDK 基础镜像，未裁剪 |
| 第一轮 C++ 运行镜像 | 0.963 GB | 4.75 GB | 多阶段构建，只保留 SDK `bin/bin` 和运行依赖 |
| 当前 C++ 运行镜像 | 0.478 GB | 2.02 GB | 在第一轮基础上按动态追踪结果裁剪可选插件和资源包 |

当前 C++ 运行镜像相对 Java/GPA 参考镜像，压缩内容减少约 68.4%，本地展开体积减少约 67.2%；相对第一轮 C++ 运行镜像，分别继续减少约 50.4% 和 57.5%。第一轮裁剪排除了 `include/`、`help/`、`sample/`、`support/`、编译器、CMake 和其他开发文件，运行镜像不包含 JRE、ObjectsJava 或 GPA runtime。

第二轮对 20 节点二维 DAG、PostGIS 只读打开、CAD 检查与渲染、UDBX 升级和 OSGB 转 S3M 做了完整动态文件访问追踪，共确认 338 个实际打开的 SDK 文件。在与追踪结果零冲突的前提下，删除了 124 个未使用的 IFC/BIM、天气、海洋和 JVM 顶层插件文件，以及 `SilverLiningResource`、`OceanResource` 两个资源目录；这些文件在完整 SDK 中的展开体积合计约 2.25 GB。

S3M 转换实际会动态加载 PCL、VTK 和 OpenVDB，因此这些库必须保留。CAD 核心实际会加载 `libUDKernel.so`、`libUDDrawing.so`、`libUDComponents.so` 和 `libUDDgn.so`，也必须保留。完整 SDK 母版和基础镜像继续保存全部插件、头文件和开发资源，以支持未来新增算子；后续扩大裁剪范围时仍必须先补充动态追踪场景，再重跑 24 个算子、PostGIS、UDBX、CAD、S3M、动态 NFS 和 MinIO/S3 回归，不能仅按静态链接关系删除 `.so`、`.sdx`、`.spi`、`.l3d` 或资源文件。

日常启动和重启统一使用：

```bash
bash scripts/dev/start.sh -supermap-workflow
bash scripts/dev/restart.sh -supermap-workflow
```

`restart.sh -supermap-workflow` 和 `restart.sh -all` 会根据 C++ 源码、`CMakeLists.txt`、`run.sh`、Dockerfile、基础镜像 ID 和目标平台计算构建指纹。指纹与现有代码镜像一致时直接复用；输入变化或镜像不存在时才在镜像构建阶段重新编译。之后脚本会替换 8103 容器、等待 healthy 并向 System 注册。C++ 编译失败会在旧容器被删除前终止，不会启动旧二进制。

代码镜像默认名为 `addp-supermap-workflow-engine:dev`。可配置项只保留运行参数：

```bash
SUPERMAP_WORKFLOW_BASE_IMAGE=addp-supermap-workflow-base:local
SUPERMAP_WORKFLOW_IMAGE=addp-supermap-workflow-engine:dev
SUPERMAP_WORKFLOW_PORT=8103
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out
SUPERMAP_WORKFLOW_MEMORY_LIMIT=8g
```

工作流算子由 Manager、Meta、Develop 等调用方实时从 `/api/operators` 发现。只修改 SuperMap C++ 代码时，无需重启 System、Manager 或 Meta；局部重启 SuperMap Workflow 即可。

## 验证

不依赖厂商 SDK 的协议、算子目录、资源主机和贴图路径边界测试使用 `make test-supermap-workflow`，已登记根 `make test`、模块自动发现和 Platform CI。该入口需要 CMake、C++20 编译器与 nlohmann-json 开发包，构建目录位于操作系统临时目录并自动清理。

SGM 原生转换、NFS / S3 外贴图、大小写扩展名及原始对象键、候选查找顺序、缺图／403／500／损坏图片拒绝、输出保留及临时文件清理由镜像构建阶段的 `sgm-conversion-test` 验证。S3 用例使用进程内回环 HTTP 夹具调用真实 SigV4 下载路径，只证明精确对象读取协议，不代表真实 MinIO 权限或 Manager T4；SDK 样例只从外部构建上下文读取，不进入代码仓库。标准入口：

```bash
make build-images IMAGE_BUILD_ARGS="--services supermap-workflow-engine --verify --jobs 1 --tag <独立验证标签>"
```

2026-10-15 大写图片扩展名回归：当前 Linux SDK 读取秋树的 `hongye.PNG` 时丢失红叶贴图绑定，同一图片字节及模型引用统一为小写扩展名后可读取。所有输入统一在执行私有目录由 SDK 准备模型，业务模型和图片不改名、不改写，未增加失败重试路线。新增 `.PNG`／`.PnG` 的 NFS 与签名 S3 回归先在旧代码上复现缺图，再核对修复后的精确对象键、贴图缓冲字节、缺图／损坏／403／500 拒绝、旧产物保留和临时文件清理。标准产品镜像构建通过五项 SDK CTest，镜像 ID 为 `sha256:f33f41f2ff5a49a9bc4a39e30c1d8c5c63aa651c8cb917f298655212c1a4343b`。

用户已有 iDesktopX 样例的隔离验证通过同一生产算子生成 OSGB，再通过 Model3D 正式算子生成并校验 GLB：秋树和春树均为 9589 顶点／7273 面／两张 1024² 图片，冬树为 3154 顶点／1919 面／一张 1024² 图片，汽车为 21012 顶点／11683 面／无图片。春树、冬树及汽车与修复前产物逐项比较坐标、法线、UV、索引、场景节点和绑定图片像素保持一致，不依赖无语义的 primitive 排列顺序。隔离源码副本通过根 `make test-model3d-workflow`（278 既有测试＋4 个临时真实样例测试，共 282 项）；私有样例及临时测试未进入仓库或公共镜像。同轮 `make test-module MODULE=engines` 及 `make test-build-registration test-engine-startup-isolation` 通过；首次模块门禁因 zizmor 下载网络失败，使用已校验离线 wheel 后完整复验通过。该隔离转换验证本身不代替 Manager 页面或 Hosted T4 验收；该轮 JPEG 输出未认证 PNG 透明度，后续透明贴图验收见下文。


2026-10-15 用户重启后的真实页面验收：运行镜像 `sha256:012fc2f37ed8c34dafc6742ea8e86e2bde6ff98bfca677b0f89c78dd93955961` 的源码指纹 `1d93e5f0015c3f396e17a8b1fb111ef2843f0add` 与当前 SDK 基础镜像、构建文件和源码完全一致。Business MinIO 秋树 `item_id=44563` 经 Manager 页面重新执行任务 17，于 16:53:54 成功并可用；生成 GLB 为 839688 字节，SHA-256 `d0bc31aad15f561f0c92157f2e0c5c10072fb820a16606d7dae68a12b2aeacbb`，保留 9589 顶点、7273 面和两张 1024² RGB JPEG，两张图片均实际绑定到材质及有效 UV。浏览器首次生成、重新打开缓存和旋转查看均通过；211 个业务源文件哈希不变，Manager 的当前租户 SGM 临时前缀与 SuperMap Runtime 临时目录均为空。源码 `8f17c1d8f` 的 [Platform CI 38706914360](https://github.com/pampa0629/addp/actions/runs/38706914360) 已通过。本记录是本地真实页面验收，不计为 SGM Hosted T4；该轮 JPEG 输出尚未保留叶片透明通道。

2026-10-15 23:11:08，Model3D 重启加载 `51fccfb784dbf9a0` 后，经同一 Manager 任务 17 再次生成秋树快显，完整 SGM → OSGB → GLB 路线保留叶片透明度。新 GLB 为 1608268 字节，SHA-256 `c6805b7c36b08c26737ccc9ecd8c37223df908545c4b8b2b9ed961b3bd6aee81`；9589 顶点、7273 面、两张 1024² 图片及材质 UV 保持，叶片改为 RGBA PNG 和 `BLEND`，树干保持 RGB JPEG。叶片全透明／半透明／不透明像素为 774205／126685／147686，与 OSGB 解码一致；页面白底消失，旋转与刷新缓存正常。211 个业务源文件哈希不变，Manager 当前租户 SGM 临时前缀与 SuperMap Runtime 临时目录为空。转换器源码 `808a7c729` 的 [Platform CI 38742900337](https://github.com/pampa0629/addp/actions/runs/38742900337) 及 Linux x86_64 产品构建通过，同一源码的 Manager Hosted T4 亦通过（八种既有模型及下载／清理回归，不含私有 SGM 样例），完整范围见 [Model3D 验收记录](../model3d-workflow/README.md)。本条为本地真实 SGM 页面验收，不计为 SGM Hosted T4。

健康检查：

```bash
curl http://localhost:8103/health
```

执行 UDBX 叠加分析 DAG：

```bash
curl -s http://localhost:8103/api/workflow \
  -H 'Content-Type: application/json' \
  -d '{
    "workflow_def": {
      "tasks": [
        {
          "id": "open_input",
          "operator": "datasource.open",
          "params": {"path": "/mnt/supermap/data/example_data.udbx", "alias": "example_data", "read_only": true},
          "depends_on": []
        },
        {
          "id": "create_output",
          "operator": "datasource.create",
          "params": {"path": "/tmp/supermap-out/addp_overlay_result.udbx", "alias": "overlay_output", "overwrite": true},
          "depends_on": []
        },
        {
          "id": "select_landuse",
          "operator": "dataset.select",
          "params": {"datasource": {"$ref": "open_input"}, "dataset_name": "Landuse_R"},
          "depends_on": ["open_input"]
        },
        {
          "id": "select_geomor",
          "operator": "dataset.select",
          "params": {"datasource": {"$ref": "open_input"}, "dataset_name": "Geomor_R"},
          "depends_on": ["open_input"]
        },
        {
          "id": "intersect",
          "operator": "overlay.intersect",
          "params": {
            "input_dataset": {"$ref": "select_landuse"},
            "overlay_dataset": {"$ref": "select_geomor"},
            "output_datasource": {"$ref": "create_output"},
            "output_dataset_name": "OverlayOutput",
            "overwrite": true
          },
          "depends_on": ["select_landuse", "select_geomor", "create_output"]
        }
      ]
    },
    "input_data": {}
  }'
```

期望 `status=success`，`all_results.intersect.kind=supermap_dataset`，并生成目标 UDBX 文件。

上面的 `/tmp/supermap-out` 只用于 runtime 直接调试。Develop 正式任务中，`datasource.create` 应通过 `target_parent_locator + target_name` 选择 NFS 目录；Develop Backend 在执行期把 NFS 引擎的通用连接事实（`server`、`export_path`、可选 `nfs_version`）和挂载根内相对 `path` 传给 runtime，runtime 在容器内动态挂载该 NFS export 后由 SuperMap C++ 直接写入 UDBX。NFS 引擎不需要、也不应配置工作流专用挂载路径。UDBX 暂不支持直接写入 MinIO / S3。

执行已验证的示例分析 DAG：

```bash
SUPERMAP_DATA_HOST_PATH=/path/to/supermap-iobjectspy-2026-linux-arm64/data \
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out \
./scripts/dev/restart.sh -supermap-workflow

curl -s http://localhost:8103/api/workflow \
  -H 'Content-Type: application/json' \
  -d '{
    "workflow_def": {
      "tasks": [
        {"id":"open_input","operator":"datasource.open","params":{"path":"/mnt/supermap/data/example_data.udbx","alias":"example_data","read_only":true},"depends_on":[]},
        {"id":"create_output","operator":"datasource.create","params":{"path":"/tmp/supermap-out/addp_supermap_analysis.udbx","alias":"addp_supermap_analysis","overwrite":true},"depends_on":[]},
        {"id":"select_landuse","operator":"dataset.select","params":{"datasource":{"$ref":"open_input"},"dataset_name":"Landuse_R"},"depends_on":["open_input"]},
        {"id":"filter_large","operator":"vector.filter","params":{"dataset":{"$ref":"select_landuse"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLarge","attribute_filter":"Area > 1000","overwrite":true},"depends_on":["select_landuse","create_output"]},
        {"id":"inner_point","operator":"vector.inner_point","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargePoint","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"envelope","operator":"vector.feature_envelope","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargeEnvelope","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"project","operator":"dataset.project","params":{"dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLarge3857","target_epsg":3857,"overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"dissolve","operator":"vector.dissolve","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargeDissolve","field_names":["Area_1"],"dissolve_type":"multipart","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"final_info","operator":"dataset.info","params":{"dataset":{"$ref":"dissolve"}},"depends_on":["dissolve"]}
      ]
    },
    "input_data": {}
  }'
```

该示例用于验证 `vector.filter`、`vector.inner_point`、`vector.feature_envelope`、`dataset.project`、`vector.dissolve` 可在同一 SuperMap C++ DAG 执行上下文中连续执行，并在输出 UDBX 中生成 `LanduseLarge`、`LanduseLargePoint`、`LanduseLargeEnvelope`、`LanduseLarge3857`、`LanduseLargeDissolve`。

Develop 任务管理验证可以通过标准开发任务 API 保存同一 `workflow_definition`，`execution_config.engine_id` 指向 System 中已注册的 `supermap_workflow` 引擎实例。统一执行记录只保存结果摘要和 runtime execution id；完整节点结果可在 runtime 本地状态中查询，后续资产登记和血缘应把需要长期保留的输出数据集引用结构化写入 ADDP 任务/血缘模型。

## 架构约定

- Develop 前端只生成 ADDP `workflow_def`，不直接生成 SuperMap `processflow` JSON。
- 每个 SuperMap 算子在该 runtime 内实现为 C++ Operator，不拆成独立 OS 进程或独立 HTTP 服务。
- DAG 内部优先传递 `UGDatasetVector`、`UGDataSource`、`UGRecordset`、`UGGeometry` 的共享句柄；只有输入、最终输出、显式保存、checkpoint、跨进程边界才落盘。
- HTTP 响应不得返回完整大对象，只返回摘要、保存结果引用和 `lineage_events`。
- 真实空间算子接入时不得硬编码空间字段名，必须从 SuperMap 数据集/字段元数据读取。
