# Test 脚本说明

本目录包含 ADDP 各模块的测试和验证脚本，用于确保功能正常运行。

## 📋 脚本清单

### 资源同步测试

#### `test-resource-sync.sh`
**用途**: 测试 System 和 Meta 模块之间的资源同步

**功能**:
- 在 System 模块创建测试资源
- 触发 Meta 模块同步
- 验证资源在 Meta 模块中正确创建
- 测试增量更新和删除同步

**测试覆盖**:
- ✅ 资源创建同步
- ✅ 资源更新同步
- ✅ 资源删除同步
- ✅ 租户隔离验证

**命令**:
```bash
./scripts/test/test-resource-sync.sh
```

**预期输出**:
```
[✓] System 创建资源成功
[✓] Meta 同步资源成功
[✓] 资源字段匹配
[✓] 租户隔离正确
```

---

### 瓦片 API 测试

#### `test-tile-api.sh`
**用途**: 测试 Manager 模块的 MVT 瓦片生成和缓存

**功能**:
- 请求指定图层的 MVT 瓦片
- 验证瓦片格式正确性
- 测试缓存命中率
- 性能压力测试

**测试覆盖**:
- ✅ MVT 瓦片生成（z/x/y 参数）
- ✅ MinIO 缓存存储
- ✅ 缓存命中逻辑
- ✅ 并发请求处理

**命令**:
```bash
# 基础测试
./scripts/test/test-tile-api.sh

# 指定图层和缩放级别
./scripts/test/test-tile-api.sh --table dltb --zoom 10
```

**预期输出**:
```
[✓] 瓦片生成成功 (200 OK)
[✓] Content-Type: application/x-protobuf
[✓] 缓存到 MinIO 成功
[✓] 第二次请求命中缓存 (<10ms)
```

**注意**: 此脚本当前为交互式,计划改进为自动化测试。

---

## 🔄 测试流程

### 完整系统测试

```bash
# 1. 测试基础设施（确保服务运行）
./scripts/infra/status.sh

# 2. 测试资源同步（System ↔ Meta）
./scripts/test/test-resource-sync.sh

# 3. 测试 MVT 瓦片（Manager）
./scripts/test/test-tile-api.sh
```

### CI/CD 集成

这些测试脚本可以集成到 CI/CD 流程：

```yaml
# .github/workflows/test.yml
jobs:
  test:
    steps:
      - name: 启动基础设施
        run: ./scripts/infra/up.sh

      - name: 运行测试
        run: |
          ./scripts/test/test-resource-sync.sh
          ./scripts/test/test-tile-api.sh
```

---

## 📊 测试覆盖范围

| 模块 | 测试脚本 | 覆盖功能 | 状态 |
|------|---------|---------|------|
| **System** | test-resource-sync.sh | 资源管理、租户隔离 | ✅ |
| **Manager** | test-tile-api.sh | MVT 瓦片、缓存 | ⚠️ 需改进 |
| **Meta** | test-resource-sync.sh | 元数据同步 | ✅ |
| **Transfer** | - | 数据传输、队列 | ⏳ 规划中 |
| **Gateway** | - | API 路由 | ⏳ 待补充 |
| **Orchestrator** | - | 编排流程 | ⏳ 待补充 |

**图例**:
- ✅ 已实现且可用
- ⚠️ 已实现但需改进
- ⏳ 规划中/待实现

---

## ⚠️ 运行前提条件

1. **基础设施已启动**
   ```bash
   ./scripts/infra/up.sh
   ./scripts/infra/status.sh  # 确认所有服务运行
   ```

2. **后端服务已启动**
   ```bash
   ./scripts/dev/start.sh
   # 或
   ./scripts/prod/start.sh
   ```

3. **测试数据准备**
   - 某些测试需要预先创建测试资源
   - 脚本会自动创建和清理测试数据

4. **环境变量**
   - 测试脚本会读取 `.env` 文件
   - 确保 `JWT_SECRET`、数据库连接等配置正确

---

## 🐛 测试失败排查

### 常见问题

**问题 1**: `Connection refused` 错误
```bash
# 解决方案：检查服务是否运行
docker compose ps
./scripts/dev/status.sh
```

**问题 2**: `Unauthorized` 错误
```bash
# 解决方案：检查 JWT token 是否有效
# 测试脚本应该自动登录获取 token
```

**问题 3**: 测试数据未清理
```bash
# 解决方案：手动清理测试数据
# 测试脚本应该在结束时自动清理
```

---

## 📝 编写新测试

如需添加新的测试脚本，请遵循以下模板：

```bash
#!/bin/bash
set -e

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo "开始测试: XXX 功能"

# 测试前准备
echo "1. 准备测试数据..."

# 执行测试
echo "2. 执行测试..."
result=$(curl -s -X POST http://localhost:8080/api/xxx)

# 验证结果
if [ "$result" == "expected" ]; then
    echo -e "${GREEN}[✓] 测试通过${NC}"
else
    echo -e "${RED}[✗] 测试失败${NC}"
    exit 1
fi

# 清理
echo "3. 清理测试数据..."
```

---

## 🔗 相关文档

- [调试脚本](../debug/README.md) - 故障诊断工具
- [开发环境](../dev/README.md) - 启动开发服务
- [基础设施](../infra/README.md) - 启动数据库等服务

---

## 💡 最佳实践

1. **测试隔离**: 每个测试使用独立的测试数据，避免相互影响
2. **自动清理**: 测试结束后自动删除测试数据
3. **幂等性**: 测试可以重复运行，不会因为之前的运行而失败
4. **详细输出**: 测试失败时提供足够的调试信息
5. **快速反馈**: 优先运行快速测试，慢速测试放在最后

---

## 🗑️ 已删除的测试脚本

以下脚本已被删除，因为相应功能尚未实现或已过时：

- ~~`verify-transfer.sh`~~ - 功能重复，已被 `scripts/infra/status.sh` 覆盖
- ~~`transfer/test-encryption.sh`~~ - Transfer 模块尚未实现该功能
- ~~`transfer/test-spatial-api.sh`~~ - API 设计草稿，非可执行测试

当相关功能实现后，将添加相应的测试脚本。
