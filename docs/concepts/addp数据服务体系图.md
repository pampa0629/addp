# ADDP 数据服务体系图

本文档展示 ADDP 平台的数据服务发布机制,包括 OGC 标准服务和查询服务 API。

---

## 目录

1. [数据服务概述](#数据服务概述)
2. [服务发布流程](#服务发布流程)
3. [OGC 标准服务](#ogc-标准服务)
4. [查询服务 API](#查询服务-api)

---

## 数据服务概述

**数据服务** 是将数据以 API 形式对外发布,支持 OGC 标准服务(空间数据)和查询服务(非空间数据)。

```mermaid
graph TB
    Service[数据服务 Service]

    Service --> OGC[OGC 标准服务<br/>空间数据]
    Service --> Query[查询服务 API<br/>非空间数据]

    OGC --> WMS[WMS<br/>Web Map Service<br/>地图图片服务]
    OGC --> WFS[WFS<br/>Web Feature Service<br/>矢量要素服务]
    OGC --> WMTS[WMTS<br/>Web Map Tile Service<br/>瓦片地图服务]
    OGC --> WCS[WCS<br/>Web Coverage Service<br/>栅格数据服务]

    Query --> REST[RESTful API<br/>条件查询/分页/字段选择]
    Query --> Spatial[空间查询<br/>边界框/空间关系]

    classDef root fill:#fff9c4,stroke:#f57f17
    classDef category fill:#e1f5ff,stroke:#01579b
    classDef service fill:#e8f5e9,stroke:#1b5e20

    class Service root
    class OGC,Query category
    class WMS,WFS,WMTS,WCS,REST,Spatial service
```

---

## 服务发布流程

```mermaid
sequenceDiagram
    participant User as 用户
    participant ServiceFE as Service 前端
    participant ServiceBE as Service Backend
    participant DB as PostgreSQL<br/>(service schema)
    participant DataSource as 数据源<br/>(PostgreSQL/MinIO)

    User->>ServiceFE: 1. 配置服务发布
    ServiceFE->>ServiceBE: 2. POST /api/service/publish<br/>{name, engine_id, table, service_type}
    ServiceBE->>DB: 3. 保存服务配置<br/>(services 表)
    DB-->>ServiceBE: 4. 返回服务 ID
    ServiceBE->>ServiceBE: 5. 生成服务端点<br/>(WMS/WFS/API 端点)
    ServiceBE-->>ServiceFE: 6. 返回服务信息
    ServiceFE-->>User: 7. 展示服务端点 URL

    Note over User,DataSource: === 客户端访问服务 ===

    User->>ServiceBE: 8. GET /api/service/wms?<br/>service=WMS&request=GetMap&...
    ServiceBE->>DB: 9. 查询服务配置
    DB-->>ServiceBE: 10. 返回配置 (engine_id, table)
    ServiceBE->>DataSource: 11. 查询空间数据
    DataSource-->>ServiceBE: 12. 返回数据
    ServiceBE->>ServiceBE: 13. 渲染为地图图片 (WMS)<br/>或返回 GeoJSON (WFS)
    ServiceBE-->>User: 14. 返回结果
```

---

## OGC 标准服务

ADDP 支持以下 OGC 标准服务:

```mermaid
graph TB
    subgraph "WMS - 地图图片服务"
        WMS[WMS Service]
        WMS --> GetCap1[GetCapabilities<br/>获取服务元数据]
        WMS --> GetMap[GetMap<br/>请求地图图片]
        WMS --> GetFeature1[GetFeatureInfo<br/>查询要素信息]
    end

    subgraph "WFS - 矢量要素服务"
        WFS[WFS Service]
        WFS --> GetCap2[GetCapabilities<br/>获取服务元数据]
        WFS --> GetFeature2[GetFeature<br/>获取矢量要素]
        WFS --> DescFeature[DescribeFeatureType<br/>获取要素类型定义]
    end

    subgraph "WMTS - 瓦片地图服务"
        WMTS[WMTS Service]
        WMTS --> GetCap3[GetCapabilities<br/>获取服务元数据]
        WMTS --> GetTile[GetTile<br/>获取指定瓦片]
    end

    subgraph "WCS - 栅格数据服务"
        WCS[WCS Service]
        WCS --> GetCap4[GetCapabilities<br/>获取服务元数据]
        WCS --> GetCoverage[GetCoverage<br/>获取栅格数据]
        WCS --> DescCoverage[DescribeCoverage<br/>获取覆盖范围描述]
    end

    classDef wms fill:#e3f2fd,stroke:#1976d2
    classDef wfs fill:#e8f5e9,stroke:#388e3c
    classDef wmts fill:#fff3e0,stroke:#f57c00
    classDef wcs fill:#f3e5f5,stroke:#7b1fa2

    class WMS,GetCap1,GetMap,GetFeature1 wms
    class WFS,GetCap2,GetFeature2,DescFeature wfs
    class WMTS,GetCap3,GetTile wmts
    class WCS,GetCap4,GetCoverage,DescCoverage wcs
```

### OGC 标准服务说明

| 服务 | 说明 | 主要操作 | 适用场景 |
|------|------|---------|---------|
| **WMS** | Web Map Service<br/>地图图片服务 | - GetCapabilities<br/>- GetMap<br/>- GetFeatureInfo | 在线地图展示,快速渲染 |
| **WFS** | Web Feature Service<br/>矢量要素服务 | - GetCapabilities<br/>- GetFeature<br/>- DescribeFeatureType | 矢量数据下载,GIS分析 |
| **WMTS** | Web Map Tile Service<br/>瓦片地图服务 | - GetCapabilities<br/>- GetTile | 高性能地图展示,离线地图 |
| **WCS** | Web Coverage Service<br/>栅格数据服务 | - GetCapabilities<br/>- GetCoverage<br/>- DescribeCoverage | 栅格数据下载,遥感分析 |

---

## 查询服务 API

ADDP 提供 RESTful 数据查询接口,支持非空间数据访问:

```mermaid
graph LR
    API[查询服务 API]

    API --> Condition[条件查询<br/>WHERE 过滤]
    API --> Pagination[分页查询<br/>offset/limit]
    API --> Fields[字段选择<br/>指定返回字段]
    API --> Spatial[空间查询<br/>边界框/空间关系]

    Condition --> Example1["GET /api/service/data/:id?<br/>filter=population>1000000"]
    Pagination --> Example2["GET /api/service/data/:id?<br/>offset=0&limit=100"]
    Fields --> Example3["GET /api/service/data/:id?<br/>fields=name,population"]
    Spatial --> Example4["GET /api/service/data/:id?<br/>bbox=120,30,121,31"]

    classDef api fill:#e1f5ff,stroke:#01579b
    classDef feature fill:#e8f5e9,stroke:#1b5e20
    classDef example fill:#fff9c4,stroke:#f57f17

    class API api
    class Condition,Pagination,Fields,Spatial feature
    class Example1,Example2,Example3,Example4 example
```

### 查询 API 参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `filter` | 条件过滤 (WHERE 子句) | `filter=population>1000000` |
| `offset` | 分页偏移量 | `offset=0` |
| `limit` | 分页大小 | `limit=100` |
| `fields` | 指定返回字段 | `fields=name,population,area` |
| `bbox` | 边界框空间查询 | `bbox=120,30,121,31` (minX,minY,maxX,maxY) |
| `spatial_filter` | 空间关系查询 | `spatial_filter=within:POLYGON(...)` |
| `sort` | 排序 | `sort=population:desc` |

---

## 服务注册与权限

**服务注册**:
- 服务名称、描述、版本
- API 端点和参数定义
- 数据源配置 (引擎、表、字段)

**权限控制**:
- **公开服务**: 无需认证,任何人可访问
- **需认证服务**: 需要 JWT Token
- **租户隔离**: 服务仅对所属租户可见

**访问统计**:
- 记录调用次数
- 统计流量和耗时
- 分析热点数据

---

## 相关文档

- [返回核心概念关系图](../addp核心概念关系图.md)
- [Service 模块详情](../../service/CLAUDE.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
