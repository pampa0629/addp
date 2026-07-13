# Engines 计算引擎目录说明

## 模块定位

`engines/` 集中管理 ADDP 计算与 Notebook 运行时。GeoPython Workflow、Spark Workflow 和 Jupyter 是默认部署的内置运行时；Math Workflow 是 `addp.workflow/v1` 参考实现，用于示范扩展引擎规范；Model3D Workflow 是三维模型转换专用运行时；PointCloud Workflow 是点云处理专用运行时；SuperMap Workflow 是面向超图 iObjects Java / SPS 的工作流运行时。Develop 和业务模块通过引擎能力声明和 HTTP API 发现算子、执行工作流或 Notebook。

## 重要目录与端口

```text
engines/
├── python-workflow/  # 空间工作流引擎，默认端口 8099
├── spark-workflow/   # Spark 工作流引擎，默认端口 8098
├── math-workflow/    # 数学工作流参考实现，默认端口 8089，开发环境自动启动服务但需手动注册
├── model3d-workflow/ # 三维模型转换运行时，默认端口 8101，开发环境自动启动并自注册，需配置 MODEL3D_CONVERTER_BIN 指向可执行文件路径
├── pointcloud-workflow/ # 点云处理运行时，默认端口 8102；绑定 engine runtime 内部 PDAL 后自注册，POINTCLOUD_PDAL_BIN 不指向宿主机全局命令
├── supermap-workflow/ # 超图 SPS 工作流运行时，默认端口 8103；通过 Docker 绑定 SuperMap Java SDK 和许可，不提交 SDK 到仓库
├── jupyter/          # Jupyter API 8097，Lab 8088
└── docs/             # 引擎 API 与设计文档
```

端口以 `.env` 和 `scripts/dev/start.sh` 为准：`PYTHON_WORKFLOW_PORT`、`SPARK_WORKFLOW_PORT`、`MATH_WORKFLOW_PORT`、`MODEL3D_WORKFLOW_PORT`、`POINTCLOUD_WORKFLOW_PORT`、`SUPERMAP_WORKFLOW_PORT`、`JUPYTER_API_PORT`、`JUPYTER_LAB_PORT`。

## 开发规则

- 新增或修改引擎接口前，阅读 `docs/spec/addp工作流计算引擎接口规范.md`、`docs/spec/addp引擎插件接口规范.md` 和 `docs/spec/addp引擎能力声明规范.md`。
- 引擎应提供健康检查、算子发现、执行接口；生产内置运行时自注册时只提交身份与连接信息，能力声明由 common engine 插件的 `Capabilities()` 生成。参考实现可以随开发环境自动启动服务但不自注册，由用户在 System 中按扩展引擎手动注册。
- 算子元数据要包含输入、输出、参数、示例和开发模式，保证 Develop 工作流画布可动态消费。
- `pointcloud-workflow` 的 PDAL 属于该 engine runtime 内部依赖；不得要求 Manager、System 或宿主机全局安装 PDAL。未绑定 PDAL 时健康检查应保持 `degraded`，且不自注册。
- `supermap-workflow` 的 SuperMap iObjects Java / SPS 属于该 engine runtime 内部依赖；通过 Docker 镜像或只读 volume 绑定，不提交 SDK、native `.so` 或许可文件到仓库。
- SuperMap Workflow 本地开发固定使用两层镜像：Git 忽略的 `engines/supermap-workflow/vendor/` 只用于构建稳定基础镜像，ADDP Java 源码只进入代码镜像。`restart.sh -supermap-workflow` 和 `restart.sh -all` 必须每次重新执行代码镜像的 `javac` 并替换容器；不得恢复胖/瘦镜像分支、源码挂载、运行时编译或可选 rebuild 开关。
- 引擎目录中不要沉淀一次性实验脚本；临时验证放到操作系统临时目录。

## 启动与验证

```bash
bash scripts/dev/start.sh -python-workflow
bash scripts/dev/start.sh -spark-workflow
bash scripts/dev/start.sh -math-workflow
bash scripts/dev/start.sh -model3d-workflow
bash scripts/dev/start.sh -pointcloud-workflow
bash scripts/dev/start.sh -supermap-workflow
bash scripts/dev/start.sh -jupyter
```

常用健康检查：

```bash
curl http://localhost:8099/health
curl http://localhost:8098/health
curl http://localhost:8089/health
curl http://localhost:8101/health
curl http://localhost:8102/health
curl http://localhost:8103/health
curl http://localhost:8097/health
```

## 相关文档

- `engines/README.md`
- `engines/docs/README.md`
- `engines/docs/workflow-engine-api-v1.yaml`
- `develop/CLAUDE.md`
- `docs/spec/addp工作流计算引擎接口规范.md`
