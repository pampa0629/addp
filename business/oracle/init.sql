-- Oracle 普通表与 Spatial 样例数据。该脚本由 test-data.sh 以 APP_USER 执行，保持幂等。
WHENEVER SQLERROR EXIT SQL.SQLCODE

DECLARE
  spatial_type_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO spatial_type_count
    FROM all_types
   WHERE owner = 'MDSYS'
     AND type_name = 'SDO_GEOMETRY';
  IF spatial_type_count <> 1 THEN
    RAISE_APPLICATION_ERROR(-20001, 'Oracle Spatial is unavailable; use gvenzl/oracle-free:23 instead of a -slim image and recreate the Oracle data volume');
  END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE TABLE customers (
    id NUMBER(10,0) CONSTRAINT customers_pk PRIMARY KEY,
    customer_code VARCHAR2(32 CHAR) CONSTRAINT customers_code_uq UNIQUE NOT NULL,
    name VARCHAR2(120 CHAR) NOT NULL,
    active NUMBER(1,0) DEFAULT 1 NOT NULL,
    created_at DATE NOT NULL
  )';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

DECLARE
  all_column_logging_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO all_column_logging_count
    FROM user_log_groups
   WHERE table_name = 'CUSTOMERS'
     AND log_group_type = 'ALL COLUMN LOGGING';
  IF all_column_logging_count = 0 THEN
    EXECUTE IMMEDIATE 'ALTER TABLE customers ADD SUPPLEMENTAL LOG DATA (ALL) COLUMNS';
  END IF;
END;
/

DECLARE
  constraint_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO constraint_count
    FROM user_constraints
   WHERE constraint_name = 'CUSTOMERS_CODE_UQ';
  IF constraint_count = 0 THEN
    EXECUTE IMMEDIATE 'ALTER TABLE customers ADD CONSTRAINT customers_code_uq UNIQUE (customer_code)';
  END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE TABLE order_events (
    id NUMBER(10,0) NOT NULL,
    event_time DATE NOT NULL,
    event_type VARCHAR2(40 CHAR) NOT NULL,
    payload CLOB,
    CONSTRAINT order_events_pk PRIMARY KEY (id, event_time)
  )
  PARTITION BY RANGE (event_time) (
    PARTITION order_events_2026_q1 VALUES LESS THAN (DATE ''2026-04-01''),
    PARTITION order_events_max VALUES LESS THAN (MAXVALUE)
  )';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE TABLE orders (
    id NUMBER(10,0) CONSTRAINT orders_pk PRIMARY KEY,
    order_no VARCHAR2(40 CHAR) NOT NULL,
    customer_id NUMBER(10,0) NOT NULL,
    amount NUMBER(12,2) NOT NULL,
    ordered_at TIMESTAMP(6) NOT NULL,
    notes CLOB,
    payload BLOB,
    CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES customers(id)
  )';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE TABLE customer_locations (
    id NUMBER(10,0) CONSTRAINT customer_locations_pk PRIMARY KEY,
    name VARCHAR2(120 CHAR) NOT NULL,
    shape MDSYS.SDO_GEOMETRY NOT NULL
  )';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE TABLE spatial_features (
    id NUMBER(10,0) CONSTRAINT spatial_features_pk PRIMARY KEY,
    geometry_type VARCHAR2(40 CHAR) NOT NULL,
    shape MDSYS.SDO_GEOMETRY NOT NULL
  )';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

DECLARE
  index_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO index_count
    FROM user_indexes
   WHERE index_name = 'ORDERS_ORDERED_AT_IDX';
  IF index_count = 0 THEN
    EXECUTE IMMEDIATE 'CREATE INDEX orders_ordered_at_idx ON orders (ordered_at)';
  END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE 'CREATE OR REPLACE VIEW order_summary AS
    SELECT o.order_no, c.customer_code, c.name AS customer_name, o.amount, o.ordered_at
      FROM orders o JOIN customers c ON c.id = o.customer_id';
END;
/

MERGE INTO customers target
USING (SELECT 1 id, 'CUST-001' customer_code, 'ADDP Demo' name, 1 active, DATE '2026-01-15' created_at FROM dual) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.customer_code = source.customer_code, target.name = source.name, target.active = source.active
WHEN NOT MATCHED THEN INSERT (id, customer_code, name, active, created_at)
VALUES (source.id, source.customer_code, source.name, source.active, source.created_at);

MERGE INTO customers target
USING (SELECT 2 id, 'CUST-002' customer_code, 'Oracle Reader' name, 1 active, DATE '2026-02-20' created_at FROM dual) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.customer_code = source.customer_code, target.name = source.name, target.active = source.active
WHEN NOT MATCHED THEN INSERT (id, customer_code, name, active, created_at)
VALUES (source.id, source.customer_code, source.name, source.active, source.created_at);

MERGE INTO orders target
USING (SELECT 1001 id, 'ORD-1001' order_no, 1 customer_id, 1299.50 amount, TIMESTAMP '2026-03-01 09:30:00' ordered_at, 'first order' notes FROM dual) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.order_no = source.order_no, target.customer_id = source.customer_id, target.amount = source.amount, target.ordered_at = source.ordered_at, target.notes = source.notes
WHEN NOT MATCHED THEN INSERT (id, order_no, customer_id, amount, ordered_at, notes)
VALUES (source.id, source.order_no, source.customer_id, source.amount, source.ordered_at, source.notes);

MERGE INTO customer_locations target
USING (
  SELECT 1 id,
         'ADDP Demo Campus' name,
         MDSYS.SDO_GEOMETRY(2001, 4326, MDSYS.SDO_POINT_TYPE(116.397, 39.908, NULL), NULL, NULL) shape
    FROM dual
) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.name = source.name, target.shape = source.shape
WHEN NOT MATCHED THEN INSERT (id, name, shape)
VALUES (source.id, source.name, source.shape);

DECLARE
  PROCEDURE upsert_spatial_feature(feature_id NUMBER, feature_type VARCHAR2, feature_wkt VARCHAR2) IS
    geometry MDSYS.SDO_GEOMETRY;
  BEGIN
    geometry := SDO_UTIL.FROM_WKTGEOMETRY(feature_wkt);
    geometry.sdo_srid := 4326;
    MERGE INTO spatial_features target
    USING (SELECT feature_id id, feature_type geometry_type, geometry shape FROM dual) source
       ON (target.id = source.id)
     WHEN MATCHED THEN UPDATE SET target.geometry_type = source.geometry_type, target.shape = source.shape
     WHEN NOT MATCHED THEN INSERT (id, geometry_type, shape)
          VALUES (source.id, source.geometry_type, source.shape);
  END;
BEGIN
  upsert_spatial_feature(1, 'LineString', 'LINESTRING (116.35 39.90, 116.45 39.95)');
  upsert_spatial_feature(2, 'Polygon', 'POLYGON ((116.30 39.85, 116.30 39.95, 116.40 39.95, 116.40 39.85, 116.30 39.85))');
  upsert_spatial_feature(3, 'MultiPoint', 'MULTIPOINT ((116.30 39.80), (116.50 40.00))');
  upsert_spatial_feature(4, 'MultiLineString', 'MULTILINESTRING ((116.20 39.80, 116.30 39.90), (116.40 39.90, 116.50 40.00))');
  upsert_spatial_feature(5, 'MultiPolygon', 'MULTIPOLYGON (((116.10 39.70, 116.10 39.75, 116.15 39.75, 116.15 39.70, 116.10 39.70)), ((116.50 40.00, 116.50 40.05, 116.55 40.05, 116.55 40.00, 116.50 40.00)))');
  upsert_spatial_feature(6, 'GeometryCollection', 'GEOMETRYCOLLECTION (POINT (116.397 39.908), LINESTRING (116.35 39.90, 116.45 39.95))');
END;
/

MERGE INTO customer_locations target
USING (
  SELECT 2 id,
         'Oracle Spatial Lab' name,
         MDSYS.SDO_GEOMETRY(2001, 4326, MDSYS.SDO_POINT_TYPE(121.474, 31.230, NULL), NULL, NULL) shape
    FROM dual
) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.name = source.name, target.shape = source.shape
WHEN NOT MATCHED THEN INSERT (id, name, shape)
VALUES (source.id, source.name, source.shape);

DELETE FROM user_sdo_geom_metadata
 WHERE table_name = 'CUSTOMER_LOCATIONS'
   AND column_name = 'SHAPE';

INSERT INTO user_sdo_geom_metadata (table_name, column_name, diminfo, srid)
VALUES (
  'CUSTOMER_LOCATIONS',
  'SHAPE',
  MDSYS.SDO_DIM_ARRAY(
    MDSYS.SDO_DIM_ELEMENT('LONGITUDE', -180, 180, 0.005),
    MDSYS.SDO_DIM_ELEMENT('LATITUDE', -90, 90, 0.005)
  ),
  4326
);

DELETE FROM user_sdo_geom_metadata
 WHERE table_name = 'SPATIAL_FEATURES'
   AND column_name = 'SHAPE';

INSERT INTO user_sdo_geom_metadata (table_name, column_name, diminfo, srid)
VALUES (
  'SPATIAL_FEATURES',
  'SHAPE',
  MDSYS.SDO_DIM_ARRAY(
    MDSYS.SDO_DIM_ELEMENT('LONGITUDE', -180, 180, 0.005),
    MDSYS.SDO_DIM_ELEMENT('LATITUDE', -90, 90, 0.005)
  ),
  4326
);

DECLARE
  index_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO index_count
    FROM user_indexes
   WHERE index_name = 'CUSTOMER_LOCATIONS_SHAPE_SIDX';
  IF index_count = 0 THEN
    EXECUTE IMMEDIATE 'CREATE INDEX customer_locations_shape_sidx ON customer_locations(shape) INDEXTYPE IS MDSYS.SPATIAL_INDEX_V2';
  END IF;
END;
/

DECLARE
  index_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO index_count
    FROM user_indexes
   WHERE index_name = 'SPATIAL_FEATURES_SHAPE_SIDX';
  IF index_count = 0 THEN
    EXECUTE IMMEDIATE 'CREATE INDEX spatial_features_shape_sidx ON spatial_features(shape) INDEXTYPE IS MDSYS.SPATIAL_INDEX_V2';
  END IF;
END;
/

MERGE INTO order_events target
USING (SELECT 1 id, DATE '2026-03-01' event_time, 'created' event_type, '{"order_no":"ORD-1001"}' payload FROM dual) source
ON (target.id = source.id AND target.event_time = source.event_time)
WHEN MATCHED THEN UPDATE SET target.event_type = source.event_type, target.payload = source.payload
WHEN NOT MATCHED THEN INSERT (id, event_time, event_type, payload)
VALUES (source.id, source.event_time, source.event_type, source.payload);

MERGE INTO orders target
USING (SELECT 1002 id, 'ORD-1002' order_no, 2 customer_id, 88.00 amount, TIMESTAMP '2026-03-05 16:10:00' ordered_at, 'second order' notes FROM dual) source
ON (target.id = source.id)
WHEN MATCHED THEN UPDATE SET target.order_no = source.order_no, target.customer_id = source.customer_id, target.amount = source.amount, target.ordered_at = source.ordered_at, target.notes = source.notes
WHEN NOT MATCHED THEN INSERT (id, order_no, customer_id, amount, ordered_at, notes)
VALUES (source.id, source.order_no, source.customer_id, source.amount, source.ordered_at, source.notes);

COMMIT;
SELECT COUNT(*) AS customer_count FROM customers;
SELECT COUNT(*) AS order_count FROM orders;
SELECT COUNT(*) AS order_event_count FROM order_events;
SELECT COUNT(*) AS customer_location_count FROM customer_locations;
SELECT COUNT(*) AS spatial_feature_count FROM spatial_features;
SELECT COUNT(*) AS spatial_metadata_count
  FROM user_sdo_geom_metadata
 WHERE table_name IN ('CUSTOMER_LOCATIONS', 'SPATIAL_FEATURES')
   AND column_name = 'SHAPE';
SELECT COUNT(*) AS spatial_index_count
  FROM user_indexes
 WHERE index_name IN ('CUSTOMER_LOCATIONS_SHAPE_SIDX', 'SPATIAL_FEATURES_SHAPE_SIDX')
   AND index_type = 'DOMAIN';
SELECT COUNT(*) AS spatial_wkb_count
  FROM (
    SELECT shape FROM customer_locations
    UNION ALL
    SELECT shape FROM spatial_features
  )
 WHERE DBMS_LOB.GETLENGTH(SDO_UTIL.TO_WKBGEOMETRY(shape)) > 0;
