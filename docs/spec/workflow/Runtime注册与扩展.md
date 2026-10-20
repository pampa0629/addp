# ADDP 工作流 Runtime 注册与扩展指南

[返回工作流计算引擎接口规范](../addp工作流计算引擎接口规范.md)

## 引擎注册

工作流运行时注册后形成 ADDP 引擎实例身份；可用候选还必须满足 active、online 和目标 capability。生产内置运行时先监听，再异步向 System Backend 自注册；本地必需工具缺失或 `/health` 返回 unhealthy 不阻止身份登记，注册失败按统一策略退避重试。System 通过公共 Provider 周期探测，将未就绪实例标为 offline、恢复后的实例标为 online，不改变生命周期和永久身份。注册成功不改变健康结果，Runtime 健康也不依赖注册成功。参考示例运行时可以只作为扩展规范样例存在，由用户在 System 引擎管理中按扩展引擎手动注册。

所有注册方式最终都必须形成同一种 System Engine Instance：实例保存稳定 `engine_type`、`engine.capabilities/v1` 和确定的 `protocol/host/port`。业务模块通过 Tenant Service Access Token 读取 System Runtime Descriptor；不得读取 `/api/v1/internal/engines`、转发用户 Token、发送 Internal API Key，或根据运行时名称和固定端口拼接 endpoint。Common Engine 根据实例 capabilities 选择唯一的通用 HTTP Provider，不维护 GeoPython、Spark、Math、Model3D、PointCloud、SuperMap 等按 `engine_type` 重复实现的 Workflow Plugin。

GeoPython Workflow / Spark Workflow 等生产内置运行时的注册由所属模块使用独立 Service Principal 和 Confidential OAuth Client 完成：先以 `context_type=platform` 通过 Client Credentials Grant 获取短期 Platform Service Access Token，再调用 System 唯一的公开注册 API，并且只发送 `Authorization: Bearer <service_access_token>`。不得调用 `/api/v1/internal/**`，不得发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。需要外部 SDK 或许可绑定的运行时（例如 SuperMap Workflow）也可以启动后由 System 扩展引擎表单手动注册。

Math Workflow 是 `addp.workflow/v1` 参考实现，可以随 ADDP 开发环境启动服务，但不随启动自动注册。需要使用时，在 System 引擎管理中选择“注册扩展引擎”，填入 `engine_type=math_workflow`、默认端口 `8089`，可先通过表单“检查服务”确认 `/health` 与 `/api/operators` 可达，再测试连接并保存。

默认部署运行时和用户自研扩展运行时在 System 中待遇一致：业务模块不得假设 `geopython_workflow`、`spark_workflow`、`math_workflow`、`model3d_workflow`、`pointcloud_workflow` 等工作流引擎一定存在；只有已注册、启用且声明 `compute.workflow.supported=true` 的 Engine Instance 才能被发现和调用。所有实现 `addp.workflow/v1` 的运行时都必须在自注册 payload 中提交或由手动注册表单生成完整 `engine.capabilities/v1`；System 校验并保存该声明，不按 `engine_type` 生成内置能力。算子列表、参数、分类、执行模式和输出端口通过 `GET /api/operators` 动态获取，不写入能力声明。

System 在保存声明 `compute.workflow.supported=true` 且 `compute.workflow.runtime_api="addp.workflow/v1"` 的手动注册扩展运行时前，必须执行一次只读协议探测：

- 校验 `engine.capabilities/v1` 的 `engine_type` 与注册请求中的 `engine_type` 完全一致。
- 调用该运行时实例的 `GET /health`，必须满足 HTTP `200`、`status="healthy"` 及完整健康响应契约；复用公共 Provider 的校验，不能只判断 HTTP 状态。
- 调用同一运行时实例的 `GET /api/operators`，响应必须是 `addp.workflow/v1` 算子列表结构。
- 若 `operators` 非空，必须对每个算子执行完整元数据契约校验；其中算子 `engine_type` 必须等于注册请求中的 `engine_type`，不得返回 `geopython_workflow`、`spark_workflow` 等其他运行时类型作为兼容值。
- 探测阶段只验证协议面、必填字段、分类、执行模式和输出端口结构；不做算子参数枚举、范围、正则、业务语义等深度校验。

System 管理界面可以在用户保存前提供手动“测试连接/协议探测”入口，便于用户先确认运行时可达和算子契约有效。该入口只能复用创建前测试 API，不能在前端或 System 后端另写一套按具体 `engine_type` 判断的探测逻辑。

探测失败时 System 不得保存该引擎，也不得降级为“已保存但不可用”。创建前测试 API 和最终保存入口必须使用同一条 Common Engine / `WorkflowRuntimeProvider` 主路径；最终保存入口仍必须再次探测，不能因为前端已经手动测试通过就跳过后端校验。普通通用引擎仍按对应 `EnginePlugin.TestConnection()` 做只读连接测试。

工作流执行由 Common Engine 的 `WorkflowRuntimeProvider.ExecuteWorkflow()` 统一调用。Common Engine 在调用运行时前必须通过同一工作流运行时实例的 `ListOperators()` 获取算子列表，严格校验算子元数据，并确认工作流定义中每个 `tasks[].operator` 存在且 `execution_modes` 显式包含 `workflow`；不满足时应拒绝执行，不能把 direct-only 算子交给工作流运行时编排。

Common Engine 同时必须校验工作流定义本身：

- `tasks` 必须是非空数组。
- 每个任务必须显式提供非空 `id`、非空 `operator`、对象类型 `params`、字符串数组 `depends_on`。
- `tasks[].id` 不得重复。
- `depends_on` 不得为空字符串、不得引用自身、不得重复、不得引用不存在的任务。
- 任务依赖图不得包含环。
- `params` 中出现 `{ "$ref": "<task_id>" }` 时，`<task_id>` 必须存在且必须同时出现在该任务的 `depends_on` 中。
- `{ "$ref": "<task_id>", "port": "<port_name>" }` 引用非默认端口时，`<port_name>` 必须存在于被引用任务算子的 `output_ports[]` 中。

Develop 画布加载、导入和再次保存时必须保留这一区别：没有参数引用的 `depends_on` 仅以虚线显示执行顺序，不参与输入参数绑定；不能根据端口类型或参数名称为它补造 `$ref`。真实数据连线继续以实线显示并按参数引用保存。

工作流运行时仍必须保留同等或更细的校验防线，但不能把 Common Engine 的前置校验缺失作为运行时私有行为处理。

若某类工作流引擎需要绑定外部运行时资源，例如 `spark_workflow` 需要实际 Spark 资源 ID，应作为执行期运行时参数传入标准请求顶层字段（当前为 `engine_id`），而不是写入 `capabilities`，也不是由 Develop 等业务模块直接拼接引擎私有 HTTP 契约。对 `spark_workflow`，Develop 执行配置必须提供 `engine_specific.spark_cluster_id`；后端在调用 Provider 前必须校验该 ID 指向已启用的 `engine_type=spark` 通用引擎资源，并将其映射为标准请求顶层 `engine_id`。GeoPython Workflow、Math Workflow、Model3D Workflow、PointCloud Workflow、SuperMap Workflow 不需要也不得携带该 Spark 绑定。

Model3D Workflow 是三维模型转换专用运行时，`engine_type=model3d_workflow`，默认端口 `8101`。当前转换算子均同时支持 workflow 与 direct，完整清单与逐格式范围见 [Model3D README](../../../engines/model3d-workflow/README.md)。算子只表达持久化格式转换，不把 Manager 快显归属写入 Runtime Operator Spec；Manager direct 调用选择 infra 目标并登记私有 artifact，Develop workflow 调用要求用户选择业务存储并触发 Meta scan。`_3dtile`、Assimp、IfcConvert、OpenSKP、Blender/MAX 导入器和 KSplat 脚本均是运行时内部依赖。PLY / SPLAT 高斯泼溅可以转 KSplat；源已经是 KSplat 时直接作为业务数据项或 Manager 基础预览读取，不进入转换算子。

Develop 对 `dae_to_glb`、`3ds_to_glb`、`max_to_glb` 使用显式资源适配：公开定义选择源主文件，执行期从其父目录派生 `directory` 源并保留主文件 `entrypoint`，供运行时读取相邻贴图。三者均输出业务存储中的 `.glb` 单文件。MAX 额外公开长度单位 `source_unit`（`mm/cm/m/km/in/ft/mi`，默认 `m`）及 `texture_files` JSON 映射，后者将模型内位图引用映射到主文件所在目录内的相对图片路径，执行时归入 `options`。原始 `access_plan/options` 不允许作为用户执行输入。转换选项的名称和说明使用 Develop 国际化消息，算子发现根据 `Accept-Language` 返回。

Document Workflow 的 `document_to_pdf` 当前只接受 PPTX；Develop 通过单文件源和 `.pdf` 业务目标适配 `addp.workflow.access-plan/v1`，公开 `strip_embedded_media`（默认 `true`）并派生为运行时 `options`。SuperMap 的 `sgm_to_osgb` 采用单文件源和 `.osgb` 业务目标适配，运行时只接收 `access_plan`，不附加 `options`。这五个转换算子复用 Develop 的源读取／目标写入授权、稳定输出和 Meta 自动扫描主线，不登记 Manager 私有快显产物，不隐式跨运行时串联。新增验证由现有 Develop Go T1 和模块门禁自动发现，无新增基础设施依赖。

Model3D 的 macOS Apple Silicon 开发态固定使用私有原生 `_3dtile`、Assimp 5.2.5 与 IfcConvert 0.8.4-e8eb5e4；不使用 Docker 转换 wrapper 或镜像回退。工具包按架构、固定源码、Cargo.lock、ADDP patch、原生依赖清单和安装实现计算指纹，安装完成后校验文件并执行真实压缩 OSGB、贴图和 IFC 转换预检。原生构建与运行不继承全局 GDAL/PROJ 或动态库路径。首次构建依赖 Rust 1.92.0、CMake 4.4.0、Xcode Command Line Tools 和 Conda；后续启动复用已验证工具包。当前仅验收 Darwin arm64 开发安装，其他平台不得回退到开发容器。Linux 产品部署及 Hosted 产品验收继续使用正式 Runtime 镜像；Hosted 验收不通过开发脚本启动 Model3D。开发启动、局部重启和全量重启复用同一准备函数，外部监听或运行中依赖漂移必须在停止服务前拒绝。

SuperMap Workflow 的 `sgm_to_osgb` 同时支持 workflow 与 direct，使用 `addp.workflow.access-plan/v1` 的单文件 `file/sgm` 源和 `file/osgb` 目标。两端支持 NFS `mounted_path` 与 MinIO/S3 `object_store`，目标写入模式只允许 `create/replace`。Runtime 将主文件置于私有目录，根据 SDK 材质引用精确准备相邻外部贴图，不递归搬运源目录；缺图、越界、读取失败或无有效像素时拒绝发布。导出 OSGB 后重读核验非空几何、数量和贴图完整性，再发布，跨 HTTP 仅返回不含凭据或临时路径的发布摘要。Manager 的 SGM GLB 快显在一个 execution 中串行调用该算子与 Model3D `osgb_to_glb`，临时 OSGB 归 Manager infra 并在执行返回时尝试清理；Develop 单独消费该通用算子时选择业务 OSGB 目标，不带 Manager 私有归属。

SuperMap Workflow 的 `osgb_scene_to_s3m` 同时支持 workflow 与 direct，使用 `addp.workflow.access-plan/v1`。源当前使用 NFS `mounted_path` whole scope；目标支持 NFS `mounted_path` 和 MinIO/S3 `object_store`。运行时必须先调用 iObjects C++ `UGOSGBCacheBuilder::GenerateOSGBConfigFile` 生成源 SCP，再通过 `UGObliquePhotogrammetryBuilder` 构建唯一的 S3M 结果路径；构建参数固定为纹理 DXT、几何 Draco、版本 3.01 和文件类型 S3MB。Builder 必须保留 S3MB 内部和 `rootTiles[].boundingBox` 的场景局部坐标；构建完成后，运行时使用 SuperMap C++ 坐标转换 API 按源 EPSG 将 JSON SCP 的 `position` 与 `geoBounds` 统一规范化为 `EPSG:4326`，不得启用会改变瓦片局部坐标语义的中心修改操作，也不得让前端猜测投影或为单个 EPSG 硬编码转换参数。当前多根场景产物为 JSON `config/scene.scp + config/scene/Data/**/*.s3mb`，manifest 中的 `./scene/Data/...` 始终相对 `config/scene.scp` 解析。ready 前必须验证 manifest 的 `crs=epsg:4326`、`position.unit=Degree`、位置经纬度范围和全部根瓦片引用。`root_tile_count` 以 Builder 输出 manifest 的实际 `rootTiles` 为事实；Builder 可以跳过空或无有效几何的源根占位，因此不要求输出根数与源目录候选数严格相等；但 manifest 必须至少包含一个根瓦片，且每个引用均必须存在。Develop workflow 模式写入用户选择的业务存储，结果经 Meta 扫描形成 `format=s3m + layout=whole` item；Manager direct 模式写入 Manager infra MinIO，登记为 `manager.model3d_tiles` 中 `target_format=s3m` 的受管快显结果，不形成 Meta item。

OSGB Scene 的 Manager 分块瓦片任务统一使用 `model3d_tiles_generation`。任务定义保存在 `manager.task_definitions`，`source.item_fingerprint + target_format` 标识当前目标结果；`target_format` 只允许 `3d_tiles`、`s3m`。`3d_tiles` 调用 `osgb_scene_to_3dtiles`，`s3m` 调用 `osgb_scene_to_s3m`。Manager 必须根据当前租户可用工作流引擎实例及其 direct 算子声明决定格式能否生成；已生成结果的读取不依赖工作流引擎在线。

PointCloud Workflow 是点云处理专用运行时，`engine_type=pointcloud_workflow`，默认端口 `8102`。`las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 和 `xyz_to_copc` 同时支持 workflow 与 direct。PDAL 是运行时内部依赖；运行时未绑定 PDAL 时 `/health` 返回 HTTP 503 + unhealthy，但监听后仍异步登记身份，System 将其观测为 offline。源和目标由调用方按 `addp.workflow.access-plan/v1` 派生；PointCloud Runtime 不解析 ADDP locator。对象存储源由运行时按 `object_store` 访问参数生成受控读取方式，COPC 始终先写入 `POINTCLOUD_WORK_DIR` / `CPL_TMPDIR`，再按目标访问参数发布。Manager direct 结果仍是 infra 快显 artifact；Develop workflow 结果是用户选择的业务 COPC 数据项。源已经是 COPC 时直接读取，不进入转换算子。

SuperMap Workflow 是超图数据格式和空间算法专用运行时，官方实现使用 `engine_type=supermap_workflow`、默认端口 `8103`。运行时由 C++ 实现，对外提供 `addp.workflow/v1`，对内绑定 SuperMap iObjects C++ SDK 和许可文件。运行时必须独立校验 `workflow_def`，按 `depends_on` 做稳定拓扑排序，并在单次执行上下文中串行执行 DAG；第一阶段不实现条件、循环或子工作流。节点间通过 C++ 类型化内存句柄传递 Datasource、DatasetVector 等对象，只有用户显式保存数据集或跨 HTTP 边界返回结果摘要时才落盘。当前真实算子覆盖 datasource open/open_postgis/open_postgresql/create/enable_postgis/enable_postgresql、dataset select/info/project/save、vector filter/spatial_filter/buffer/dissolve/merge/feature_envelope/inner_point/query、table bounded batch read/write、overlay intersect/clip/erase/union，以及 OSGB scene 转 S3M。`datasource.enable_postgis`、`datasource.enable_postgresql`、`datasource.upgrade_udbx` 和 `table.read_*`、`table.write_*` 是 direct-only 算子，不进入 Develop 工作流画布，`osgb_scene_to_s3m` 同时支持 workflow 与 direct。

SuperMap table direct session 使用唯一线协议 `supermap.table-batch/v1`：请求和响应承载统一 `BatchData` JSON，二进制字段按 JSON 标准 base64 编码，geometry 字段固定承载 EWKB。当前不声明 Arrow IPC；运行时不得接受第二种批编码并静默回退。若性能验证确认 HTTP JSON 编解码成为主要瓶颈，必须先修订本规范并引入 ABI 隔离的单一替代编码，再删除本协议实现。

GeoPython Runtime 的 GDAL vector dataset direct 能力服务于 FileGDB、PGeo 等不能嵌入业务 Backend 的格式 Provider。容器检查算子固定为 `vector_dataset.inspect`：输入 source-only `addp.workflow.access-plan/v1` 与 `child_limit`，输出唯一结构 `gdal.vector-dataset.inspect/v1`，其中 `container` 只包含 child 名称、类型、行列数和 `native.table` 等轻量索引，`format_info` 保存 GDAL driver/version 等格式私有事实，不返回行样本或所有 child 的完整字段。批处理算子固定为 `vector_dataset.read_open/read_batch/read_close` 和 `vector_dataset.write_open/write_batch/write_close/write_abort`；PGeo 只声明 inspect/read 算子。批处理使用唯一线协议 `gdal.vector-batch/v1`，请求通过 source-only 或 target-only access plan 并显式携带由已确认 container child 派生的 `layer`；Runtime 不解析 locator。任务配置只保存通用 `child_name`，调用方必须从 Meta `type_info.container.children` 解析真实入口，不得要求用户填写 GDAL layer。批次承载统一 `BatchData` JSON，bytes/geometry 按 JSON base64 编码，geometry 固定为 EWKB。会话协议必须对 Gunicorn 多 worker 安全，不得依赖单 worker进程内 session map；第一阶段以调用方持有 offset/target facts、Runtime 每次调用重新打开 dataset 的无服务端状态实现为准。FileGDB writer 的 `write_open` 创建完整目标 scope，后续 batch 只追加选定 layer，`write_abort` 清理未完成 scope；`write_close` 成功才形成提交点。

`datasource.enable_postgis` 只初始化 `supermap/sdx_postgis`；`datasource.enable_postgresql` 只初始化 `supermap/sdx_postgresql`。System 必须依据目标 PostgreSQL 实例的 PostGIS 事实选择其中一个算子，同一实例不得启用两者。`datasource.open_postgis` 使用 `UGEngineType::PostgreSQLGis`，只打开已有 PostGIS 空间表所在数据源，不得调用 create；`datasource.open_postgresql` 使用 `UGEngineType::PostgreSQL`，只打开已有 SuperMap SDX+ for PostgreSQL 工作区。两个 open 算子和两个 enable 算子不得互相回退。

Transfer 对 `supermap/sdx_postgresql` 的 bounded table 读写必须通过运行时 direct table session 算子完成。跨 HTTP 边界固定使用 `supermap.table-batch/v1` JSON 承载 ADDP `BatchData`，geometry 值固定为 base64 EWKB，字段名由 `SpatialInfo.primary_geometry_column` 和 `geometry_columns` 指定，不得假定为 `geom`。读取由 SDK Recordset 分块导出为 JSON/EWKB；写入由 SDK 将 EWKB 解码到分块 `UGMemRecordset` 后调用 `DatasetVector.Append`。写会话成功关闭前必须调用 `ComputeBounds()` 和 `UpdateSpatialIndex()`，关闭并重开 Dataset 后校验 `GetObjectCount()`、Bounds 非空语义与空间索引非 dirty；任一步失败都必须使 Transfer execution 失败。SuperMap 私有 geometry Blob 不得进入 Transfer、Common Spatial、BatchData 或任务 JSON。

PostgreSQL Engine Instance 的 SuperMap 空间工作区只保存 `bound_runtime_engine_id`，不保存或校验 `runtime_engine_type`。System、Meta 和 Transfer 必须通过该 ID 获取同 Tenant 可见的 Runtime Descriptor，校验实例处于 active 状态、声明 `addp.workflow/v1`，并通过同一实例的 `/api/operators` 校验所需 direct 算子。官方 `supermap_workflow` 和用户独立部署的兼容 Runtime 在此路径上待遇一致；任何调用方不得用 `engine_type=supermap_workflow` 代替绑定关系和算子能力校验。

`datasource.upgrade_udbx` 对已有 UDBX 执行原位 schema 升级：运行时先以 SQLite 只读检查 SuperMap 系统表和 `SmRegister` 关键字段，再以可写方式打开数据源，由当前 iObjects C++ SDK 完成官方 schema 迁移，关闭后重新检查并返回 `changed`、`schema_current` 和 `dataset_count`。该算子必须保持幂等；调用方负责权限确认、升级前备份和审计，不得在普通读取链路中隐式触发。ADDP `locator` 只属于 Develop/UI 的资源选择契约，调用方在调用 runtime 前必须把它派生为 `connection_info`、`schema` 和 `table` 并移除，SuperMap runtime 不解析 ADDP locator。完整 SuperMap C++ SDK 作为外部只读母版保存，不进入 ADDP 代码仓库或最终运行镜像；基础镜像从显式指定的母版构建。许可作为受控制品单独注入。运行时未绑定 SDK runtime 或许可时 `/health` 应返回不可用依赖，System 连接测试必须失败并提示具体缺失项。

单算子调用由 Common Engine 的 `WorkflowRuntimeProvider.InvokeOperator()` 统一调用，只允许调用 `execution_modes` 包含 `direct` 的算子。`InvokeOperator()` 是模块受控能力调用，不是任务执行入口；它不创建 Develop 任务，不进入 Orchestrator，也不进入 Monitor 通用执行监控。调用方模块必须持有明确业务目的并管理自身领域状态，例如 Manager 触发 `raster_to_cog` 后负责记录 COG 生成结果状态、源 item fingerprint、目标 `storage_ref` 和失败原因。凡是需要任务编排、调度、重试、跨模块依赖或统一监控的执行，必须建模为工作流任务并走 `ExecuteWorkflow()`。

业务模块需要 direct 算子能力时，不得按内置 `engine_type` 硬编码查找运行时，例如不得要求 `geopython_workflow` 必须存在。调用方应声明自己需要的算子名和调用模式，例如 `operator=raster_to_cog`、`execution_mode=direct`；Common Engine 或调用方模块应在当前租户可见的已启用 workflow 引擎中，通过 `ListOperators()` 查找实际提供该 direct 算子的运行时实例。有可用运行时则调用；没有任何运行时提供该算子时，该业务功能应进入“能力暂不可用”状态或产生明确失败原因，而不是回退到私有 HTTP、单节点 workflow 或固定内置引擎假设。

调用方模块在 direct 调用前必须通过同一个工作流运行时实例的 `ListOperators()` / `GET /api/operators` 获取算子元数据，并确认目标算子的 `execution_modes` 显式包含 `direct`；不得只依赖运行时 `/api/operators/{name}/invoke` 返回 403 作为权限判断。运行时仍必须保留 `DIRECT_NOT_SUPPORTED` 防线，调用方前置校验和运行时校验共同构成单一路径的能力边界。

运行时本地执行状态查询由 Common Engine 的 `WorkflowRuntimeProvider.GetExecutionStatus()` 统一调用，对应 `GET /api/executions/{execution_id}`。Develop、Orchestrator 或其他模块如果需要轮询工作流运行时本地状态，必须通过 Provider / dbbridge 主路径，不得自行拼接运行时私有 HTTP URL。这里的 `execution_id` 是工作流运行时返回的本地执行 ID，不是 `common.task_executions.execution_id`；进入 ADDP 任务体系后的统一执行状态仍以 `common.task_executions` 为准。

Develop 执行工作流时必须把工作流运行时的本地状态收敛为 ADDP 统一执行状态：`success` 才能写入统一成功态；`failed` / `error` / `cancelled` 必须写入统一失败或取消语义；`running` / `pending` 只能作为中间态轮询，不得直接结束统一 execution。运行时状态摘要可以保存到 execution metadata 的 `runtime_status`，但该字段只用于诊断，不作为 Monitor 或 Orchestrator 的主状态源。

业务模块使用 direct 调用时必须在本模块领域表或执行记录中保存最小审计信息，而不是把运行时响应作为唯一事实源。至少包括：

- **调用目的**：模块名、领域动作或任务类型，例如 Manager 的 `raster_cog_generation`。
- **运行时身份**：工作流运行时实例 ID、实例名称、`engine_type`，以及运行时返回的 `execution_id`（若有）。
- **算子身份**：算子 `id/name`、调用模式 `direct`、实际使用的参数策略或关键配置摘要。
- **资源身份**：源资源 locator / item fingerprint / source engine，目标 `storage_ref` 或等价领域引用；不得只记录运行时临时 URI。
- **领域状态**：调用前后的领域对象状态、产物状态、大小/空间事实等可供前端和后续任务消费的结果摘要。
- **失败信息**：错误码或错误消息、失败阶段、是否可重试；失败不能只留在运行时日志中。

以 Manager COG 生成结果为例，Manager 通过 `InvokeOperator("raster_to_cog")` 调用实际提供该 direct 算子的工作流运行时，但 COG 是否可用以 `manager.raster_cog.status` 为准；`raster_cog` 记录中应保留源 item fingerprint、源 locator、目标 `storage_ref`、`workflow_runtime.engine_id/name/engine_type/execution_id/operator/mode`、栅格 facts 与失败原因。工作流运行时只负责执行 GDAL 转换，不负责登记 Manager 的 COG 结果生命周期。

## 扩展新引擎指南

### Python Runtime 接入步骤

#### 1. 定义领域算子和元数据

在 `engines/` 下建立 Runtime 目录，领域执行器只负责实际计算。算子描述遵循[算子元数据与实现规范](算子元数据与实现.md)，显式声明 `engine_type`、`execution_modes`、`effects`、参数与输出端口；可参考 [Math 算子实现](../../../engines/math-workflow/operators/)。

公开资源选择与运行时执行参数按 [Develop 资源与执行契约](Develop资源与执行.md)分层：Develop 负责把 locator 派生为运行时参数或访问计划，Runtime 消费派生结果。执行请求中的租户上下文与执行授权由拥有执行入口的后端从可信上下文注入。

#### 2. 复用公共执行核心

Python Runtime 使用 [common-python 工作流核心](../../../common-python/addp_common/workflow_runtime/)，接入职责如下：

| 接入点 | 职责 |
| --- | --- |
| `validate_workflow_def` | 校验任务结构、算子范围、依赖与引用关系 |
| `validate_execution_authorization` | 根据本次 DAG 的算子 `effects` 校验租户上下文和执行授权覆盖范围 |
| `WorkflowRunner` | 执行公共 DAG、引用解析与结果端口适配；Runtime 注入领域算子执行器 |
| `ExecutionRegistry` | 记录运行时本地执行快照，支持异步提交和状态查询 |

可对照 [Math API 服务](../../../engines/math-workflow/api_server.py)查看上述调用关系。`ExecutionRegistry` 实例由服务进程共享，执行 ID 对应可查询的真实快照；跨进程或分布式执行的状态后端要求见[常见问题](#常见问题)。

#### 3. 完整实现 HTTP 协议

按 [HTTP 协议](HTTP协议.md)实现健康检查、算子列表、工作流执行、单算子调用和执行状态查询五个接口，统一响应与错误码。工作流入口在执行前完成定义与授权校验；单算子入口校验 `direct` 能力。同步、异步响应及未知执行 ID 的行为均以该协议为准。

Math 用于学习协议与公共核心；生产服务的 HTTP 进程组织可参考 [GeoPython](../../../engines/geopython-workflow/api_server.py) 和 [Spark](../../../engines/spark-workflow/runtime_server.py) 的 Gunicorn 入口。两者当前采用单 worker、四请求线程；进程内执行快照的共享范围随 worker 生命周期确定。

#### 4. 接入标准生命周期和注册

开发启动、局部与全量重启、停止、依赖准备、端口归属及构建身份统一遵循[开发服务生命周期与构建身份规范](../addp开发服务生命周期与构建身份规范.md)；端口按[端口分配规范](../addp端口分配.md)登记。新增 Runtime 时同步核对标准脚本的显式选择与依赖关系，以及产品镜像构建登记。

注册方式遵循[引擎注册](#引擎注册)：生产内置 Runtime 先完成自身 HTTP readiness，再异步注册到 System；参考实现或扩展 Runtime 可由用户手动注册。实例的稳定地址、能力声明和协议探测要求相同。

#### 5. 验证与交付

按[测试与验收规范](../addp测试与验收规范.md)为新增 Runtime 登记标准测试入口与 CI 覆盖，并分别记录确定性测试、依赖验证和真实运行验收结果：

- **算子契约**：将实际 `GET /api/operators` 响应或算子数组交给 [workflow_operator_contract.py](../../../engines/docs/workflow_operator_contract.py)，使用 `--engine-type` 指定实际类型。
- **执行与状态**：通过本 Runtime 的标准门禁验证 DAG、引用、授权、`direct` 边界、同步或异步响应和状态查询；Math 示例的现有入口为 `make test-math-workflow`。
- **生命周期与登记**：涉及启动、构建或 CI 登记时，按影响运行 `make test-dev-lifecycle`、`make test-engine-startup-isolation`、`make test-build-registration` 和 `make test-python-ci-registration`；公共 Python 核心有改动时运行 `make test-common-python`。
- **运行验收**：使用标准生命周期准备环境，通过实际调用模块验证执行与产物；进程内 API 测试不能替代真实引擎、外部依赖或分布式验收。

### 关键要点

1. **遵循标准接口**: 确保实现 5 个标准 API 接口
2. **算子元数据完整**: 提供完整的必填字段、参数、执行模式、effects 和输出端口，并通过 `engines/docs/workflow_operator_contract.py` 校验
3. **错误处理**: 使用标准错误码，提供清晰的错误信息
4. **注册方式明确**: 生产内置运行时可启动自注册；参考实现和用户自研运行时可在 System 引擎管理中手动注册
5. **文档完善**: 编写 `README.md`；API 文档以 OpenAPI/Swagger 或本规范为准，不维护手写 API 清单

### 常见问题

**Q1: 如何支持分布式计算？**
- DAG 校验、依赖解析与状态管理继续复用 `common-python/addp_common/workflow_runtime`；Spark 或 Dask 只作为算子执行器或 Runtime 私有调度后端接入，不复制公共 DAG 核心。

**Q2: 如何处理大文件输入？**
- 使用标准对象存储 `ResourceLocator`，例如 `locator=addp://engine/4/path/bucket/path/data.parquet?type=object&item_id=46`；Develop Backend 会在执行前派生运行时所需的 `connection_info` 和 Spark 可读 `s3a://bucket/path/data.parquet`。

**Q3: 如何支持异步执行？**
- `/api/executions/<id>` 是必需接口。单进程 Python Runtime 使用公共 `ExecutionRegistry`；需要跨进程或分布式执行时，可以替换其状态持久化和调度后端，但对外状态契约保持一致。未知 ID 必须返回 `EXECUTION_NOT_FOUND`。

Spark Workflow 保持同步 `/api/workflow`，本地标准生命周期使用原生 Python/JDK，产品验收独立运行镜像；两者均以 `python api_server.py` 启动 Gunicorn，一个 HTTP worker、四个请求线程共享 `ExecutionRegistry`。HTTP worker 数量不限制远端 Spark Executor 数量。Runtime 先通过自身 HTTP 健康检查，再在后台使用公共注册客户端持续退避注册到 System；System 未就绪不阻塞 HTTP readiness。注册地址使用稳定 `RUNTIME_HOST` 和公共 advertised-port 规则。执行快照只在该 worker 生命周期内有效，平台业务执行历史仍由 `common.task_executions` 持有，不新增状态数据库。

Spark 只提供领域算子执行器和已声明输出端口适配；DAG 校验、排序、嵌套引用与 `$input` 参数统一由公共 `WorkflowRunner` 执行。单端口节点的 `all_results[task_id]` 直接返回结果，多端口返回端口对象；DataFrame 对外仅返回轻量摘要。容器默认根据选择的 Spark general Engine 连接真实集群，不强制 `SPARK_MODE=local`。镜像构建中的显式本地计算仅验证依赖，不作为分布式验收。

---

## 附录

### 参考实现

- **Math Workflow**: `engines/math-workflow/` - 简单示例，适合学习
- **GeoPython Workflow**: `engines/geopython-workflow/` - 生产级 Python 实现，包含 Pandas / GeoPandas 等运行库
- **Spark Workflow**: `engines/spark-workflow/` - 分布式计算示例
- **Model3D Workflow**: `engines/model3d-workflow/` - 三维模型和 BIM 快显转换运行时
- **PointCloud Workflow**: `engines/pointcloud-workflow/` - 点云 COPC 快显转换运行时
- **SuperMap Workflow**: `engines/supermap-workflow/` - 超图 iObjects C++ 工作流运行时
