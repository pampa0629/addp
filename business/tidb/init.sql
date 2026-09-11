SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS addp_engine_probe (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO addp_engine_probe (id, name)
VALUES (1, 'TiDB 8.5.8')
ON DUPLICATE KEY UPDATE name = VALUES(name);

CREATE TABLE IF NOT EXISTS customers (
  id BIGINT NOT NULL PRIMARY KEY,
  customer_code VARCHAR(32) NOT NULL,
  name VARCHAR(80) NOT NULL,
  city VARCHAR(64),
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT uk_customers_code UNIQUE (customer_code),
  INDEX idx_customers_city (city)
);

CREATE TABLE IF NOT EXISTS orders (
  id BIGINT NOT NULL PRIMARY KEY,
  order_no VARCHAR(40) NOT NULL,
  customer_id BIGINT NOT NULL,
  status VARCHAR(32) NOT NULL,
  total_amount DECIMAL(12, 2) NOT NULL,
  ordered_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT uk_orders_no UNIQUE (order_no),
  INDEX idx_orders_customer (customer_id),
  INDEX idx_orders_updated_at (updated_at)
);

INSERT INTO customers (id, customer_code, name, city, active, created_at, updated_at) VALUES
  (1, 'TIDB-CUST-1001', '王小丽', '上海', 1, '2026-01-15 10:00:00.000001', '2026-09-01 08:00:00.000001'),
  (2, 'TIDB-CUST-1002', '陈大明', '北京', 1, '2026-02-20 11:20:00.000002', '2026-09-01 08:00:00.000002'),
  (3, 'TIDB-CUST-1003', '李佳佳', '深圳', 1, '2026-03-01 14:10:00.000003', '2026-09-01 08:00:00.000003')
ON DUPLICATE KEY UPDATE
  customer_code = VALUES(customer_code),
  name = VALUES(name),
  city = VALUES(city),
  active = VALUES(active),
  created_at = VALUES(created_at),
  updated_at = VALUES(updated_at);

INSERT INTO orders (id, order_no, customer_id, status, total_amount, ordered_at, updated_at) VALUES
  (1, 'TIDB-ORD-1001', 1, 'delivered', 88.50, '2026-09-01 09:00:00.000001', '2026-09-01 09:00:00.000001'),
  (2, 'TIDB-ORD-1002', 2, 'processing', 1699.00, '2026-09-02 10:30:00.000002', '2026-09-02 10:30:00.000002'),
  (3, 'TIDB-ORD-1003', 3, 'paid', 399.00, '2026-09-03 12:45:00.000003', '2026-09-03 12:45:00.000003')
ON DUPLICATE KEY UPDATE
  order_no = VALUES(order_no),
  customer_id = VALUES(customer_id),
  status = VALUES(status),
  total_amount = VALUES(total_amount),
  ordered_at = VALUES(ordered_at),
  updated_at = VALUES(updated_at);
