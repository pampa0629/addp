CREATE TABLE IF NOT EXISTS addp_engine_probe (
    id BIGINT PRIMARY KEY,
    engine_name VARCHAR(64) NOT NULL,
    initialized_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO addp_engine_probe (id, engine_name)
SELECT 1, 'openGauss'
WHERE NOT EXISTS (SELECT 1 FROM addp_engine_probe WHERE id = 1);

CREATE TABLE IF NOT EXISTS customers (
    id BIGINT PRIMARY KEY,
    customer_code VARCHAR(32) NOT NULL UNIQUE,
    customer_name VARCHAR(128) NOT NULL,
    city VARCHAR(64) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS products (
    id BIGINT PRIMARY KEY,
    sku VARCHAR(32) NOT NULL UNIQUE,
    product_name VARCHAR(128) NOT NULL,
    category VARCHAR(64) NOT NULL,
    unit_price NUMERIC(12, 2) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
    id BIGINT PRIMARY KEY,
    order_no VARCHAR(32) NOT NULL UNIQUE,
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    order_status VARCHAR(24) NOT NULL,
    total_amount NUMERIC(14, 2) NOT NULL,
    ordered_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_updated_at ON orders(updated_at, id);

CREATE TABLE IF NOT EXISTS order_items (
    id BIGINT PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id),
    product_id BIGINT NOT NULL REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price NUMERIC(12, 2) NOT NULL,
    UNIQUE (order_id, product_id)
);

INSERT INTO customers (id, customer_code, customer_name, city, active, created_at)
SELECT 1, 'C001', '长沙星城研究中心', '长沙', TRUE, TIMESTAMP '2025-01-02 09:00:00'
WHERE NOT EXISTS (SELECT 1 FROM customers WHERE id = 1);
INSERT INTO customers (id, customer_code, customer_name, city, active, created_at)
SELECT 2, 'C002', '武汉江城实验室', '武汉', TRUE, TIMESTAMP '2025-01-03 10:00:00'
WHERE NOT EXISTS (SELECT 1 FROM customers WHERE id = 2);
INSERT INTO customers (id, customer_code, customer_name, city, active, created_at)
SELECT 3, 'C003', '西安长安数据中心', '西安', TRUE, TIMESTAMP '2025-01-04 11:00:00'
WHERE NOT EXISTS (SELECT 1 FROM customers WHERE id = 3);

INSERT INTO products (id, sku, product_name, category, unit_price, active, created_at)
SELECT 1, 'SKU-001', '数据治理基础包', 'software', 1999.00, TRUE, TIMESTAMP '2025-01-05 09:00:00'
WHERE NOT EXISTS (SELECT 1 FROM products WHERE id = 1);
INSERT INTO products (id, sku, product_name, category, unit_price, active, created_at)
SELECT 2, 'SKU-002', '数据质量评估服务', 'service', 899.50, TRUE, TIMESTAMP '2025-01-06 09:00:00'
WHERE NOT EXISTS (SELECT 1 FROM products WHERE id = 2);
INSERT INTO products (id, sku, product_name, category, unit_price, active, created_at)
SELECT 3, 'SKU-003', '元数据采集连接器', 'software', 1299.00, TRUE, TIMESTAMP '2025-01-07 09:00:00'
WHERE NOT EXISTS (SELECT 1 FROM products WHERE id = 3);

INSERT INTO orders (id, order_no, customer_id, order_status, total_amount, ordered_at, updated_at)
SELECT 1, 'OG-2025-0001', 1, 'paid', 2898.50, TIMESTAMP '2025-02-01 10:15:00', TIMESTAMP '2025-02-01 10:20:00'
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = 1);
INSERT INTO orders (id, order_no, customer_id, order_status, total_amount, ordered_at, updated_at)
SELECT 2, 'OG-2025-0002', 2, 'processing', 2598.00, TIMESTAMP '2025-02-02 11:30:00', TIMESTAMP '2025-02-02 12:00:00'
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = 2);
INSERT INTO orders (id, order_no, customer_id, order_status, total_amount, ordered_at, updated_at)
SELECT 3, 'OG-2025-0003', 3, 'completed', 1299.00, TIMESTAMP '2025-02-03 14:00:00', TIMESTAMP '2025-02-04 09:00:00'
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = 3);

INSERT INTO order_items (id, order_id, product_id, quantity, unit_price)
SELECT 1, 1, 1, 1, 1999.00
WHERE NOT EXISTS (SELECT 1 FROM order_items WHERE id = 1);
INSERT INTO order_items (id, order_id, product_id, quantity, unit_price)
SELECT 2, 1, 2, 1, 899.50
WHERE NOT EXISTS (SELECT 1 FROM order_items WHERE id = 2);
INSERT INTO order_items (id, order_id, product_id, quantity, unit_price)
SELECT 3, 2, 3, 2, 1299.00
WHERE NOT EXISTS (SELECT 1 FROM order_items WHERE id = 3);
INSERT INTO order_items (id, order_id, product_id, quantity, unit_price)
SELECT 4, 3, 3, 1, 1299.00
WHERE NOT EXISTS (SELECT 1 FROM order_items WHERE id = 4);
