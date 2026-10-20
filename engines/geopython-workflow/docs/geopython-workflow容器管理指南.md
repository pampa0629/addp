# GeoPython Workflow 运行说明

GeoPython Workflow 的 `engine_type` 为 `geopython_workflow`，通过 Common Engine 的 `WorkflowRuntimeProvider` 提供 `addp.workflow/v1` 能力。业务模块按[工作流协议](../../../docs/spec/addp工作流计算引擎接口规范.md)调用。

## 本地开发

当前开发入口使用独立 Python 3.12 虚拟环境与原生 GDAL。GDAL Python 绑定与原生库版本必须一致；PGeo 使用 MDBTools 和 unixODBC，配置限于 Runtime 自身。完整依赖与隔离要求见[生命周期规范](../../../docs/spec/addp开发服务生命周期与构建身份规范.md)。

从仓库根目录使用标准启动入口：

```bash
bash scripts/dev/start.sh -geopython-workflow
```

需要重启时，由开发者在自己的终端执行：

```bash
./scripts/dev/restart.sh -geopython-workflow
```

start/restart 复用 [geopython-workflow.sh](../../../scripts/dev/geopython-workflow.sh)，同步依赖并核对 PGeo、OpenFileGDB、GTiff/COG 驱动、坐标系和端口归属。默认端口为 `8099`，实际地址以标准生命周期输出及 `.dev-state/ports.env` 为准；依赖、停止与日志入口见[引擎开发指南](../../README.md#本地开发运行方式)。

## 产品镜像

产品部署和 Hosted 产品验收使用镜像。服务在 [docker-compose.runtimes.yml](../../../docker-compose.runtimes.yml) 中登记为 `geopython-workflow-engine`，使用仓库根目录作为构建上下文，以便打包 `common-python`。

从仓库根目录构建：

```bash
make build-images IMAGE_BUILD_ARGS="--services geopython-workflow-engine --verify --jobs 1"
```

网络与挂载按 Runtime Compose 和[标准部署步骤](../../../docs/guide/addp部署和开发步骤.md)配置。构建参数见[镜像构建说明](../../../scripts/build/README.md#build-imagessh)。

## 接口与注册

五个标准接口、执行状态与错误码统一见 [HTTP 协议](../../../docs/spec/workflow/HTTP协议.md)，算子分类、参数与端口见[算子目录](../operators/README.md)。

Runtime 按[注册契约](../../../docs/spec/workflow/Runtime注册与扩展.md#引擎注册)提交完整 `engine.capabilities/v1`，System 校验并保存声明；算子元数据通过 `GET /api/operators` 动态发现。认证和连接身份要求以注册契约为准。

## 验证

确定性回归使用 `make test-geopython-workflow`；产品栅格链路使用已登记的 `raster-workflow` Hosted T4。两者的覆盖边界与标准入口见[引擎开发指南](../../README.md)，测试分层见[测试与验收规范](../../../docs/spec/addp测试与验收规范.md)。
