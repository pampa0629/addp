# Engines 计算引擎目录说明

## 模块定位

`engines/` 集中管理 ADDP 内置计算与 Notebook 引擎，包括 Python Workflow、Spark Workflow、Math Workflow 和 Jupyter。Develop 通过引擎能力声明和 HTTP API 发现算子、执行工作流或 Notebook。

## 重要目录与端口

```text
engines/
├── python-workflow/  # 空间工作流引擎，默认端口 8099
├── spark-workflow/   # Spark 工作流引擎，默认端口 8098
├── math-workflow/    # 数学工作流引擎，默认端口 8089
├── jupyter/          # Jupyter API 8097，Lab 8088
└── docs/             # 引擎 API 与设计文档
```

端口以 `.env` 和 `scripts/dev/start.sh` 为准：`PYTHON_WORKFLOW_PORT`、`SPARK_WORKFLOW_PORT`、`MATH_WORKFLOW_PORT`、`JUPYTER_API_PORT`、`JUPYTER_LAB_PORT`。

## 开发规则

- 新增或修改引擎接口前，阅读 `docs/spec/addp工作流计算引擎接口规范.md`、`docs/spec/addp引擎插件接口规范.md` 和 `docs/spec/addp引擎能力声明规范.md`。
- 引擎应提供健康检查、算子发现、执行接口，并向 System 注册 capabilities。
- 算子元数据要包含输入、输出、参数、示例和开发模式，保证 Develop 工作流画布可动态消费。
- 引擎目录中不要沉淀一次性实验脚本；临时验证放到操作系统临时目录。

## 启动与验证

```bash
bash scripts/dev/start.sh -python-workflow
bash scripts/dev/start.sh -spark-workflow
bash scripts/dev/start.sh -math-workflow
bash scripts/dev/start.sh -jupyter
```

常用健康检查：

```bash
curl http://localhost:8099/health
curl http://localhost:8098/health
curl http://localhost:8089/health
curl http://localhost:8097/health
```

## 相关文档

- `engines/README.md`
- `engines/docs/README.md`
- `engines/docs/workflow-engine-api-v1.yaml`
- `develop/CLAUDE.md`
- `docs/spec/addp工作流计算引擎接口规范.md`
