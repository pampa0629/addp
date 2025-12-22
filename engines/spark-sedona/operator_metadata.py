"""
Spark Sedona 算子元数据定义
供前端工作流编辑器使用
"""

from typing import List, Dict, Any


def get_operator_metadata() -> List[Dict[str, Any]]:
    """
    返回所有算子的元数据定义

    Returns:
        算子元数据列表
    """
    return [
        # ========================================
        # 类别 1: 数据 I/O 算子 (5个)
        # ========================================
        {
            "id": "load",
            "name": "load",
            "display_name": "数据加载",
            "module": "spark",
            "category": "数据I/O",
            "description": "从数据库、文件、湖仓加载数据",
            "parameters": [
                {
                    "name": "source_type",
                    "type": "enum",
                    "enum": ["table", "file", "sql", "catalog"],
                    "required": True,
                    "description": "数据源类型",
                    "default": "table"
                },
                {
                    "name": "resource_id",
                    "type": "integer",
                    "required": False,
                    "description": "数据资源ID (从system.resources获取,如PostgreSQL/MinIO资源)",
                    "depends_on": {"source_type": ["table", "file"]}
                },
                {
                    "name": "schema",
                    "type": "string",
                    "required": False,
                    "description": "数据库schema (默认public)",
                    "default": "public",
                    "depends_on": {"source_type": ["table"]}
                },
                {
                    "name": "table",
                    "type": "string",
                    "required": True,
                    "description": "表名",
                    "depends_on": {"source_type": ["table"]}
                },
                {
                    "name": "geom_column",
                    "type": "string",
                    "required": False,
                    "description": "几何列名 (Sedona空间列)",
                    "default": "geom",
                    "depends_on": {"source_type": ["table", "file"]}
                },
                {
                    "name": "path",
                    "type": "string",
                    "required": True,
                    "description": "文件路径 (s3://bucket/file 或 hdfs://...)",
                    "depends_on": {"source_type": ["file"]}
                },
                {
                    "name": "format",
                    "type": "enum",
                    "enum": ["parquet", "geoparquet", "shapefile", "csv", "json", "delta", "hudi", "iceberg"],
                    "required": True,
                    "description": "文件格式",
                    "default": "parquet",
                    "depends_on": {"source_type": ["file"]}
                },
                {
                    "name": "sql",
                    "type": "text",
                    "required": True,
                    "description": "SQL查询语句",
                    "depends_on": {"source_type": ["sql"]}
                },
                {
                    "name": "catalog_name",
                    "type": "string",
                    "required": True,
                    "description": "Catalog名称 (如iceberg_catalog)",
                    "depends_on": {"source_type": ["catalog"]}
                },
                {
                    "name": "database",
                    "type": "string",
                    "required": True,
                    "description": "数据库名",
                    "depends_on": {"source_type": ["catalog"]}
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True,
                    "description": "加载的DataFrame"
                }
            ]
        },

        {
            "id": "save",
            "name": "save",
            "display_name": "数据保存",
            "module": "spark",
            "category": "数据I/O",
            "description": "保存数据到数据库或文件",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame (引用上游任务)"
                },
                {
                    "name": "target_type",
                    "type": "enum",
                    "enum": ["table", "file"],
                    "required": True,
                    "description": "保存目标类型",
                    "default": "table"
                },
                {
                    "name": "resource_id",
                    "type": "integer",
                    "required": False,
                    "description": "目标资源ID"
                },
                {
                    "name": "schema",
                    "type": "string",
                    "required": False,
                    "description": "数据库schema",
                    "default": "public",
                    "depends_on": {"target_type": ["table"]}
                },
                {
                    "name": "table",
                    "type": "string",
                    "required": True,
                    "description": "表名",
                    "depends_on": {"target_type": ["table"]}
                },
                {
                    "name": "mode",
                    "type": "enum",
                    "enum": ["overwrite", "append"],
                    "required": False,
                    "description": "保存模式",
                    "default": "overwrite"
                },
                {
                    "name": "path",
                    "type": "string",
                    "required": True,
                    "description": "文件路径",
                    "depends_on": {"target_type": ["file"]}
                },
                {
                    "name": "format",
                    "type": "enum",
                    "enum": ["parquet", "geoparquet", "csv", "json", "delta"],
                    "required": True,
                    "description": "文件格式",
                    "default": "parquet",
                    "depends_on": {"target_type": ["file"]}
                }
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "dict",
                    "is_default": True,
                    "description": "保存结果信息"
                }
            ]
        },

        {
            "id": "preview",
            "name": "preview",
            "display_name": "数据预览",
            "module": "spark",
            "category": "数据I/O",
            "description": "预览DataFrame前N行",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "limit",
                    "type": "integer",
                    "required": False,
                    "description": "预览行数",
                    "default": 100
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "cache",
            "name": "cache",
            "display_name": "内存缓存",
            "module": "spark",
            "category": "数据I/O",
            "description": "将DataFrame缓存到内存,提高性能",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "persist",
            "name": "persist",
            "display_name": "持久化",
            "module": "spark",
            "category": "数据I/O",
            "description": "持久化DataFrame到指定存储级别",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "storage_level",
                    "type": "enum",
                    "enum": ["MEMORY_ONLY", "DISK_ONLY", "MEMORY_AND_DISK", "MEMORY_AND_DISK_SER"],
                    "required": False,
                    "description": "存储级别",
                    "default": "MEMORY_AND_DISK"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        # ========================================
        # 类别 2: Sedona 空间算子 (示例：7个核心算子)
        # ========================================
        {
            "id": "st_buffer",
            "name": "st_buffer",
            "display_name": "缓冲区分析",
            "module": "spark",
            "category": "空间分析",
            "description": "创建几何对象的缓冲区",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "geom_column",
                    "type": "string",
                    "required": False,
                    "description": "几何列名",
                    "default": "geom"
                },
                {
                    "name": "distance",
                    "type": "float",
                    "required": True,
                    "description": "缓冲距离 (单位与坐标系一致)"
                },
                {
                    "name": "output_column",
                    "type": "string",
                    "required": False,
                    "description": "输出几何列名",
                    "default": "buffer_geom"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "st_centroid",
            "name": "st_centroid",
            "display_name": "质心计算",
            "module": "spark",
            "category": "空间分析",
            "description": "计算几何对象的质心",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "geom_column",
                    "type": "string",
                    "required": False,
                    "description": "几何列名",
                    "default": "geom"
                },
                {
                    "name": "output_column",
                    "type": "string",
                    "required": False,
                    "description": "输出质心列名",
                    "default": "centroid"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "spatial_join",
            "name": "spatial_join",
            "display_name": "空间连接",
            "module": "spark",
            "category": "空间分析",
            "description": "基于空间关系连接两个数据集",
            "parameters": [
                {
                    "name": "left_df",
                    "type": "ref",
                    "required": True,
                    "description": "左表DataFrame"
                },
                {
                    "name": "right_df",
                    "type": "ref",
                    "required": True,
                    "description": "右表DataFrame"
                },
                {
                    "name": "left_geom",
                    "type": "string",
                    "required": False,
                    "description": "左表几何列名",
                    "default": "geom"
                },
                {
                    "name": "right_geom",
                    "type": "string",
                    "required": False,
                    "description": "右表几何列名",
                    "default": "geom"
                },
                {
                    "name": "join_type",
                    "type": "enum",
                    "enum": ["intersects", "contains", "within", "overlaps"],
                    "required": False,
                    "description": "空间连接类型",
                    "default": "intersects"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "st_distance",
            "name": "st_distance",
            "display_name": "距离计算",
            "module": "spark",
            "category": "空间分析",
            "description": "计算几何对象到目标几何的距离",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "geom_column",
                    "type": "string",
                    "required": False,
                    "description": "几何列名",
                    "default": "geom"
                },
                {
                    "name": "target_geom",
                    "type": "string",
                    "required": True,
                    "description": "目标几何 (WKT格式或列名)"
                },
                {
                    "name": "output_column",
                    "type": "string",
                    "required": False,
                    "description": "输出距离列名",
                    "default": "distance"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "st_transform",
            "name": "st_transform",
            "display_name": "坐标转换",
            "module": "spark",
            "category": "空间分析",
            "description": "转换几何对象的坐标系",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "geom_column",
                    "type": "string",
                    "required": False,
                    "description": "几何列名",
                    "default": "geom"
                },
                {
                    "name": "source_crs",
                    "type": "string",
                    "required": False,
                    "description": "源坐标系",
                    "default": "EPSG:4326"
                },
                {
                    "name": "target_crs",
                    "type": "string",
                    "required": False,
                    "description": "目标坐标系",
                    "default": "EPSG:3857"
                },
                {
                    "name": "output_column",
                    "type": "string",
                    "required": False,
                    "description": "输出列名",
                    "default": "geom_transformed"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        # ========================================
        # 类别 3: 数据转换算子 (5个)
        # ========================================
        {
            "id": "select",
            "name": "select",
            "display_name": "选择列",
            "module": "spark",
            "category": "数据转换",
            "description": "选择DataFrame中的指定列",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "columns",
                    "type": "array",
                    "required": True,
                    "description": "列名列表",
                    "example": ["id", "name", "geom"]
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "filter",
            "name": "filter",
            "display_name": "条件过滤",
            "module": "spark",
            "category": "数据转换",
            "description": "根据条件过滤数据",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "condition",
                    "type": "text",
                    "required": True,
                    "description": "过滤条件 (SQL WHERE子句,不含WHERE关键字)",
                    "example": "population > 1000000 AND country = '中国'"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "add_column",
            "name": "add_column",
            "display_name": "添加列",
            "module": "spark",
            "category": "数据转换",
            "description": "添加新列 (支持SQL表达式)",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "column_name",
                    "type": "string",
                    "required": True,
                    "description": "新列名"
                },
                {
                    "name": "expression",
                    "type": "text",
                    "required": True,
                    "description": "列表达式 (SQL表达式)",
                    "example": "ST_Area(geom) / 1000000"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "rename_column",
            "name": "rename_column",
            "display_name": "重命名列",
            "module": "spark",
            "category": "数据转换",
            "description": "重命名DataFrame中的列",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "old_name",
                    "type": "string",
                    "required": True,
                    "description": "原列名"
                },
                {
                    "name": "new_name",
                    "type": "string",
                    "required": True,
                    "description": "新列名"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "drop_column",
            "name": "drop_column",
            "display_name": "删除列",
            "module": "spark",
            "category": "数据转换",
            "description": "删除DataFrame中的指定列",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "columns",
                    "type": "array",
                    "required": True,
                    "description": "要删除的列名列表"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        # ========================================
        # 类别 4: 聚合分析算子 (2个)
        # ========================================
        {
            "id": "group_by",
            "name": "group_by",
            "display_name": "分组聚合",
            "module": "spark",
            "category": "聚合分析",
            "description": "按列分组并执行聚合计算",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "group_columns",
                    "type": "array",
                    "required": True,
                    "description": "分组列名列表"
                },
                {
                    "name": "agg_exprs",
                    "type": "dict",
                    "required": True,
                    "description": "聚合表达式字典 (key=输出列名, value=聚合表达式)",
                    "example": {"total_count": "count(*)", "total_population": "sum(population)"}
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "join",
            "name": "join",
            "display_name": "表连接",
            "module": "spark",
            "category": "聚合分析",
            "description": "连接两个DataFrame",
            "parameters": [
                {
                    "name": "left_df",
                    "type": "ref",
                    "required": True,
                    "description": "左表"
                },
                {
                    "name": "right_df",
                    "type": "ref",
                    "required": True,
                    "description": "右表"
                },
                {
                    "name": "on",
                    "type": "string",
                    "required": True,
                    "description": "连接条件 (列名或SQL表达式)"
                },
                {
                    "name": "how",
                    "type": "enum",
                    "enum": ["inner", "left", "right", "outer"],
                    "required": False,
                    "description": "连接类型",
                    "default": "inner"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        # ========================================
        # 类别 5: Spark SQL 算子 (2个)
        # ========================================
        {
            "id": "sql",
            "name": "sql",
            "display_name": "自由SQL查询",
            "module": "spark",
            "category": "Spark SQL",
            "description": "执行自由SQL查询",
            "parameters": [
                {
                    "name": "query",
                    "type": "text",
                    "required": True,
                    "description": "SQL查询语句",
                    "example": "SELECT country, COUNT(*) as city_count FROM cities GROUP BY country"
                }
            ],
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True
                }
            ]
        },

        {
            "id": "create_temp_view",
            "name": "create_temp_view",
            "display_name": "创建临时视图",
            "module": "spark",
            "category": "Spark SQL",
            "description": "将DataFrame注册为临时视图供SQL查询使用",
            "parameters": [
                {
                    "name": "input_df",
                    "type": "ref",
                    "required": True,
                    "description": "输入DataFrame"
                },
                {
                    "name": "view_name",
                    "type": "string",
                    "required": True,
                    "description": "视图名称"
                }
            ],
            "output_ports": [
                {
                    "name": "result",
                    "type": "dict",
                    "is_default": True,
                    "description": "创建结果信息"
                }
            ]
        }
    ]
