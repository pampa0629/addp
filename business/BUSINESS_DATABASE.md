# 业务数据库示例 (business_demo)

## 📊 数据库概览

这是一个完全独立的业务数据库示例,模拟电商业务场景,用于测试 ADDP 平台的数据管理和元数据功能。

## 🔌 连接信息

### 基本信息
- **数据库名称**: `business_demo`
- **数据库类型**: PostgreSQL 15
- **主机地址**: `localhost` (开发环境)
- **端口**: `5432`
- **用户名**: `addp`
- **密码**: `addp_password`
- **Schema**: `public` (默认)

### 连接字符串

```bash
# psql 命令行连接
psql -U addp -d business_demo -h localhost -p 5432

# JDBC 连接字符串
jdbc:postgresql://localhost:5432/business_demo?user=addp&password=addp_password

# 标准 PostgreSQL URI
postgresql://addp:addp_password@localhost:5432/business_demo
```

### 在 ADDP 系统中添加此数据源

1. 登录统一门户: http://localhost:5170
2. 进入 **"系统管理" > "存储引擎"**
3. 点击 **"新增资源"**,填写以下信息:

```json
{
  "name": "业务数据库示例",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": "5432",
    "database": "business_demo",
    "username": "addp",
    "password": "addp_password",
    "sslmode": "disable"
  }
}
```

## 📋 数据表结构

### 1. users (用户表)
存储用户基本信息

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| username | VARCHAR(50) | 用户名(唯一) |
| email | VARCHAR(100) | 邮箱(唯一) |
| full_name | VARCHAR(100) | 姓名 |
| phone | VARCHAR(20) | 手机号 |
| age | INTEGER | 年龄 |
| gender | VARCHAR(10) | 性别 |
| status | VARCHAR(20) | 状态(active/inactive) |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |

**数据量**: 8 条记录

### 2. products (商品表)
存储商品信息

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| product_name | VARCHAR(200) | 商品名称 |
| category | VARCHAR(50) | 分类 |
| brand | VARCHAR(50) | 品牌 |
| price | DECIMAL(10,2) | 售价 |
| cost | DECIMAL(10,2) | 成本 |
| stock_quantity | INTEGER | 库存数量 |
| description | TEXT | 商品描述 |
| image_url | VARCHAR(500) | 图片URL |
| status | VARCHAR(20) | 状态 |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |

**数据量**: 12 条记录
**分类**: 手机、笔记本电脑、耳机、平板电脑、智能穿戴、鼠标

### 3. orders (订单表)
存储订单主表信息

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| order_number | VARCHAR(50) | 订单号(唯一) |
| user_id | INTEGER | 用户ID(外键) |
| total_amount | DECIMAL(12,2) | 订单总额 |
| discount_amount | DECIMAL(10,2) | 优惠金额 |
| final_amount | DECIMAL(12,2) | 实付金额 |
| status | VARCHAR(20) | 订单状态 |
| payment_method | VARCHAR(50) | 支付方式 |
| shipping_address | TEXT | 收货地址 |
| order_date | TIMESTAMP | 下单时间 |
| paid_at | TIMESTAMP | 支付时间 |
| shipped_at | TIMESTAMP | 发货时间 |
| completed_at | TIMESTAMP | 完成时间 |

**数据量**: 7 条记录
**状态分布**: pending(1), paid(1), shipped(1), completed(4)

### 4. order_items (订单明细表)
存储订单商品明细

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| order_id | INTEGER | 订单ID(外键) |
| product_id | INTEGER | 商品ID(外键) |
| product_name | VARCHAR(200) | 商品名称 |
| quantity | INTEGER | 数量 |
| unit_price | DECIMAL(10,2) | 单价 |
| subtotal | DECIMAL(12,2) | 小计 |
| created_at | TIMESTAMP | 创建时间 |

**数据量**: 9 条记录

### 5. merchants (商家表)
存储商家信息

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| merchant_name | VARCHAR(100) | 商家名称 |
| contact_person | VARCHAR(50) | 联系人 |
| phone | VARCHAR(20) | 电话 |
| email | VARCHAR(100) | 邮箱 |
| address | TEXT | 地址 |
| business_license | VARCHAR(50) | 营业执照号 |
| status | VARCHAR(20) | 状态 |
| created_at | TIMESTAMP | 创建时间 |

**数据量**: 4 条记录

### 6. inventory_logs (库存流水表)
存储库存变动记录

| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL | 主键 |
| product_id | INTEGER | 商品ID(外键) |
| change_type | VARCHAR(20) | 变动类型(in/out/adjust) |
| quantity | INTEGER | 变动数量 |
| before_stock | INTEGER | 变动前库存 |
| after_stock | INTEGER | 变动后库存 |
| reason | VARCHAR(100) | 变动原因 |
| operator | VARCHAR(50) | 操作人 |
| created_at | TIMESTAMP | 创建时间 |

**数据量**: 9 条记录

## 📊 视图 (Views)

### 1. order_summary (订单汇总视图)
汇总订单和用户信息

```sql
SELECT * FROM order_summary;
```

字段: id, order_number, username, full_name, total_amount, final_amount, status, order_date, item_count

### 2. product_inventory (商品库存统计视图)
统计商品销售和库存信息

```sql
SELECT * FROM product_inventory;
```

字段: id, product_name, category, brand, price, stock_quantity, total_sales, total_quantity_sold, total_revenue

## 📈 数据统计

- **用户数**: 8
- **商品数**: 12
- **订单数**: 7
- **商家数**: 4
- **订单明细**: 9
- **库存流水**: 9

## 🔍 常用查询示例

### 查询所有用户
```sql
SELECT * FROM users WHERE status = 'active';
```

### 查询热销商品
```sql
SELECT
    product_name,
    category,
    brand,
    price,
    total_quantity_sold,
    total_revenue
FROM product_inventory
WHERE total_quantity_sold > 0
ORDER BY total_revenue DESC;
```

### 查询订单详情
```sql
SELECT
    o.order_number,
    u.full_name,
    o.final_amount,
    o.status,
    o.order_date
FROM orders o
JOIN users u ON o.user_id = u.id
ORDER BY o.order_date DESC;
```

### 查询库存预警商品(库存小于30)
```sql
SELECT
    product_name,
    category,
    stock_quantity,
    price
FROM products
WHERE stock_quantity < 30 AND status = 'available'
ORDER BY stock_quantity ASC;
```

## 🔗 表关系

```
users (1) ←→ (N) orders
orders (1) ←→ (N) order_items
products (1) ←→ (N) order_items
products (1) ←→ (N) inventory_logs
```

## 🛠️ 测试场景

这个数据库可以用于测试以下 ADDP 功能:

1. **数据源管理**: 将此数据库添加为数据源
2. **元数据扫描**: 自动扫描表结构、字段类型
3. **数据预览**: 预览表中的数据
4. **数据血缘**: 追踪订单和商品的关系
5. **数据搜索**: 按商品名、用户名等搜索
6. **数据统计**: 统计订单金额、商品销量等

## 🗑️ 清理数据库

如果需要删除此测试数据库:

```bash
# 方法1: 使用 dropdb 命令
dropdb -U addp business_demo

# 方法2: 使用 psql
psql -U addp -d postgres -c "DROP DATABASE business_demo;"
```

## 📝 备份与恢复

### 备份
```bash
pg_dump -U addp business_demo > business_demo_backup.sql
```

### 恢复
```bash
psql -U addp -d business_demo < business_demo_backup.sql
```
