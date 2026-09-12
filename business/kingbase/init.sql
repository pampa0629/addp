CREATE TABLE IF NOT EXISTS addp_engine_probe (
    id BIGINT PRIMARY KEY,
    engine_name VARCHAR(64) NOT NULL,
    initialized_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO addp_engine_probe (id, engine_name)
VALUES (1, 'KingbaseES V9R1C10')
ON CONFLICT (id) DO UPDATE SET engine_name = EXCLUDED.engine_name;

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

CREATE INDEX IF NOT EXISTS idx_kingbase_orders_customer_id ON orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_kingbase_orders_updated_at ON orders(updated_at, id);

CREATE TABLE IF NOT EXISTS order_items (
    id BIGINT PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id),
    product_id BIGINT NOT NULL REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price NUMERIC(12, 2) NOT NULL,
    UNIQUE (order_id, product_id)
);

INSERT INTO customers (id, customer_code, customer_name, city, active, created_at) VALUES
    (1, 'KB-C001', '长沙星城研究中心', '长沙', TRUE, TIMESTAMP '2026-01-02 09:00:00'),
    (2, 'KB-C002', '武汉江城实验室', '武汉', TRUE, TIMESTAMP '2026-01-03 10:00:00'),
    (3, 'KB-C003', '西安长安数据中心', '西安', TRUE, TIMESTAMP '2026-01-04 11:00:00')
ON CONFLICT (id) DO UPDATE SET
    customer_code = EXCLUDED.customer_code,
    customer_name = EXCLUDED.customer_name,
    city = EXCLUDED.city,
    active = EXCLUDED.active,
    created_at = EXCLUDED.created_at;

INSERT INTO products (id, sku, product_name, category, unit_price, active, created_at) VALUES
    (1, 'KB-SKU-001', '数据治理基础包', 'software', 1999.00, TRUE, TIMESTAMP '2026-01-05 09:00:00'),
    (2, 'KB-SKU-002', '数据质量评估服务', 'service', 899.50, TRUE, TIMESTAMP '2026-01-06 09:00:00'),
    (3, 'KB-SKU-003', '元数据采集连接器', 'software', 1299.00, TRUE, TIMESTAMP '2026-01-07 09:00:00')
ON CONFLICT (id) DO UPDATE SET
    sku = EXCLUDED.sku,
    product_name = EXCLUDED.product_name,
    category = EXCLUDED.category,
    unit_price = EXCLUDED.unit_price,
    active = EXCLUDED.active,
    created_at = EXCLUDED.created_at;

INSERT INTO orders (id, order_no, customer_id, order_status, total_amount, ordered_at, updated_at) VALUES
    (1, 'KB-2026-0001', 1, 'paid', 2898.50, TIMESTAMP '2026-02-01 10:15:00', TIMESTAMP '2026-02-01 10:20:00'),
    (2, 'KB-2026-0002', 2, 'processing', 2598.00, TIMESTAMP '2026-02-02 11:30:00', TIMESTAMP '2026-02-02 12:00:00'),
    (3, 'KB-2026-0003', 3, 'completed', 1299.00, TIMESTAMP '2026-02-03 14:00:00', TIMESTAMP '2026-02-04 09:00:00')
ON CONFLICT (id) DO UPDATE SET
    order_no = EXCLUDED.order_no,
    customer_id = EXCLUDED.customer_id,
    order_status = EXCLUDED.order_status,
    total_amount = EXCLUDED.total_amount,
    ordered_at = EXCLUDED.ordered_at,
    updated_at = EXCLUDED.updated_at;

INSERT INTO order_items (id, order_id, product_id, quantity, unit_price) VALUES
    (1, 1, 1, 1, 1999.00),
    (2, 1, 2, 1, 899.50),
    (3, 2, 3, 2, 1299.00),
    (4, 3, 3, 1, 1299.00)
ON CONFLICT (id) DO UPDATE SET
    order_id = EXCLUDED.order_id,
    product_id = EXCLUDED.product_id,
    quantity = EXCLUDED.quantity,
    unit_price = EXCLUDED.unit_price;
