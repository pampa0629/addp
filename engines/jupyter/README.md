# Jupyter Engine

ADDP 的无头 Notebook 执行运行时。Develop 通过统一任务执行链调用 8097 API，运行时使用 Papermill 执行保存在 MinIO 中的 Notebook；不向浏览器暴露 Jupyter Lab。

## 架构设计

```
Develop TaskExecution
  -> Notebook Runtime API (8097)
     -> MinIO 输入 Notebook
     -> Papermill 无头执行
     -> MinIO 执行结果
```

## API 接口

### 1. 健康检查
```bash
GET http://jupyter-engine:8097/health
```

### 2. 执行 Notebook
```bash
POST http://jupyter-engine:8097/api/execute
Content-Type: application/json

{
  "tenant_id": 1,
  "notebook_path": "analysis.ipynb",
  "parameters": {
    "param1": "value1",
    "param2": 123
  },
  "kernel": "python3"
}
```

响应：
```json
{
  "status": "success",
  "execution_time_seconds": 1.234,
  "output_path": "tenant_1/executions/analysis_exec_20260729_120000.ipynb",
  "output_count": 5,
  "outputs": [...]
}
```

### 3. 列出可用 Kernel
```bash
GET http://jupyter-engine:8097/api/kernels
```

## 本地开发

```bash
# 在仓库根目录构建镜像
docker build -f engines/jupyter/Dockerfile -t addp-jupyter-engine .

# 运行容器
docker run -p 8097:8097 \
  addp-jupyter-engine
```

## 环境变量

- `API_PORT`: Notebook Runtime API 端口（默认 8097）
- `JUPYTER_GUNICORN_WORKERS`: Gunicorn worker 数（默认 2）
- `JUPYTER_GUNICORN_TIMEOUT`: 单次 Notebook 执行超时秒数（默认 7200）
- `MINIO_ENDPOINT`、`MINIO_ROOT_USER`、`MINIO_ROOT_PASSWORD`: Notebook 存储连接

## 依赖库

详见 requirements.txt
