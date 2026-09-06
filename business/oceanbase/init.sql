SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS addp_engine_probe (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO addp_engine_probe (id, name)
VALUES (1, 'OceanBase Community Edition')
ON DUPLICATE KEY UPDATE name = VALUES(name);

CREATE TABLE IF NOT EXISTS customers (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_code VARCHAR(32) NOT NULL,
  name VARCHAR(80) NOT NULL,
  gender VARCHAR(16) NOT NULL DEFAULT 'unknown',
  email VARCHAR(120) NOT NULL,
  phone VARCHAR(32),
  city VARCHAR(64),
  membership_level VARCHAR(32) NOT NULL DEFAULT 'standard',
  points INT NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT uk_customers_code UNIQUE (customer_code),
  INDEX idx_customers_city (city),
  INDEX idx_customers_membership (membership_level)
);

CREATE TABLE IF NOT EXISTS products (
  id BIGINT NOT NULL PRIMARY KEY,
  sku VARCHAR(32) NOT NULL,
  name VARCHAR(120) NOT NULL,
  category VARCHAR(64) NOT NULL,
  price DECIMAL(12, 2) NOT NULL,
  stock INT NOT NULL DEFAULT 0,
  attributes JSON,
  on_sale TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT uk_products_sku UNIQUE (sku),
  INDEX idx_products_category (category)
);

CREATE TABLE IF NOT EXISTS orders (
  id BIGINT NOT NULL PRIMARY KEY,
  order_no VARCHAR(40) NOT NULL,
  customer_id BIGINT NOT NULL,
  status VARCHAR(32) NOT NULL,
  total_amount DECIMAL(12, 2) NOT NULL,
  payment_method VARCHAR(32),
  ordered_at DATETIME(6) NOT NULL,
  shipped_at DATETIME(6),
  shipping_address VARCHAR(255),
  CONSTRAINT uk_orders_no UNIQUE (order_no),
  CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES customers(id),
  INDEX idx_orders_customer (customer_id),
  INDEX idx_orders_status (status),
  INDEX idx_orders_ordered_at (ordered_at)
);

CREATE TABLE IF NOT EXISTS order_items (
  id BIGINT NOT NULL PRIMARY KEY,
  order_id BIGINT NOT NULL,
  product_id BIGINT NOT NULL,
  quantity INT NOT NULL,
  unit_price DECIMAL(12, 2) NOT NULL,
  discount_amount DECIMAL(12, 2) NOT NULL DEFAULT 0,
  CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id),
  CONSTRAINT fk_order_items_product FOREIGN KEY (product_id) REFERENCES products(id),
  INDEX idx_order_items_order (order_id),
  INDEX idx_order_items_product (product_id)
);

INSERT INTO customers (
  id, customer_code, name, gender, email, phone, city,
  membership_level, points, active, created_at, updated_at
) VALUES
  (1, 'CUST-10001', '王小丽', 'female', 'alice.wang@example.com', '13800010001', '上海', 'gold', 5800, 1, '2024-01-15 10:00:00.000000', '2026-04-20 09:30:00.000000'),
  (2, 'CUST-10002', '陈大明', 'male', 'bob.chen@example.com', '13800010002', '北京', 'platinum', 12500, 1, '2023-06-20 11:20:00.000000', '2026-04-18 16:45:00.000000'),
  (3, 'CUST-10003', '李佳佳', 'female', 'carol.li@example.com', '13800010003', '深圳', 'silver', 2300, 1, '2024-11-01 14:10:00.000000', '2026-04-15 12:00:00.000000'),
  (4, 'CUST-10004', '赵一鸣', 'male', 'zhao.yiming@example.com', '13800010004', '杭州', 'standard', 860, 0, '2025-03-08 08:40:00.000000', '2026-03-30 19:10:00.000000'),
  (5, 'CUST-10005', '孙晓雨', 'female', 'sun.xiaoyu@example.com', '13800010005', '成都', 'gold', 7200, 1, '2025-08-12 17:25:00.000000', '2026-04-21 10:05:00.000000')
ON DUPLICATE KEY UPDATE
  customer_code = VALUES(customer_code),
  name = VALUES(name),
  gender = VALUES(gender),
  email = VALUES(email),
  phone = VALUES(phone),
  city = VALUES(city),
  membership_level = VALUES(membership_level),
  points = VALUES(points),
  active = VALUES(active),
  created_at = VALUES(created_at),
  updated_at = VALUES(updated_at);

INSERT INTO products (
  id, sku, name, category, price, stock, attributes, on_sale, created_at
) VALUES
  (1, 'SKU-MBP-16', 'MacBook Pro 16', '笔记本电脑', 16999.00, 45, JSON_OBJECT('cpu', 'M3 Max', 'ram', '64GB', 'storage', '2TB SSD'), 1, '2025-12-01 09:00:00.000000'),
  (2, 'SKU-KEY-87', 'Magic Keyboard', '键盘', 1299.00, 180, JSON_OBJECT('layout', '87键', 'wireless', TRUE, 'backlight', FALSE), 1, '2025-12-05 09:00:00.000000'),
  (3, 'SKU-MOU-01', 'Magic Mouse 2', '鼠标', 799.00, 130, JSON_OBJECT('dpi', 4000, 'wireless', TRUE, 'charging', 'Lightning'), 1, '2025-12-10 09:00:00.000000'),
  (4, 'SKU-HUB-C8', 'USB-C Hub 8合1', '扩展坞', 399.00, 260, JSON_OBJECT('ports', 8, 'hdmi', TRUE, 'ethernet', TRUE), 1, '2026-01-15 09:00:00.000000'),
  (5, 'SKU-MON-27', 'Studio Display 27', '显示器', 11499.00, 28, JSON_OBJECT('size', '27英寸', 'resolution', '5K'), 1, '2026-02-01 09:00:00.000000')
ON DUPLICATE KEY UPDATE
  sku = VALUES(sku),
  name = VALUES(name),
  category = VALUES(category),
  price = VALUES(price),
  stock = VALUES(stock),
  attributes = VALUES(attributes),
  on_sale = VALUES(on_sale),
  created_at = VALUES(created_at);

INSERT INTO orders (
  id, order_no, customer_id, status, total_amount, payment_method,
  ordered_at, shipped_at, shipping_address
) VALUES
  (1, 'ORD-20260420-001', 1, 'delivered', 2897.00, 'alipay', '2026-04-20 10:12:00.000000', '2026-04-21 15:30:00.000000', '上海市浦东新区张江高科技园区'),
  (2, 'ORD-20260421-002', 2, 'processing', 16999.00, 'wechat', '2026-04-21 14:05:00.000000', NULL, '北京市海淀区中关村软件园'),
  (3, 'ORD-20260422-003', 3, 'paid', 798.00, 'credit_card', '2026-04-22 18:35:00.000000', NULL, '深圳市南山区科技园'),
  (4, 'ORD-20260423-004', 5, 'delivered', 11499.00, 'alipay', '2026-04-23 09:18:00.000000', '2026-04-24 11:40:00.000000', '成都市高新区天府大道')
ON DUPLICATE KEY UPDATE
  order_no = VALUES(order_no),
  customer_id = VALUES(customer_id),
  status = VALUES(status),
  total_amount = VALUES(total_amount),
  payment_method = VALUES(payment_method),
  ordered_at = VALUES(ordered_at),
  shipped_at = VALUES(shipped_at),
  shipping_address = VALUES(shipping_address);

INSERT INTO order_items (
  id, order_id, product_id, quantity, unit_price, discount_amount
) VALUES
  (1, 1, 2, 1, 1299.00, 0.00),
  (2, 1, 3, 2, 799.00, 0.00),
  (3, 2, 1, 1, 16999.00, 0.00),
  (4, 3, 4, 2, 399.00, 0.00),
  (5, 4, 5, 1, 11499.00, 0.00)
ON DUPLICATE KEY UPDATE
  order_id = VALUES(order_id),
  product_id = VALUES(product_id),
  quantity = VALUES(quantity),
  unit_price = VALUES(unit_price),
  discount_amount = VALUES(discount_amount);
