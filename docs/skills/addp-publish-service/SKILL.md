---
name: addp-publish-service
description: Publish a database table or SQL query as a RESTful data query service in the ADDP platform. Use this skill when the user wants to expose data from a storage engine (PostgreSQL, MySQL, etc.) as an HTTP API endpoint through the ADDP Service module.
---

# ADDP Publish Service

You know how to publish data from any ADDP-managed storage engine as a queryable REST API service through the ADDP Service module.

## Use this skill when

- User wants to publish a database table as an API
- User wants to expose query results as a data service
- User asks to "发布服务", "发布数据", "创建查询服务", or similar
- User wants to make data accessible via HTTP without writing backend code

## Do not use this skill when

- The data source is not registered as a storage engine in ADDP System module
- User wants to publish spatial/GIS data with OGC protocols (WFS/WMTS) — that requires the spatial service flow
- User wants to register an external third-party service (use the registered service flow instead)

## Instructions

### Step 1: Authenticate

```bash
curl -X POST http://localhost:8180/api/v1/system/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"123456"}'
# Extract access_token from response
```

### Step 2: Find the engine ID

```bash
curl -X GET "http://localhost:8180/api/v1/system/engines" \
  -H "Authorization: Bearer $TOKEN"
# Find the engine by name, note its id
```

### Step 3: (Optional) Verify the table exists

```bash
# locator format: addp://engine/{engine_id}/path/{schema}/{table}?type=table
curl -X GET "http://localhost:8081/api/v1/manager/preview?locator=addp://engine/8/path/public/my_table?type=table" \
  -H "Authorization: Bearer $TOKEN"
```

### Step 4: Create the query service

```bash
curl -X POST "http://localhost:8086/api/v1/service/query" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "service_name": "my_table_query",
    "title": "My Table Query Service",
    "description": "...",
    "config_type": "table",
    "engine_id": 8,
    "schema_name": "public",
    "table_name": "my_table",
    "public_access": false
  }'
# Response includes id and endpoints.rest_api URL
```

For SQL-based services, use `"config_type": "sql"` and provide `"sql_query"` instead of schema/table.

### Step 5: Set access control

- `public_access: false` — requires JWT token on every request (default)
- `public_access: true` — open to anyone without authentication

To update:
```bash
curl -X PUT "http://localhost:8086/api/v1/service/query/{id}" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ ...same fields..., "public_access": true }'
```

### Step 6: Test the endpoint

```bash
# JSON (default)
curl "http://localhost:8086/api/query/{service_name}?limit=10"

# CSV export
curl "http://localhost:8086/api/query/{service_name}?format=csv"

# With filters
curl "http://localhost:8086/api/query/{service_name}?where=area>1000&order_by=area DESC&page=1&limit=20"
```

The canonical public endpoint goes through the Gateway: `http://localhost:8000/api/query/{service_name}`

## Key Parameters

**Create/Update service (`POST/PUT /api/v1/service/query`):**

| Field | Required | Notes |
|-------|----------|-------|
| service_name | ✅ | Unique identifier, alphanumeric + underscore |
| title | ✅ | Display name |
| config_type | ✅ | `table` or `sql` |
| engine_id | ✅ | From System engines list |
| schema_name | if table | Database schema |
| table_name | if table | Table name |
| sql_query | if sql | Custom SQL |
| public_access | ❌ | Default false |
| max_features | ❌ | Default 1000 |

**Query endpoint (`GET /api/query/{service_name}`):**

| Param | Notes |
|-------|-------|
| limit | Max records per page (default 50, max 1000) |
| page | Page number (default 1) |
| format | `json` / `csv` / `geojson` |
| fields | Comma-separated column names |
| where | SQL WHERE clause |
| order_by | SQL ORDER BY clause |

## Service Management

```bash
# List all services
GET /api/v1/service/query

# Get one service
GET /api/v1/service/query/{id}

# Delete service
DELETE /api/v1/service/query/{id}
```

## Related Skills

Works with: `addp-storage-engine-setup` (register the engine first), `addp-spatial-service` (for geometry data with OGC protocols)
