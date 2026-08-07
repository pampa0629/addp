# ADDP 计算引擎开发指南

本目录集中管理 ADDP 计算和 Notebook 运行时。GeoPython Workflow、Spark Workflow、DuckDB、Jupyter 是默认部署的内置运行时；Math Workflow 是 `addp.workflow/v1` 参考实现，用于示范扩展引擎规范；Model3D Workflow 是三维模型转换专用运行时；PointCloud Workflow 是点云处理运行时；SuperMap Workflow 是面向超图 iObjects C++ 的空间计算运行时。

## 目录结构

```
engines/
├── python-workflow/    # GeoPython Workflow 空间与数据处理工作流引擎，默认端口 8099
├── spark-workflow/     # Spark Workflow 分布式工作流引擎，默认端口 8098
├── math-workflow/      # Math Workflow 数学工作流参考实现，默认端口 8089
├── model3d-workflow/   # Model3D Workflow 三维模型转换运行时，默认端口 8101
├── pointcloud-workflow/ # PointCloud Workflow 点云处理运行时，默认端口 8102
├── supermap-workflow/  # SuperMap Workflow 超图 iObjects C++ 工作流运行时，默认端口 8103
├── jupyter/            # 无头 Notebook 执行运行时，API 默认端口 8097
├── duckdb/             # DuckDB 联邦查询运行时，API 默认端口 8104
└── docs/               # 引擎 API 与设计文档
```

## 引擎分类

### 工作流运行时

通过 `EnginePlugin + WorkflowRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.workflow.supported=true`。

**已有引擎**：
- `geopython_workflow` - GeoPython Workflow 引擎，适合中小规模空间与数据处理。
- `spark_workflow` - Spark Workflow 引擎，适合分布式计算。
- `math_workflow` - Math Workflow 参考实现，开发环境可自动启动服务但不会自动注册；需要使用时在 System 引擎管理中按扩展引擎手动注册。
- `model3d_workflow` - Model3D Workflow 三维模型转换运行时，提供 `osgb_to_glb` 和 `osgb_scene_to_3dtiles` direct 算子；开发环境启动时会自注册到 System，实际转换需通过 `MODEL3D_CONVERTER_BIN` 配置引擎部署内的 `_3dtile` 或等价转换器可执行文件路径。
- `pointcloud_workflow` - PointCloud Workflow 点云处理运行时，提供 `las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 和 `xyz_to_copc` direct 算子；绑定 engine runtime 内部 PDAL 后会自注册到 System，实际转换通过 `POINTCLOUD_PDAL_BIN` 指向引擎部署内的 PDAL 可执行文件路径，不依赖宿主机全局 PDAL。Manager 负责派生源 URI 和 Manager infra MinIO 发布计划，运行时通过 PDAL 读取源 URI，先写入受控工作目录，再发布为 Manager 私有 COPC artifact。
- `supermap_workflow` - SuperMap Workflow 超图工作流运行时，对外实现 `addp.workflow/v1`，对内绑定 SuperMap iObjects C++ SDK；运行时独立校验并按稳定拓扑顺序执行 DAG，同一 C++ 执行上下文内通过类型化共享句柄传递 Datasource、Dataset 等对象。完整 SDK 作为仓库外只读母版保存，最终运行镜像只包含已验证的运行期文件，许可作为受控制品单独注入。

### 脚本 / Notebook 运行时

通过 `EnginePlugin + ScriptRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.script.supported=true`。

**已有引擎**：
- `jupyter` - 仅由 Develop 通过租户 Service Access Token 调用的无头 Notebook 运行时，不暴露共享 Lab。

### 联邦查询运行时

通过 `EnginePlugin + FederatedQueryRuntimeProvider` 纳入统一引擎体系，能力声明为
`compute.query.federation.supported=true`。

**已有引擎**：
- `duckdb` - 由 Develop 和 Service 通过租户 Service Access Token 调用的内置联邦查询 Runtime；执行授权限定可挂载的源 Engine，支持 PostgreSQL、MySQL、MinIO 和 S3。

## 工作流运行时 HTTP 协议

工作流引擎对外实现 `addp.workflow/v1` HTTP 协议，业务模块不直接拼接这些 URL，而是通过 common engine 的 `WorkflowRuntimeProvider` 调用。

### 1. 算子发现
```
GET /api/operators
```

返回格式:
```json
{
  "status": "success",
  "operators": [
    {
      "id": "buffer",
      "name": "buffer",
      "display_name": "缓冲区分析",
      "engine_type": "geopython_workflow",
      "category": "空间分析",
      "category_path": ["空间分析"],
      "description": "对几何对象生成缓冲区",
      "execution_modes": ["workflow"],
      "parameters": [
        {
          "name": "distance",
          "type": "float",
          "required": true,
          "description": "缓冲区距离",
          "min": 0
        }
      ],
      "output_ports": [
        {
          "name": "default",
          "type": "geodataframe",
          "is_default": true,
          "description": "缓冲区分析结果"
        }
      ]
    }
  ],
  "count": 1
}
```

### 2. 工作流执行
```
POST /api/workflow
```

请求格式:
```json
{
  "workflow_def": {
    "tasks": []
  },
  "input_data": {}
}
```

响应格式:
```json
{
  "status": "success",
  "execution_id": "uuid",
  "final_result": {},
  "all_results": {}
}
```

### 3. 单算子 direct 调用
```
POST /api/operators/{name}/invoke
```

该接口只允许调用 `execution_modes` 包含 `direct` 的算子，用于业务模块受控调用单个算子。它不创建 Develop/Orchestrator/Monitor 任务；凡是需要调度、重试、跨模块编排或统一监控的执行，必须走工作流。

### 4. 健康检查
```
GET /health
```

## 能力声明

引擎能力统一使用 `engine.capabilities/v1` 结构。Common engine 插件由 `Capabilities()` 声明能力模板；非插件内置 Workflow Runtime 在启动注册时提交与 `engine_type` 一致的标准能力声明。

能力只表达引擎自身 native / provider 能力，例如 `compute.workflow`、`compute.script`、`storage.catalog`、`storage.store`。不要在引擎能力中维护 Transfer、Preview、Develop 等模块对引擎的适配列表。

工作流引擎的算子列表、参数、输出端口等动态能力不写入 `capabilities`，通过 `WorkflowRuntimeProvider.ListOperators()` 实时发现。

## 引擎注册

工作流运行时必须先注册到 System 资源中心，才会成为 ADDP 可发现和可调用的引擎实例。生产内置运行时可以在启动时自注册；参考实现和用户自研扩展运行时可以在 System 引擎管理中手动注册。

**生产内置运行时自注册端点**: `POST http://system-backend:8180/api/v1/system/runtime/engines`

Runtime 使用自身 Confidential OAuth Client 获取 Platform Service Access Token，并只发送 `Authorization: Bearer <platform_service_access_token>`。

**注册数据格式**:
```json
{
  "engine_type": "geopython_workflow",
  "name": "GeoPython 工作流引擎",
  "description": "基于 Python 的工作流执行引擎",
  "connection_info": {
    "protocol": "http",
    "port": 8099
  },
  "is_builtin": true
}
```

Math Workflow 是参考实现，随 `scripts/dev/start.sh -all` / `-develop` 启动服务，但不会自动注册。需要使用时，在 System 引擎管理中使用扩展引擎注册表单填入示例值、测试连接并保存。

## 新增引擎checklist

创建新引擎时,请遵循以下步骤:

- [ ] 在`engines/`目录下创建引擎目录
- [ ] 实现 `addp.workflow/v1` HTTP 协议（`/health`、`/api/operators`、`/api/workflow`、`/api/operators/{name}/invoke`、`/api/executions/{id}`）
- [ ] 在 common engine 插件中声明 `engine.capabilities/v1` 能力
- [ ] 决定注册方式：生产运行时可配置启动自注册；参考实现可通过 System 引擎管理手动注册
- [ ] 添加到 `scripts/dev/start.sh`
- [ ] 如需容器化部署，添加到 `docker-compose.yml`、`scripts/build/build-images.sh`、`scripts/local/start.sh` 和 `scripts/prod/start.sh`
- [ ] 编写 README 说明引擎功能和使用方法

## 参考实现

- **Math Workflow Engine**: [math-workflow](./math-workflow/) - 最小工作流参考实现，手动注册示例。
- **GeoPython Workflow Engine**: [python-workflow](./python-workflow/) - Python 数据处理工作流实现。
- **Spark Workflow Engine**: [spark-workflow](./spark-workflow/) - Spark / Sedona 工作流实现。
- **Model3D Workflow Engine**: [model3d-workflow](./model3d-workflow/) - OSGB 快显和 OSGB Scene 转 3D Tiles 三维模型转换运行时。
- **PointCloud Workflow Engine**: [pointcloud-workflow](./pointcloud-workflow/) - LAS / LAZ / E57 / PCD / XYZ 转 COPC 点云快显转换运行时。
- **SuperMap Workflow Engine**: [supermap-workflow](./supermap-workflow/) - 超图 iObjects C++ 空间计算工作流运行时。

- **DuckDB Federated Query Runtime**: [duckdb](./duckdb/) - Develop 与 Service 共用的联邦查询运行时。

## 相关文档

- [ADDP架构设计方案](/Users/pampa/.claude/plans/buzzing-bubbling-porcupine.md)
- [Common模块文档](../common/README.md)
- [System模块文档](../system/CLAUDE.md)
