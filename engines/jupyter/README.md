# Jupyter Engine

ADDP 平台的 Jupyter Notebook 引擎，提供：
- Jupyter Lab UI（端口 8088）- 用户交互式开发
- Flask API Server（端口 8097）- 后端批量执行接口

## 架构设计

```
Jupyter Engine (容器)
├── Jupyter Lab (8088) - 前端 UI
│   └── 用户通过浏览器访问编写 Notebook
├── Flask API (8097) - 后端 API
│   └── Develop Backend 调用执行 Notebook
└── Papermill - 批量执行工具
    └── 执行 Notebook 并返回结果
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
  "input_path": "/workspace/notebooks/1/10/abc.ipynb",
  "output_path": "/workspace/notebooks/1/10/exec-123.ipynb",
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
  "output_path": "/workspace/notebooks/1/10/exec-123.ipynb",
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
# 构建镜像
docker build -t addp-jupyter-engine .

# 运行容器
docker run -p 8088:8088 -p 8097:8097 \
  -v $(pwd)/notebooks:/workspace/notebooks \
  addp-jupyter-engine
```

## 环境变量

- `JUPYTER_PORT`: Jupyter Lab 端口（默认 8088）
- `API_PORT`: Flask API 端口（默认 8097）

## 依赖库

详见 requirements.txt
