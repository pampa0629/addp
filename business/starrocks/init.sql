CREATE DATABASE IF NOT EXISTS business;
CREATE TABLE IF NOT EXISTS business.source (
  name VARCHAR(128),
  id BIGINT,
  amount DECIMAL(38,18),
  optional_text VARCHAR(128)
) PRIMARY KEY(name,id) DISTRIBUTED BY HASH(name) BUCKETS 1
PROPERTIES('replication_num'='1');
INSERT INTO business.source
SELECT '正式接入中文',9007199254740993,12345678901234567890.123456789012345678,NULL
WHERE NOT EXISTS (SELECT 1 FROM business.source WHERE name='正式接入中文' AND id=9007199254740993);
INSERT INTO business.source
SELECT '保留尾空格  ',2,-0.000000000000000001,''
WHERE NOT EXISTS (SELECT 1 FROM business.source WHERE name='保留尾空格  ' AND id=2);

-- 业务示范：客户、商品、订单。固定主键只补缺失记录，保留用户修改。
CREATE TABLE IF NOT EXISTS business.customers (
  customer_id BIGINT,
  customer_name VARCHAR(128),
  city VARCHAR(64),
  joined_date DATE,
  vip BOOLEAN,
  email VARCHAR(128)
) PRIMARY KEY(customer_id) DISTRIBUTED BY HASH(customer_id) BUCKETS 1
PROPERTIES('replication_num'='1');
INSERT INTO business.customers (customer_id,customer_name,city,joined_date,vip,email)
SELECT 1001,'湘江环境监测中心','长沙','2026-01-15',TRUE,'xiangjiang@example.invalid'
WHERE NOT EXISTS (SELECT 1 FROM business.customers WHERE customer_id=1001);
INSERT INTO business.customers (customer_id,customer_name,city,joined_date,vip,email)
SELECT 1002,'株洲智慧园区','株洲','2026-02-20',FALSE,'zhuzhou@example.invalid'
WHERE NOT EXISTS (SELECT 1 FROM business.customers WHERE customer_id=1002);
INSERT INTO business.customers (customer_id,customer_name,city,joined_date,vip,email)
SELECT 1003,'湘潭城市运营中心','湘潭','2026-03-08',TRUE,NULL
WHERE NOT EXISTS (SELECT 1 FROM business.customers WHERE customer_id=1003);
INSERT INTO business.customers (customer_id,customer_name,city,joined_date,vip,email)
SELECT 1004,'洞庭湖生态研究站','岳阳','2026-04-12',TRUE,'dongting@example.invalid'
WHERE NOT EXISTS (SELECT 1 FROM business.customers WHERE customer_id=1004);
INSERT INTO business.customers (customer_id,customer_name,city,joined_date,vip,email)
SELECT 1005,'常德农业示范基地','常德','2026-05-06',FALSE,'changde@example.invalid'
WHERE NOT EXISTS (SELECT 1 FROM business.customers WHERE customer_id=1005);

CREATE TABLE IF NOT EXISTS business.products (
  product_id BIGINT,
  product_name VARCHAR(128),
  category VARCHAR(64),
  unit_price DECIMAL(12,2),
  active BOOLEAN
) PRIMARY KEY(product_id) DISTRIBUTED BY HASH(product_id) BUCKETS 1
PROPERTIES('replication_num'='1');
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2001,'工业温度传感器','物联网设备',299.00,TRUE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2001);
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2002,'空气质量监测仪','物联网设备',1299.50,TRUE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2002);
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2003,'边缘计算网关','物联网设备',2499.00,TRUE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2003);
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2004,'无人机航测服务','空间数据服务',5800.00,TRUE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2004);
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2005,'遥感影像数据包','空间数据服务',699.90,TRUE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2005);
INSERT INTO business.products (product_id,product_name,category,unit_price,active)
SELECT 2006,'年度数据分析服务','分析服务',12000.00,FALSE
WHERE NOT EXISTS (SELECT 1 FROM business.products WHERE product_id=2006);

CREATE TABLE IF NOT EXISTS business.orders (
  order_id BIGINT,
  customer_id BIGINT,
  product_id BIGINT,
  order_time DATETIME,
  quantity INT,
  unit_price DECIMAL(12,2),
  order_amount DECIMAL(14,2),
  status VARCHAR(32),
  remark VARCHAR(256)
) PRIMARY KEY(order_id) DISTRIBUTED BY HASH(order_id) BUCKETS 1
PROPERTIES('replication_num'='1');
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3001,1001,2001,'2026-10-01 10:00:00',10,299.00,2990.00,'已完成','首批监测设备'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3001);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3002,1002,2002,'2026-10-02 10:00:00',3,1299.50,3898.50,'已完成',NULL
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3002);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3003,1003,2003,'2026-10-03 10:00:00',2,2499.00,4998.00,'已付款','分两批交付'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3003);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3004,1004,2004,'2026-10-04 10:00:00',1,5800.00,5800.00,'已完成','湖区航测'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3004);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3005,1005,2005,'2026-10-05 10:00:00',4,699.90,2799.60,'待付款',''
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3005);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3006,1001,2006,'2026-10-06 10:00:00',1,12000.00,12000.00,'已付款','年度分析订阅'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3006);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3007,1002,2001,'2026-10-07 10:00:00',5,299.00,1495.00,'已完成','追加采购'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3007);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3008,1003,2005,'2026-10-08 10:00:00',2,699.90,1399.80,'已取消','调整采购计划'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3008);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3009,1004,2003,'2026-10-09 10:00:00',1,2499.00,2499.00,'已付款','湖区网关升级'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3009);
INSERT INTO business.orders (order_id,customer_id,product_id,order_time,quantity,unit_price,order_amount,status,remark)
SELECT 3010,1005,2002,'2026-10-10 10:00:00',1,1299.50,1299.50,'已完成','农业环境监测'
WHERE NOT EXISTS (SELECT 1 FROM business.orders WHERE order_id=3010);
