# 环境变量占位符问题修复

## 问题描述

**症状**: System Backend 和其他后端服务不断重启,健康检查失败

**根本原因**: `.env.prod` 文件中有多个占位符变量未被替换为实际值

## 发现的占位符变量

在 `.env.prod.example` 中,以下变量使用了 `WILL_BE_GENERATED_ON_SETUP` 占位符:

1. ✅ `JWT_SECRET` - 已生成
2. ✅ `ENCRYPTION_KEY` - 已生成
3. ❌ `INTERNAL_API_KEY` - **未生成** (缺失)
4. ✅ `POSTGRES_PASSWORD` - 已生成
5. ❌ `REDIS_PASSWORD` - **未生成** (缺失)
6. ❌ `MINIO_ROOT_PASSWORD` - **未生成** (缺失)
7. ❌ `BUSINESS_MINIO_SECRET_KEY` - **未生成** (缺失)

## 导致的问题

### 问题 1: 数据库认证失败

PostgreSQL 容器初始化时使用默认密码 `addp_password`,但 `.env.prod` 中的 `POSTGRES_PASSWORD` 是自动生成的随机值(如 `FI0ZmcQx26WlvryG`),导致所有后端服务无法连接数据库。

**错误日志**:
```
System Backend: failed to connect to database: password authentication failed
```

### 问题 2: Redis 连接失败

Redis 配置了密码,但 `.env.prod` 中的 `REDIS_PASSWORD` 是占位符,导致后端服务连接Redis失败。

### 问题 3: MinIO 连接失败

MinIO 密码不匹配,导致文件上传和对象存储功能失败。

## 修复方案

### ✅ 已实施的修复

修改 `scripts/deploy/3-server-setup.sh` (第440-486行),生成所有缺失的密钥:

```bash
# Generate JWT_SECRET (32 bytes base64)
JWT_SECRET=$(openssl rand -base64 32)
sed -i.bak "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" "$ENV_FILE"

# Generate ENCRYPTION_KEY (32 bytes base64)
ENCRYPTION_KEY=$(openssl rand -base64 32)
sed -i.bak "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=${ENCRYPTION_KEY}|" "$ENV_FILE"

# Generate INTERNAL_API_KEY (32 bytes base64) - 新增
INTERNAL_API_KEY=$(openssl rand -base64 32)
sed -i.bak "s|^INTERNAL_API_KEY=.*|INTERNAL_API_KEY=${INTERNAL_API_KEY}|" "$ENV_FILE"

# Generate POSTGRES_PASSWORD (16 characters alphanumeric)
POSTGRES_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${POSTGRES_PASSWORD}|" "$ENV_FILE"

# Generate REDIS_PASSWORD (16 characters alphanumeric) - 新增
REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
sed -i.bak "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASSWORD}|" "$ENV_FILE"

# Generate MINIO_ROOT_PASSWORD (16 characters alphanumeric) - 新增
MINIO_ROOT_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
sed -i.bak "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD}|" "$ENV_FILE"

# Generate BUSINESS_MINIO_SECRET_KEY (16 characters alphanumeric) - 新增
BUSINESS_MINIO_SECRET_KEY=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
sed -i.bak "s|^BUSINESS_MINIO_SECRET_KEY=.*|BUSINESS_MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY}|" "$ENV_FILE"
```

## 手动修复步骤 (如果已部署)

如果您已经部署了旧版本,需要手动修复:

### 步骤 1: SSH 到服务器

```bash
ssh pampa@192.168.1.182
cd ~/addp
```

### 步骤 2: 检查 .env.prod 中的占位符

```bash
grep "WILL_BE_GENERATED_ON_SETUP" .env.prod
```

如果有输出,说明存在未替换的占位符。

### 步骤 3: 生成缺失的密钥

```bash
# 生成所有缺失的密钥
INTERNAL_API_KEY=$(openssl rand -base64 32)
REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
MINIO_ROOT_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)
BUSINESS_MINIO_SECRET_KEY=$(openssl rand -base64 16 | tr -d '=/+' | cut -c1-16)

# 替换占位符
sed -i "s|^INTERNAL_API_KEY=WILL_BE_GENERATED_ON_SETUP|INTERNAL_API_KEY=${INTERNAL_API_KEY}|" .env.prod
sed -i "s|^REDIS_PASSWORD=WILL_BE_GENERATED_ON_SETUP|REDIS_PASSWORD=${REDIS_PASSWORD}|" .env.prod
sed -i "s|^MINIO_ROOT_PASSWORD=WILL_BE_GENERATED_ON_SETUP|MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD}|" .env.prod
sed -i "s|^BUSINESS_MINIO_SECRET_KEY=WILL_BE_GENERATED_ON_SETUP|BUSINESS_MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY}|" .env.prod

# 验证替换成功
grep "WILL_BE_GENERATED_ON_SETUP" .env.prod
# 应该无输出
```

### 步骤 4: 同步 PostgreSQL 密码

如果 PostgreSQL 已使用默认密码初始化,需要更新用户密码:

```bash
# 进入 PostgreSQL 容器
docker compose -f docker-compose.prod.yml exec postgres psql -U addp -d addp

# 更新密码 (替换为 .env.prod 中的 POSTGRES_PASSWORD)
ALTER USER addp WITH PASSWORD 'your-generated-password';

# 退出
\q
```

或者重新初始化 PostgreSQL (会丢失数据):

```bash
# 停止并删除 PostgreSQL 容器和卷
docker compose -f docker-compose.prod.yml stop postgres
docker compose -f docker-compose.prod.yml rm -f postgres
docker volume rm addp_postgres_data

# 重新启动 (使用新密码)
docker compose -f docker-compose.prod.yml up -d postgres
```

### 步骤 5: 重启所有服务

```bash
# 强制重新创建所有容器以应用新环境变量
docker compose -f docker-compose.prod.yml down
docker compose -f docker-compose.prod.yml up -d --force-recreate

# 查看日志
docker compose -f docker-compose.prod.yml logs -f
```

## 验证修复

### 1. 检查 .env.prod

```bash
cat .env.prod | grep -E "JWT_SECRET|ENCRYPTION_KEY|INTERNAL_API_KEY|POSTGRES_PASSWORD|REDIS_PASSWORD|MINIO_ROOT_PASSWORD|BUSINESS_MINIO_SECRET_KEY"
```

**预期**: 所有变量都应该有实际值,没有 `WILL_BE_GENERATED_ON_SETUP`

### 2. 检查服务状态

```bash
docker compose -f docker-compose.prod.yml ps
```

**预期**: 所有服务都应该是 `Up` 或 `Up (healthy)`

### 3. 测试登录

访问 `http://192.168.1.182:8000/`,使用:
- 用户名: `SuperAdmin`
- 密码: `20251001#SuperAdmin`

**预期**: 登录成功,可以正常访问所有模块

## 未来部署

### 使用修复后的脚本

修复后的 `scripts/deploy/3-server-setup.sh` 会自动生成所有必需的密钥。

```bash
# 使用 deploy-all.sh (推荐)
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

脚本会:
1. ✅ 检查 `.env.prod` 是否存在
2. ✅ 如果不存在,从 `.env.prod.example` 复制
3. ✅ 生成所有占位符变量的实际值
4. ✅ 应用到 `.env.prod` 文件
5. ✅ 启动所有服务

### 检查清单

部署后,确认以下检查点:

- [ ] `.env.prod` 文件存在
- [ ] `.env.prod` 中没有 `WILL_BE_GENERATED_ON_SETUP` 占位符
- [ ] 所有服务容器状态为 `healthy` 或 `running`
- [ ] System Backend 日志无数据库连接错误
- [ ] 可以正常登录 Portal

## 技术细节

### 为什么使用 openssl rand?

```bash
# 32 字节 base64 编码 - 用于密钥
openssl rand -base64 32
# 输出: wQ7xK8v+3j9Lm2N5pR6tY8zB1cD4eF5gH6iJ7kL8mN9o

# 16 字符字母数字 - 用于密码
openssl rand -base64 16 | tr -d '=/+' | cut -c1-16
# 输出: aB3dE6fG9hI2jK5l
```

**优势**:
- ✅ 高熵随机性
- ✅ 加密安全
- ✅ 跨平台兼容 (macOS, Linux)
- ✅ 无需额外依赖

### sed 命令说明

```bash
sed -i.bak "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASSWORD}|" .env.prod
```

- `^REDIS_PASSWORD=.*` - 匹配以 `REDIS_PASSWORD=` 开头的行
- `REDIS_PASSWORD=${REDIS_PASSWORD}` - 替换为新值
- `-i.bak` - 原地修改,创建 `.bak` 备份文件
- 使用 `|` 作为分隔符 (避免与路径中的 `/` 冲突)

## 相关文件

- **修复的文件**: [scripts/deploy/3-server-setup.sh](../scripts/deploy/3-server-setup.sh) (第440-486行)
- **模板文件**: [.env.prod.example](.env.prod.example)
- **部署脚本**: [scripts/deploy/deploy-all.sh](../scripts/deploy/deploy-all.sh)

## 总结

### 问题
`.env.prod` 中的占位符变量 `WILL_BE_GENERATED_ON_SETUP` 未被替换,导致服务连接失败

### 解决方案
修改 `3-server-setup.sh` 脚本,生成所有7个必需的密钥变量

### 影响
- ✅ 首次部署自动生成所有密钥
- ✅ 避免密码不匹配问题
- ✅ 提高安全性 (每次部署使用不同的随机密钥)

### 验证
```bash
# 部署后检查
grep "WILL_BE_GENERATED_ON_SETUP" ~/addp/.env.prod
# 应该无输出

# 检查服务健康
docker compose -f ~/addp/docker-compose.prod.yml ps
# 所有服务应该是 healthy
```
