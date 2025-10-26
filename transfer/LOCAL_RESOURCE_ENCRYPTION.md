# Transfer 模块本地存储引擎密码加密机制

**版本**: v1.3.0
**完成日期**: 2025-01-24
**状态**: ✅ 已实现

---

## 📌 概述

Transfer 模块的本地存储引擎（`LocalResource`）现已实现密码加密功能，确保敏感信息（如数据库密码、MinIO secret_key）在数据库中以加密形式存储，提升系统安全性。

---

## 🔒 加密机制

### 加密算法

- **算法**: AES-256-GCM
- **密钥长度**: 32 字节（256 位）
- **密钥来源**:
  - 优先从 System 模块配置中心获取（`ENCRYPTION_KEY`）
  - Fallback 到本地环境变量 `ENCRYPTION_KEY`
  - 开发环境默认值（不安全，仅供测试）

### 加密字段

根据资源类型，加密以下敏感字段：

| 资源类型 | 加密字段 | 说明 |
|---------|---------|------|
| **PostgreSQL / MySQL** | `connection_info.password` | 数据库密码 |
| **MinIO / S3 / OSS** | `connection_info.secret_key` | 对象存储密钥 |

### 加密时机

| 操作 | 加密/解密 | 说明 |
|------|----------|------|
| **创建资源** (`POST /api/local-resources`) | ✅ 加密 | 前端提交明文密码，后端加密后存入数据库 |
| **更新资源** (`PUT /api/local-resources/:id`) | ✅ 加密 | 更新时重新加密密码 |
| **获取资源** (`GET /api/local-resources/:id`) | 🔓 解密 | 从数据库读取后解密，返回明文给前端 |
| **列出资源** (`GET /api/local-resources`) | 🔓 解密 | 批量解密所有资源的密码 |
| **测试连接** (`POST /api/local-resources/:id/test`) | 🔓 解密 | 使用解密后的密码测试连接 |

---

## 🔧 实现细节

### 核心代码

**加密方法**:
```go
// transfer/backend/internal/service/local_resource_service.go:277-302
func (s *LocalResourceService) encryptConnectionInfo(connInfo models.JSONMap) error {
    // 验证密钥长度
    if len(s.cfg.EncryptionKey) != 32 {
        return fmt.Errorf("encryption key must be 32 bytes")
    }

    // 加密 password 字段（PostgreSQL/MySQL）
    if password, ok := connInfo["password"].(string); ok && password != "" {
        encrypted, err := commonUtils.Encrypt(password, s.cfg.EncryptionKey)
        if err != nil {
            return fmt.Errorf("failed to encrypt password: %w", err)
        }
        connInfo["password"] = encrypted
    }

    // 加密 secret_key 字段（MinIO/S3）
    if secretKey, ok := connInfo["secret_key"].(string); ok && secretKey != "" {
        encrypted, err := commonUtils.Encrypt(secretKey, s.cfg.EncryptionKey)
        if err != nil {
            return fmt.Errorf("failed to encrypt secret_key: %w", err)
        }
        connInfo["secret_key"] = encrypted
    }

    return nil
}
```

**解密方法**:
```go
// transfer/backend/internal/service/local_resource_service.go:304-333
func (s *LocalResourceService) decryptConnectionInfo(connInfo models.JSONMap) error {
    // 验证密钥长度
    if len(s.cfg.EncryptionKey) != 32 {
        return fmt.Errorf("decryption key must be 32 bytes")
    }

    // 解密 password 字段
    if encryptedPassword, ok := connInfo["password"].(string); ok && encryptedPassword != "" {
        decrypted, err := commonUtils.Decrypt(encryptedPassword, s.cfg.EncryptionKey)
        if err != nil {
            s.logger.Warn("failed to decrypt password, might be plaintext", "error", err)
            return err
        }
        connInfo["password"] = decrypted
    }

    // 解密 secret_key 字段
    if encryptedSecretKey, ok := connInfo["secret_key"].(string); ok && encryptedSecretKey != "" {
        decrypted, err := commonUtils.Decrypt(encryptedSecretKey, s.cfg.EncryptionKey)
        if err != nil {
            s.logger.Warn("failed to decrypt secret_key, might be plaintext", "error", err)
            return err
        }
        connInfo["secret_key"] = decrypted
    }

    return nil
}
```

### 调用位置

| 方法 | 调用位置 | 操作 |
|------|---------|------|
| `Create()` | [local_resource_service.go:95](transfer/backend/internal/service/local_resource_service.go#L95) | 加密后创建 |
| `Update()` | [local_resource_service.go:105](transfer/backend/internal/service/local_resource_service.go#L105) | 加密后更新 |
| `Get()` | [local_resource_service.go:87](transfer/backend/internal/service/local_resource_service.go#L87) | 解密后返回 |
| `List()` | [local_resource_service.go:61](transfer/backend/internal/service/local_resource_service.go#L61) | 批量解密 |

---

## ⚙️ 配置

### 环境变量

```bash
# transfer/backend/.env
ENCRYPTION_KEY=your-base64-encoded-32-byte-key
```

### 生成密钥

**方法 1: 使用 OpenSSL**
```bash
openssl rand -base64 32
```

**方法 2: 使用 Go 代码**
```go
package main

import (
    "encoding/base64"
    "github.com/addp/common/utils"
)

func main() {
    key, _ := utils.GenerateKey()
    encoded := utils.EncodeKey(key)
    println(encoded)
}
```

### 密钥共享

**重要**: Transfer 模块应与 System 模块使用**相同的加密密钥**。

**原因**:
1. ✅ **资源迁移便利**: 可以使用 `SyncToSystem` 方法将 Transfer 资源推送到 System
2. ✅ **一致性**: 所有存储引擎密码使用统一加密方式
3. ✅ **维护简单**: 只需管理一个密钥

**配置方式**:
```bash
# 根目录 .env（所有模块共享）
ENCRYPTION_KEY=your-base64-encoded-32-byte-key

# system/backend/.env
# (通过 common/config 读取根 .env)

# transfer/backend/.env
# (通过 common/config 读取根 .env)
```

---

## ✅ 测试验证

### 自动化测试脚本

运行 [test-encryption.sh](./test-encryption.sh) 进行完整测试：

```bash
cd /Users/pampa/code/addp/transfer
./test-encryption.sh
```

### 测试步骤

1. **创建 PostgreSQL 资源** - 提交明文密码
2. **检查数据库** - 验证密码已加密存储
3. **通过 API 获取** - 验证密码已解密返回
4. **创建 MinIO 资源** - 提交明文 secret_key
5. **检查数据库** - 验证 secret_key 已加密
6. **清理测试数据**

### 手动测试

#### 1. 创建资源（密码加密）

```bash
TOKEN="your-jwt-token"

curl -X POST http://localhost:8083/api/local-resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "测试PostgreSQL",
    "resource_type": "postgresql",
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "database": "test_db",
      "user": "test_user",
      "password": "plain_password_123"
    }
  }'
```

#### 2. 检查数据库（验证加密）

```bash
PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c \
  "SELECT id, name, connection_info->>'password' AS encrypted_password
   FROM transfer.local_resources
   WHERE resource_type = 'postgresql';"
```

**期望结果**: `encrypted_password` 列显示的是 Base64 编码的密文，不是明文 `plain_password_123`

#### 3. 通过 API 获取（验证解密）

```bash
curl http://localhost:8083/api/local-resources \
  -H "Authorization: Bearer $TOKEN" | jq
```

**期望结果**: 返回的 `connection_info.password` 是明文 `plain_password_123`

---

## 🔐 安全最佳实践

### 1. 密钥管理

| 环境 | 密钥来源 | 存储位置 |
|------|---------|---------|
| **开发环境** | `.env` 文件 | 项目根目录（不提交到 Git） |
| **测试环境** | 环境变量 | CI/CD 系统密钥管理 |
| **生产环境** | 密钥管理服务 | AWS Secrets Manager / HashiCorp Vault |

### 2. 密钥轮换

**步骤**:
1. 生成新密钥
2. 使用新密钥启动新实例
3. 迁移数据（解密 → 重新加密）
4. 停止旧实例
5. 更新所有服务的密钥

**迁移脚本示例**:
```go
// 解密旧数据
oldKey := []byte("old-32-byte-key")
decrypted, _ := utils.Decrypt(encryptedPassword, oldKey)

// 用新密钥重新加密
newKey := []byte("new-32-byte-key")
newEncrypted, _ := utils.Encrypt(decrypted, newKey)

// 更新数据库
db.Exec("UPDATE transfer.local_resources SET connection_info = ...")
```

### 3. 权限控制

- ✅ 只有 Transfer 服务可以访问 `transfer.local_resources` 表
- ✅ 数据库用户权限最小化（只读必要字段）
- ✅ API 层面强制租户隔离（`tenant_id` 过滤）

### 4. 审计日志

**建议**: 记录密码访问日志

```go
s.logger.Info("password decrypted for connection test",
    "resource_id", id,
    "user_id", userID,
    "tenant_id", tenantID)
```

---

## 🆚 与 System 模块对比

| 特性 | System 模块 | Transfer 模块 |
|------|------------|--------------|
| **加密算法** | AES-256-GCM | AES-256-GCM |
| **加密字段** | `connection_info.password` + `connection_info.secret_key` | 同左 |
| **密钥来源** | `ENCRYPTION_KEY` 环境变量 | 同左（共享） |
| **加密时机** | Create/Update | 同左 |
| **解密时机** | Get/List + TestConnection | 同左 |
| **API 端点** | `/api/resources` | `/api/local-resources` |
| **存储表** | `system.resources` | `transfer.local_resources` |
| **租户隔离** | ✅ | ✅ |

---

## 📚 相关文件

| 文件 | 说明 |
|------|------|
| [local_resource_service.go:277-333](transfer/backend/internal/service/local_resource_service.go) | 加密/解密实现 |
| [common/utils/encryption.go](../common/utils/encryption.go) | 底层加密工具 |
| [.env.example](backend/.env.example) | 配置模板 |
| [test-encryption.sh](test-encryption.sh) | 自动化测试脚本 |

---

## 🐛 故障排查

### 问题 1: 密码解密失败

**错误信息**:
```
WARN failed to decrypt password, might be plaintext
```

**原因**:
- 密钥不正确
- 数据库中存储的是明文（未加密）
- 密钥轮换后旧数据未迁移

**解决方案**:
1. 检查 `ENCRYPTION_KEY` 是否正确
2. 验证密钥长度为 32 字节
3. 检查是否与 System 模块使用相同密钥

### 问题 2: 连接测试失败

**错误信息**:
```
failed to connect: authentication failed
```

**原因**:
- 解密后的密码不正确
- 密码在数据库中损坏

**解决方案**:
1. 重新创建资源（输入正确密码）
2. 检查日志中的解密过程
3. 直接查询数据库验证加密数据

### 问题 3: 密钥长度错误

**错误信息**:
```
encryption key must be 32 bytes, got X
```

**原因**:
- `ENCRYPTION_KEY` Base64 解码后长度不是 32 字节

**解决方案**:
```bash
# 重新生成正确长度的密钥
openssl rand -base64 32
```

---

## 🎯 未来改进

### 短期（v1.4.0）
- [ ] 添加单元测试（加密/解密逻辑）
- [ ] 实现密码强度校验
- [ ] 记录密码访问审计日志

### 中期（v1.5.0）
- [ ] 支持密钥轮换工具
- [ ] 实现字段级加密（除密码外的敏感字段）
- [ ] 集成密钥管理服务（Vault）

### 长期（v2.0.0）
- [ ] 支持多租户独立密钥
- [ ] 实现前端密码输入脱敏
- [ ] 添加密码泄露检测

---

## 📝 版本历史

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.3.0 | 2025-01-24 | ✅ 初始实现：密码加密/解密功能 |
| v1.2.0 | 2025-01-21 | Portal 集成 + SystemClient + 内部认证 |
| v1.1.0 | 2025-01-20 | 基础功能实现 |

---

**维护者**: Claude Code
**反馈**: 如有问题请提交 Issue
