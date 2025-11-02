# 超级管理员自动创建指南

## 📋 概述

System 模块在首次启动时会**自动创建超级管理员账号**，管理员的用户名和密码可以通过环境变量配置。

## 🔧 工作原理

### 自动创建逻辑

在 [system/backend/internal/repository/database.go:62](../system/backend/internal/repository/database.go#L62) 中实现：

```go
func InitSuperAdmin(db *gorm.DB) error {
    // 从环境变量读取超级管理员配置
    adminUsername := getEnv("SUPER_ADMIN_USERNAME", "SuperAdmin")
    adminPassword := getEnv("SUPER_ADMIN_PASSWORD", "20251001#SuperAdmin")
    adminEmail := getEnv("SUPER_ADMIN_EMAIL", "superadmin@addp.com")

    // 检查用户是否已存在
    var user models.User
    result := db.Where("username = ?", adminUsername).First(&user)

    if result.Error == gorm.ErrRecordNotFound {
        // 创建超级管理员
        passwordHash, _ := utils.HashPassword(adminPassword)
        superAdminUser := models.User{
            Username:     adminUsername,
            Email:        adminEmail,
            PasswordHash: passwordHash,
            FullName:     "系统超级管理员",
            IsActive:     true,
            UserType:     models.UserTypeSuperAdmin,
            TenantID:     nil,
            IsSuperuser:  true,
        }
        db.Create(&superAdminUser)
        log.Printf("✅ 超级管理员已创建: %s / %s\n", adminUsername, adminPassword)
    }

    return nil
}
```

### 执行时机

在 [system/backend/cmd/server/main.go:36](../system/backend/cmd/server/main.go#L36) 启动时自动执行：

```go
func main() {
    // ... 初始化数据库

    // 自动迁移
    if err := repository.AutoMigrate(db); err != nil {
        logger.L().Error("数据库迁移失败", "error", err)
        os.Exit(1)
    }

    // 初始化超级管理员用户 ⬅️ 这里自动创建
    if err := repository.InitSuperAdmin(db); err != nil {
        logger.L().Error("超级管理员用户初始化失败", "error", err)
        os.Exit(1)
    }

    // ... 启动服务器
}
```

---

## ⚙️ 配置方式

### 方式 1: 使用 `generate-env.sh` 自动生成（推荐）

在服务器上执行：

```bash
cd ~/addp

# 自动生成随机密码
./scripts/generate-env.sh 192.168.31.238:5001
```

**输出示例**:
```
===========================================
  生成的配置
===========================================

Registry: 192.168.31.238:5001
JWT_SECRET: xK8mN3pQ7rT2vY9wZ1aB4cD6eF8gH0iJ
ENCRYPTION_KEY: pL2mN5oP8qR1sT4uV7wX0yZ3aB6cD9eF
POSTGRES_PASSWORD: aB3cD6eF9gH2iJ5k
REDIS_PASSWORD: lM8nN1oP4qR7sT0u
MINIO_PASSWORD: vW3xX6yY9zZ2aA5b

超级管理员账号:
  用户名: SuperAdmin
  密码: cC9dD2eE5fF8gG1hH4  ⬅️ 自动生成的随机密码
  邮箱: superadmin@addp.com

⚠️  重要: 请保存这些密钥，特别是 JWT_SECRET 和超级管理员密码

✅ 密钥已保存到: .env.secrets.20251031_143025.txt
```

生成的密钥会保存到带时间戳的 `.env.secrets.YYYYMMDD_HHMMSS.txt` 文件中，**请妥善保管此文件**！

### 方式 2: 手动配置 .env 文件

如果想使用自定义的管理员密码，可以手动编辑 `.env` 文件：

```bash
# 复制示例文件
cp .env.example .env

# 编辑配置
vim .env
```

修改以下配置：

```bash
# 超级管理员配置
SUPER_ADMIN_USERNAME=SuperAdmin       # 可自定义用户名
SUPER_ADMIN_PASSWORD=YourSecurePassword123!  # 自定义强密码
SUPER_ADMIN_EMAIL=admin@yourcompany.com      # 可自定义邮箱
```

**密码要求**:
- ✅ 建议至少 12 字符
- ✅ 包含大小写字母、数字和特殊字符
- ❌ 避免使用字典词汇或简单字符串
- ❌ 生产环境禁止使用默认密码 `20251001#SuperAdmin`

---

## 🚀 部署流程

### 完整部署步骤

#### 在开发机上操作

```bash
cd ~/code/addp

# 1. 确保代码是最新的（已包含超级管理员自动创建功能）
git status

# 2. 推送 system-backend 镜像到私有 Registry
./scripts/push-to-local-registry-multiarch-cached.sh 5001

# 3. 传输配置文件到服务器
scp docker-compose.prod.yml pampa@192.168.31.174:~/addp/
scp .env.example pampa@192.168.31.174:~/addp/
scp scripts/generate-env.sh pampa@192.168.31.174:~/addp/scripts/
scp scripts/clean-deploy.sh pampa@192.168.31.174:~/addp/scripts/
```

#### 在服务器上操作

```bash
ssh pampa@192.168.31.174
cd ~/addp

# 4. 生成配置文件（包含随机管理员密码）
chmod +x scripts/generate-env.sh
./scripts/generate-env.sh 192.168.31.238:5001

# ⚠️ 重要: 记录显示的管理员密码，或查看保存的密钥文件
cat .env.secrets.*.txt

# 5. 完全清理并重新部署
chmod +x scripts/clean-deploy.sh
REGISTRY=192.168.31.238:5001 ./scripts/clean-deploy.sh

# 6. 验证 system-backend 启动日志
docker logs addp-system-backend | grep "超级管理员"

# 应该看到:
# ✅ 超级管理员已创建: SuperAdmin / <生成的密码>
```

---

## ✅ 验证超级管理员

### 方法 1: 查看容器日志

```bash
docker logs addp-system-backend | grep "超级管理员"
```

**成功输出**:
```
✅ 超级管理员已创建: SuperAdmin / cC9dD2eE5fF8gG1hH4
```

### 方法 2: 直接查询数据库

```bash
# 进入 PostgreSQL 容器
docker exec -it addp-postgres psql -U addp -d addp

# 查询超级管理员
\c addp
SET search_path TO system;
SELECT id, username, email, user_type, is_superuser FROM users WHERE user_type = 'super_admin';
```

**预期输出**:
```
 id | username   | email               | user_type   | is_superuser
----+------------+---------------------+-------------+--------------
  1 | SuperAdmin | superadmin@addp.com | super_admin | t
```

### 方法 3: 测试登录

```bash
# 使用生成的密码登录
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "SuperAdmin",
    "password": "cC9dD2eE5fF8gG1hH4"
  }'
```

**成功响应**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": 1,
    "username": "SuperAdmin",
    "email": "superadmin@addp.com",
    "user_type": "super_admin",
    "is_superuser": true
  }
}
```

---

## 🔐 安全注意事项

### 1. 密码管理

- ✅ **生产环境必须修改默认密码**
- ✅ 使用 `generate-env.sh` 生成随机强密码
- ✅ 妥善保管 `.env.secrets.*.txt` 文件
- ❌ 不要将密码提交到 Git 仓库
- ❌ 不要在代码或文档中硬编码密码

### 2. 环境变量保护

```bash
# .env 文件权限设置
chmod 600 .env
chmod 600 .env.secrets.*.txt

# 确保 .gitignore 包含
.env
.env.secrets.*.txt
```

### 3. 密码修改

**重要**: 修改 `.env` 中的 `SUPER_ADMIN_PASSWORD` **不会更新已存在的用户密码**。

如需修改超级管理员密码，有两种方式：

#### 方式 1: 通过系统管理界面修改（推荐）

1. 使用当前密码登录系统
2. 进入用户管理界面
3. 修改超级管理员密码

#### 方式 2: 重置用户（需要删库重建）

```bash
# ⚠️ 警告: 此操作会删除所有数据！

# 停止服务
docker-compose -f docker-compose.prod.yml down

# 删除数据库卷
docker volume rm addp_postgres_data

# 修改 .env 中的 SUPER_ADMIN_PASSWORD

# 重新启动（会重新创建管理员）
REGISTRY=192.168.31.238:5001 ./scripts/clean-deploy.sh
```

---

## 🛠️ 故障排查

### 问题 1: 超级管理员未创建

**症状**: 登录失败，日志中没有 "超级管理员已创建" 信息

**排查步骤**:

```bash
# 1. 检查容器启动日志
docker logs addp-system-backend | tail -50

# 2. 检查数据库连接
docker exec addp-system-backend env | grep POSTGRES

# 3. 检查环境变量
docker exec addp-system-backend env | grep SUPER_ADMIN
```

**可能原因**:
- PostgreSQL 未就绪（健康检查失败）
- 环境变量未正确传递
- 数据库迁移失败

**解决方法**:
```bash
# 重启 system-backend 容器
docker-compose -f docker-compose.prod.yml restart system-backend

# 查看新的启动日志
docker logs -f addp-system-backend
```

### 问题 2: 密码错误

**症状**: 使用生成的密码登录失败

**排查步骤**:

```bash
# 1. 确认 .env 中的密码
cat .env | grep SUPER_ADMIN_PASSWORD

# 2. 确认密钥文件中的密码
cat .env.secrets.*.txt | grep "密码:"

# 3. 确认容器环境变量
docker exec addp-system-backend env | grep SUPER_ADMIN_PASSWORD
```

**可能原因**:
- 密码复制时包含了额外的空格或换行符
- 密码特殊字符在 shell 中被转义

**解决方法**:
```bash
# 使用引号包裹密码
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"SuperAdmin\",\"password\":\"$SUPER_ADMIN_PASSWORD\"}"
```

### 问题 3: 用户已存在但密码不对

**症状**: 修改了 `.env` 中的密码，但旧密码仍然有效

**原因**: `InitSuperAdmin` 只在用户不存在时创建，不会更新已有用户的密码

**解决方法**: 参考上面的 "密码修改" 章节

---

## 📚 相关文档

- [System Backend 启动失败修复](FIX_SYSTEM_BACKEND_STARTUP.md)
- [服务器部署指南](DEPLOY_TO_SERVER.md)
- [配置中心使用指南](CONFIG_CENTER.md)

---

## 🎯 总结

1. **自动创建**: System 模块首次启动时自动创建超级管理员
2. **环境变量配置**: 用户名、密码、邮箱均可通过 `.env` 文件配置
3. **随机密码生成**: `generate-env.sh` 脚本自动生成安全的随机密码
4. **密钥保存**: 所有密钥保存到带时间戳的 `.env.secrets.*.txt` 文件
5. **生产安全**: 生产环境必须修改默认密码，使用强密码

现在您可以使用自动生成的超级管理员账号登录系统了！🚀
