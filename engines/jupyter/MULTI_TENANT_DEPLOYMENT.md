# Jupyter Engine 多租户部署指南

## 概述

Jupyter Engine 支持多租户隔离，通过 MinIO 路径前缀实现不同租户的数据隔离。

**重要**：ADDP 采用租户级别隔离，租户内的用户默认共享所有 Notebook，无需用户级别的路径隔离。

## 路径结构

```
MinIO develop bucket/
├── tenant_1/
│   ├── notebooks/
│   │   ├── analysis.ipynb
│   │   └── report.ipynb
│   └── executions/
│       ├── analysis_exec_20260209_143022.ipynb
│       └── report_exec_20260209_150134.ipynb
├── tenant_2/
│   ├── notebooks/
│   └── executions/
└── ...
```

## 部署模式

### 1. 开发环境（单租户）

**适用场景**: 本地开发、测试

**配置**: 使用 `.env` 中的默认配置

```bash
# .env
DEFAULT_TENANT_ID=1
```

**启动**:
```bash
cd engines/jupyter
./venv/bin/jupyter lab --config=jupyter_lab_config.py
```

**访问**: 所有用户共享同一个 Jupyter Lab 实例，使用 `tenant_1/notebooks/` 路径

---

### 2. 生产环境（多租户）

**适用场景**: 生产部署、多租户隔离

**方案**: 为每个租户启动独立的 Jupyter Server 实例

#### 租户 1 实例
```bash
export JUPYTER_TENANT_ID=1
export JUPYTER_PORT=8088

cd engines/jupyter
./venv/bin/jupyter lab --config=jupyter_lab_config.py
```

#### 租户 2 实例
```bash
export JUPYTER_TENANT_ID=2
export JUPYTER_PORT=8089

cd engines/jupyter
./venv/bin/jupyter lab --config=jupyter_lab_config.py
```

#### 租户 3 实例
```bash
export JUPYTER_TENANT_ID=3
export JUPYTER_PORT=8090

cd engines/jupyter
./venv/bin/jupyter lab --config=jupyter_lab_config.py
```

---

### 3. 使用 Docker Compose（推荐）

为每个租户创建独立的服务：

```yaml
# docker-compose.yml
services:
  jupyter-tenant-1:
    image: jupyter-engine:latest
    environment:
      - JUPYTER_TENANT_ID=1
      - JUPYTER_PORT=8088
      - MINIO_ENDPOINT=minio:9000
      - MINIO_BUCKET=develop
    ports:
      - "8088:8088"
    volumes:
      - ./jupyter_lab_config.py:/app/jupyter_lab_config.py

  jupyter-tenant-2:
    image: jupyter-engine:latest
    environment:
      - JUPYTER_TENANT_ID=2
      - JUPYTER_PORT=8089
    ports:
      - "8089:8089"

  jupyter-tenant-3:
    image: jupyter-engine:latest
    environment:
      - JUPYTER_TENANT_ID=3
      - JUPYTER_PORT=8090
    ports:
      - "8090:8090"
```

---

## 反向代理配置

使用 Nginx 根据租户 ID 路由到不同的 Jupyter 实例：

```nginx
# nginx.conf
http {
    upstream jupyter_tenant_1 {
        server localhost:8088;
    }

    upstream jupyter_tenant_2 {
        server localhost:8089;
    }

    upstream jupyter_tenant_3 {
        server localhost:8090;
    }

    server {
        listen 8080;

        # 根据 X-Tenant-ID header 路由
        location /jupyter/tenant/1/ {
            proxy_pass http://jupyter_tenant_1/;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location /jupyter/tenant/2/ {
            proxy_pass http://jupyter_tenant_2/;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }

        location /jupyter/tenant/3/ {
            proxy_pass http://jupyter_tenant_3/;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }
    }
}
```

---

## 验证多租户隔离

### 测试租户 1
```bash
export JUPYTER_TENANT_ID=1

# 启动 Jupyter Lab
jupyter lab --config=jupyter_lab_config.py

# 访问
curl http://localhost:8088/api/contents
# 应该看到 tenant_1/notebooks/ 下的文件
```

### 测试租户 2
```bash
export JUPYTER_TENANT_ID=2

# 启动 Jupyter Lab（不同端口）
export JUPYTER_PORT=8089
jupyter lab --config=jupyter_lab_config.py

# 访问
curl http://localhost:8089/api/contents
# 应该看到 tenant_2/notebooks/ 下的文件（可能为空）
```

---

## MinIO 权限配置

为每个租户配置 MinIO 访问策略：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject"
      ],
      "Resource": [
        "arn:aws:s3:::develop/tenant_1/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::develop"
      ],
      "Condition": {
        "StringLike": {
          "s3:prefix": ["tenant_1/*"]
        }
      }
    }
  ]
}
```

---

## 安全建议

1. **独立实例**: 生产环境为每个租户启动独立的 Jupyter 实例
2. **环境变量**: 通过环境变量而非配置文件传递租户 ID
3. **网络隔离**: 使用 Docker 网络或 VPC 隔离不同租户的实例
4. **访问控制**: 在 Gateway 层面验证用户身份和租户权限
5. **审计日志**: 记录所有文件操作和执行记录
6. **租户内共享**: 租户内的用户默认共享所有 Notebook，权限控制在应用层（dev_items 表）而非存储层

---

## 故障排查

### 问题 1: 文件路径错误
**症状**: 无法找到文件，路径不正确

**解决**:
```bash
# 检查环境变量
echo $JUPYTER_TENANT_ID

# 检查 MinIO 中的实际路径
mc ls minio/develop/tenant_1/notebooks/
```

### 问题 2: 跨租户访问
**症状**: 租户 A 可以看到租户 B 的文件

**解决**:
- 确认每个租户使用独立的 Jupyter 实例
- 检查 JUPYTER_TENANT_ID 环境变量是否正确设置
- 验证 MinIO 权限策略

---

## 集成到 ADDP

### Develop 模块集成

在 Develop 模块的后端，根据用户的 tenant_id 选择对应的 Jupyter 实例：

```go
// develop/backend/internal/service/notebook_execution_service.go

func (s *NotebookExecutionService) GetJupyterURL(tenantID uint) string {
    // 根据租户 ID 返回对应的 Jupyter Lab URL
    switch tenantID {
    case 1:
        return "http://jupyter-tenant-1:8088"
    case 2:
        return "http://jupyter-tenant-2:8089"
    case 3:
        return "http://jupyter-tenant-3:8090"
    default:
        return "http://jupyter-engine:8088" // 默认实例
    }
}
```

---

## 监控和扩展

### 监控指标

- 每个租户的 Jupyter 实例状态
- 每个租户的文件数量和存储大小
- 每个租户的 Notebook 执行次数和耗时

### 自动扩展

- 根据租户数量动态启动/停止 Jupyter 实例
- 使用 Kubernetes HPA 自动扩展
- 闲置租户的实例可以停止以节省资源

---

## 总结

| 部署模式 | 适用场景 | 租户隔离 | 复杂度 |
|---------|---------|---------|--------|
| **单实例（默认）** | 开发、测试 | ❌ 无 | 低 |
| **多实例** | 生产、多租户 | ✅ 完全 | 中 |
| **Docker Compose** | 容器化部署 | ✅ 完全 | 中 |
| **Kubernetes** | 大规模部署 | ✅ 完全 | 高 |

**推荐**:
- 开发环境：单实例（默认配置）
- 生产环境：Docker Compose 多实例 + Nginx 反向代理
- 租户内共享：同一租户的用户共享所有 Notebook，权限在应用层控制
