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

数据库类型: PostgreSQL 15
连接地址: localhost:5432
数据库名: addp
  用户名: addp
  密码: addp_password


  主机 (host): localhost
  端口 (port): 5433  ← 独立端口
  数据库名 (database): business
  用户名 (username): business_user
  密码 (password): business_password
  Docker容器: postgres-business


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


