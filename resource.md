选择类型: PostgreSQL
    - 填写连接信息:
    名称: 系统数据库
  主机地址: localhost
  端口: 5432
  数据库名: addp
  用户名: addp
  密码: addp_password
  SSL模式: 禁用 (disable)


PostgreSQL 实例1 (ADDP系统库) - localhost:5432
     ├── system schema (用户、资源、日志)
     ├── manager schema (目录、纳管表)
     ├── metadata schema (数据集、字段、血缘)
     └── transfer schema (任务执行)


名称: 业务数据库
  主机地址: localhost
  端口: 5433
  数据库名: business
  用户名: business
  密码: business_password
  SSL模式: 禁用 (disable)


  ✅ PostgreSQL 实例2 (业务数据库) - localhost:5433
     ├── public.users_demo (5条记录)
     ├── public.products (8条记录)
     └── public.orders (5条记录)


  选择类型: MinIO
    - 填写连接信息:
    名称: 对象存储
  端点地址: localhost:9000
  Access Key: minioadmin
  Secret Key: minioadmin
  使用SSL: 关闭



⏺ 安全审计结果

  ✅ 用户密码已加密

  当前实现 (system/backend/pkg/utils/password.go:5-12):
  - 使用 bcrypt 加密算法
  - 成本因子: bcrypt.DefaultCost (10)
  - 所有用户密码都已正确加密存储

  数据库验证:
  zuhu1:        $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
  SuperAdmin:   $2a$10$UJvKh/XXObz7YPQpQvkDTuBYD8J4R3zoDWrV1v9RRf1f2.FEOaer2
  zuhu3:        $2a$10$2uGEVQXV0Xy0/ALNxi1nQuXGA.4y6rrfeuJmxOJtnIz7w4kU4o3Ta

  bcrypt 特点:
  - 自动加盐 (salt)
  - 不可逆哈希
  - 抗暴力破解 (计算成本高)
  - 业界标准,广泛使用

  ❌ 资源连接信息未加密

  当前状态:
  {
    "host": "localhost",
    "port": 5433,
    "user": "business",
    "password": "business_password",  ← 明文存储!
    "database": "business",
    "resource_type": "postgresql"
  }

  业务数据库密码以 明文 存储在 system.resources.connection_info JSON 字段中。

  安全建议

  资源连接信息加密方案

  推荐使用 AES-256-GCM 对称加密:

  优势:
  - 强加密强度 (256位密钥)
  - 认证加密 (GCM模式防篡改)
  - 加密后可解密 (需要实际连接数据库)
  - Go标准库支持 (crypto/aes, crypto/cipher)

  实现方案:
  1. 生成加密密钥存储在环境变量 (如 ENCRYPTION_KEY)
  2. 在保存 connection_info 前加密敏感字段 (password, access_key 等)
  3. 使用时解密后连接数据库
  4. 密钥通过密钥管理服务管理 (生产环境可用 Vault/AWS KMS)

  需要实现吗? 我可以添加加密工具函数和更新 resource service。