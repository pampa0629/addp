# GeoPython Workflow 运行说明

GeoPython Workflow 的 `engine_type` 为 `geopython_workflow`，通过 Common Engine 的 `WorkflowRuntimeProvider` 提供 `addp.workflow/v1` 能力。业务模块按[工作流协议](../../../docs/spec/addp工作流计算引擎接口规范.md)调用。

## 本地开发

当前开发入口使用不继承系统包的独立 Python 3.12 虚拟环境，安装完整 requirements 与 editable `common-python`。[GDAL Python 绑定](https://gdal.org/en/stable/api/python/python_bindings.html)必须与原生库版本匹配，不能直接加载 Homebrew 为另一 Python 主次版本编译的扩展。PGeo 使用 MDBTools 和 unixODBC，驱动及 Access 驱动名按 [PGeo 官方配置要求](https://gdal.org/en/stable/drivers/vector/pgeo.html)配置。完整依赖与隔离要求见[生命周期规范](../../../docs/spec/addp开发服务生命周期与构建身份规范.md)。

macOS 安装示例为 `brew install python@3.12 gdal mdbtools pkg-config`。GDAL、PROJ 与 pkg-config 固定选择 Homebrew 来源，GDAL 绑定编译也使用该工具链，不受 Conda base 的 PATH 和资源变量影响。GDAL/PROJ 资源目录从所选原生依赖派生，只注入该子进程；隔离终端继承的 `GDAL_DATA`、`GDAL_DRIVER_PATH`、`PROJ_DATA` 与 `PROJ_LIB`，保持离线坐标处理。PGeo 的 ODBC 配置限于 `.dev-state/geopython-odbc`，不改宿主机其他应用的配置。原生访问回环数据源时不注入容器专用的 `GEOPYTHON_WORKFLOW_LOOPBACK_HOST`。

标准入口创建独立 `engines/geopython-workflow/venv` 并同步完整依赖。GDAL 绑定可加载且与 `gdal-config --version` 一致时复用；失效时通过既有安装锁、禁用缓存，从源码重建匹配版本的绑定，不升级其他依赖。资源派生与绑定修复统一由 [gdal-env.sh](../../../scripts/dev/gdal-env.sh) 实现。

从仓库根目录使用标准启动入口：

```bash
bash scripts/dev/start.sh -geopython-workflow
```

需要重启时，由开发者在自己的终端执行：

```bash
./scripts/dev/restart.sh -geopython-workflow
```

start/restart 复用 [geopython-workflow.sh](../../../scripts/dev/geopython-workflow.sh)，同步依赖并核对绑定版本、依赖一致性、PGeo、OpenFileGDB、GTiff/COG/HFA/JP2OpenJPEG 驱动、`EPSG:4326` 和端口归属。检查失败则终止准备、不启动该服务；重启先停止所选服务再执行准备。默认端口为 `8099`，实际地址以标准生命周期输出及 `.dev-state/ports.env` 为准；外部监听者占用开发端口时拒绝启动，不自动接管容器或外部进程。日志为 `logs/geopython-workflow-engine.log`。

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

确定性回归使用 `make test-geopython-workflow`；产品栅格链路使用已登记的 `raster-workflow` Hosted T4。两者的覆盖边界与标准入口见[引擎开发指南](../../README.md#geopython-原生开发)，测试分层见[测试与验收规范](../../../docs/spec/addp测试与验收规范.md)。
