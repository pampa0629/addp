# Jupyter Engine

ADDP 的 Notebook 运行时。Develop 通过统一任务执行链调用 8097 API，运行时使用 Papermill 执行保存在 MinIO 中的 Notebook；交互编辑由 Develop 创建并代理短期隔离 JupyterLab 会话，浏览器不直连 Runtime。

## 架构设计

```
Develop TaskExecution
  -> Notebook Runtime API (8097)
     -> MinIO 输入 Notebook
     -> Papermill 无头执行
     -> MinIO 执行结果

Develop Notebook Session
  -> Runtime 控制面 (8097，Service Access Token)
     -> 隔离 JupyterLab process + 临时 workspace
     -> Develop 同源 HTTP/WebSocket 代理
     -> 关闭或过期时保存回 MinIO 并清理
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

### 4. 交互会话控制面

`POST /api/interactive-sessions` 和 `DELETE /api/interactive-sessions/{id}` 只接受 `addp-develop` 的 Tenant Service Access Token。创建请求必须携带 Tenant、User、Task、Notebook 路径、Kernel、外部 base path、TTL，以及 Develop 为当前 Session 签发的 owner API endpoint 和 Notebook Kernel Capability Token；响应中的内部端口与 Jupyter Token 只供 Develop 代理使用，不得返回浏览器。

每个会话使用独立临时目录和 JupyterLab process。Runtime 只把 owner endpoint 和对应短期 Capability 注入该 process，供 `addp_common.notebook.engines.list()` 读取脱敏查询 Engine 描述；公开会话响应、Notebook 内容和日志均不保存 Capability。正常关闭与 TTL 清理都会先把 Notebook 保存回原 MinIO 对象，再终止 process；Runtime 退出时统一清理全部会话。共享 Lab、固定 8088 入口、URL Token、用户 Token、服务 Token 和长期 Engine 连接注入均不属于支持路径。

镜像内预装 `@addp/notebook-bridge` JupyterLab 扩展。它只接受同源父页面、当前 Session ID 匹配的 `postMessage`，在当前 Notebook 中插入一个代码单元并保持未执行状态；不提供执行、文件覆盖或跨 Session 操作。

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
- `JUPYTER_GUNICORN_THREADS`: Runtime 控制面的线程数（默认 8；会话状态由单个 Gunicorn worker 持有）
- `JUPYTER_GUNICORN_TIMEOUT`: 单次 Notebook 执行超时秒数（默认 7200）
- `JUPYTER_SESSION_TTL_SECONDS`: 交互会话最大 TTL（默认 3600）
- `MINIO_ENDPOINT`、`MINIO_ROOT_USER`、`MINIO_ROOT_PASSWORD`: Notebook 存储连接

## 依赖库

详见 requirements.txt
