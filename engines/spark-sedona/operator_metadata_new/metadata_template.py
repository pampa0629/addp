"""
Spark Sedona 算子元数据模板
用于新增算子时参考,确保元数据结构一致性和完整性
"""

# ========================================
# 元数据模板示例 - st_buffer (缓冲区分析)
# ========================================

METADATA_TEMPLATE_EXAMPLE = {
    # ========== 基础信息 ==========
    "id": "st_buffer",                    # 算子唯一标识符 (与operators.py中的函数名一致)
    "name": "st_buffer",                  # 算子名称
    "display_name": "缓冲区分析",         # 中文显示名 (UI列表展示)
    "module": "spark",                    # 所属模块
    "category": "空间分析",               # 算子分类 (数据I/O, 空间分析, 数据转换, 聚合分析, Spark SQL, 窗口函数)

    # ========== 第一级描述 (必填) ==========
    "description": "创建几何对象的缓冲区",

    # ========== 第二级描述 (必填) ==========
    # 简要描述: 20-30字,用于算子选择阶段,帮助用户快速理解算子用途
    "brief_description": "在几何对象周围创建指定距离的缓冲区,常用于影响范围分析和空间查询",

    # ========== 第三级描述 (必填) ==========
    # 详细描述: JSON结构化,供Copilot AI自动生成工作流使用
    "detailed_description": {
        # 概览: 50-100字,详细说明算子功能和适用场景
        "overview": "缓冲区算子在输入几何对象周围创建一个指定距离的区域。支持点、线、面三种几何类型。缓冲距离单位与坐标系一致,地理坐标系(EPSG:4326)需先转换为投影坐标系。常用于影响范围分析、服务区计算、安全距离评估等场景。",

        # 参数详细定义: 每个参数包含类型、是否必需、描述、注意事项
        "parameters": [
            {
                "name": "input_df",
                "type": "dataframe",
                "required": True,
                "description": "输入DataFrame,必须包含有效的几何列",
                "notes": "几何列必须是Sedona Geometry类型,如从PostGIS加载需使用ST_GeomFromWKB转换"
            },
            {
                "name": "geom_column",
                "type": "string",
                "required": False,
                "description": "几何列名,默认为'geom'",
                "notes": "如果数据来源于多个表连接,可能需要显式指定列名"
            },
            {
                "name": "distance",
                "type": "float",
                "required": True,
                "description": "缓冲距离,单位与坐标系一致",
                "notes": "投影坐标系(如EPSG:32650 UTM)单位为米;地理坐标系(EPSG:4326)单位为度,1度≈111公里,建议先用st_transform转投影坐标系。负值仅对面几何有效,表示向内缩进"
            },
            {
                "name": "output_column",
                "type": "string",
                "required": False,
                "description": "输出几何列名,默认为'buffer_geom'",
                "notes": "建议使用有意义的列名,如'buffer_500m'表示500米缓冲区"
            }
        ],

        # 使用场景: 4-5个典型应用场景,必须包含具体数字和业务场景
        "use_cases": [
            "河流污染影响范围分析: 河流中心线缓冲500米,统计影响范围内的建筑物数量和人口",
            "学校服务范围计算: 学校点位缓冲1000米,分析覆盖的居民区和适龄儿童分布",
            "道路噪音影响评估: 高速公路线缓冲200米,评估噪音影响区域面积和受影响居民数量",
            "自然保护区核心区划定: 保护区边界内缩100米,划定严格保护的核心区范围",
            "通信基站覆盖分析: 基站点位缓冲2000米,计算信号覆盖范围和重叠区域"
        ],

        # 注意事项: 4-5条,覆盖坐标系、性能、空值处理、数据质量等常见问题
        "notes": [
            "地理坐标系(EPSG:4326)单位为度,缓冲1度≈111公里,必须先用st_transform转投影坐标系(如EPSG:32650 UTM Zone 50N)",
            "缓冲距离为负值时仅对面几何有效,点和线会返回空几何,需要使用filter过滤掉空值",
            "大数据集(>100万条)建议先用filter过滤再缓冲,或使用cache缓存中间结果,避免内存溢出",
            "Sedona使用JTS库计算缓冲区,默认16段圆弧,精度足够大多数应用,如需更高精度可调整quadrantSegments参数",
            "缓冲区算子会生成新的几何列,原几何列保留,如需替换原列可使用drop_column删除原列"
        ],

        # 输入输出类型
        "input": "DataFrame (包含Geometry列,支持点/线/面)",
        "output": "DataFrame (保留原属性列,新增buffer_geom列,类型为Polygon或MultiPolygon)",

        # 工作流示例: 完整的DAG节点定义,符合Develop模块的工作流格式
        "workflow_example": {
            "id": "buffer_rivers",                # 节点ID
            "operator": "st_buffer",              # 算子名称
            "params": {
                "input_df": {"$ref": "load_rivers"},  # 引用上游节点
                "geom_column": "geom",
                "distance": 500,
                "output_column": "buffer_500m"
            },
            "depends_on": ["load_rivers"]         # 依赖的上游节点
        }
    },

    # ========== 参数定义 (供前端表单渲染) ==========
    "parameters": [
        {
            "name": "input_df",
            "type": "ref",                        # 引用类型 (引用上游任务输出)
            "required": True,
            "description": "输入DataFrame (引用上游任务)"
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

    # ========== 输出端口定义 ==========
    "output_ports": [
        {
            "name": "default",                    # 端口名称 (default表示默认端口)
            "type": "dataframe",                  # 端口类型
            "is_default": True,                   # 是否为默认端口
            "description": "缓冲区分析结果DataFrame"
        }
    ]
}


# ========================================
# 多输出端口算子示例 - split_by_area (按面积分割)
# ========================================

MULTI_OUTPUT_TEMPLATE_EXAMPLE = {
    "id": "split_by_area",
    "name": "split_by_area",
    "display_name": "按面积分割",
    "module": "spark",
    "category": "空间分析",
    "description": "根据面积阈值将要素分割为大小两组",
    "brief_description": "根据面积阈值将几何要素分割为大面积和小面积两组,用于分层处理和制图综合",

    "detailed_description": {
        "overview": "按面积分割算子根据指定的面积阈值将输入的面几何要素分为大面积组和小面积组,通过两个输出端口分别输出。常用于分层处理、制图综合、数据清洗等场景。",

        "parameters": [
            {
                "name": "input_df",
                "type": "dataframe",
                "required": True,
                "description": "输入DataFrame,必须包含面几何",
                "notes": "仅支持Polygon和MultiPolygon类型,点和线会被过滤掉"
            },
            {
                "name": "geom_column",
                "type": "string",
                "required": False,
                "description": "几何列名",
                "notes": "默认为'geom'"
            },
            {
                "name": "threshold",
                "type": "float",
                "required": True,
                "description": "面积阈值 (单位与坐标系一致)",
                "notes": "投影坐标系单位为平方米,地理坐标系单位为平方度"
            }
        ],

        "use_cases": [
            "建筑物分类: 按100平方米阈值分割,大建筑进行详细分析,小建筑简化处理",
            "湖泊分层: 按1平方公里阈值分割,大湖单独管理,小湖聚合显示",
            "地块筛选: 按500平方米阈值分割,大地块用于开发用地分析,小地块过滤掉"
        ],

        "notes": [
            "面积计算使用ST_Area函数,地理坐标系结果不准确,建议先转投影坐标系",
            "两个输出端口分别为'large'(大于等于阈值)和'small'(小于阈值)",
            "如果所有要素都大于或都小于阈值,其中一个端口会返回空DataFrame"
        ],

        "input": "DataFrame (包含Polygon或MultiPolygon几何)",
        "output": "两个DataFrame: large端口(面积>=阈值), small端口(面积<阈值)",

        "workflow_example": {
            "id": "split_buildings",
            "operator": "split_by_area",
            "params": {
                "input_df": {"$ref": "load_buildings"},
                "geom_column": "geom",
                "threshold": 100
            },
            "depends_on": ["load_buildings"]
        }
    },

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
            "name": "threshold",
            "type": "float",
            "required": True,
            "description": "面积阈值 (单位与坐标系一致)"
        }
    ],

    # 多输出端口定义
    "output_ports": [
        {
            "name": "large",
            "type": "dataframe",
            "is_default": True,                   # 主输出端口
            "description": "面积大于等于阈值的要素"
        },
        {
            "name": "small",
            "type": "dataframe",
            "is_default": False,
            "description": "面积小于阈值的要素"
        }
    ]
}


# ========================================
# 元数据编写规范
# ========================================

METADATA_GUIDELINES = """
## 元数据编写规范

### 1. 必填字段
- id, name, display_name, module, category
- description (一句话简要描述)
- brief_description (20-30字)
- detailed_description (JSON结构化,包含overview, parameters, use_cases, notes, input, output, workflow_example)
- parameters (前端表单渲染)
- output_ports (输出端口定义)

### 2. brief_description 编写要点
- 长度: 20-30字
- 内容: 算子功能 + 典型应用场景
- 目的: 帮助用户和AI快速理解算子用途
- 示例: "使用Douglas-Peucker算法简化几何,减少顶点数量,常用于地图制图和性能优化"

### 3. use_cases 编写要点
- 数量: 4-5个
- 格式: "业务场景: 具体操作和数字"
- 要求: 必须包含具体的业务场景、操作步骤、数值参数
- 好的示例: "河流污染影响范围分析: 河流中心线缓冲500米,统计影响范围内的建筑物数量"
- 差的示例: "进行缓冲区分析" (过于抽象,AI无法匹配)

### 4. notes 编写要点
- 数量: 4-5条
- 覆盖: 坐标系转换、单位换算、性能优化、空值处理、数据质量
- 格式: 问题 + 解决方案
- 示例: "地理坐标系(EPSG:4326)单位为度,缓冲1度≈111公里,必须先用st_transform转投影坐标系"

### 5. workflow_example 编写要点
- 必须符合Develop模块的DAG格式
- 包含完整的节点定义: id, operator, params, depends_on
- params中的引用使用{"$ref": "node_id"}格式
- 参数值使用真实的业务数据(如distance=500而非distance=100)

### 6. parameters 字段类型
- ref: 引用上游任务输出
- string: 字符串
- integer: 整数
- float: 浮点数
- enum: 枚举值
- array: 数组
- dict: 字典
- text: 长文本 (用于SQL表达式等)

### 7. output_ports 编写要点
- 单输出算子: 使用name="default", is_default=True
- 多输出算子: 第一个端口is_default=True,其他为False
- description要清晰说明端口的语义
"""


# ========================================
# 快速参考: 算子分类
# ========================================

OPERATOR_CATEGORIES = {
    "数据I/O": ["load", "save", "preview", "cache", "persist"],
    "空间分析": [
        "st_buffer", "st_centroid", "st_intersection", "st_union", "st_difference",
        "st_simplify", "st_convex_hull", "st_envelope",
        "spatial_join", "st_contains", "st_within", "st_overlaps", "st_crosses",
        "st_distance", "st_transform",
        "st_area", "st_length", "st_bounds", "st_is_valid"
    ],
    "数据转换": [
        "select", "filter", "add_column", "rename_column", "drop_column",
        "sort", "distinct", "sample", "coalesce", "repartition",
        "drop_duplicates", "fillna", "dropna"
    ],
    "聚合分析": [
        "group_by", "join",
        "pivot", "unpivot", "rollup", "cube"
    ],
    "Spark SQL": ["sql", "create_temp_view"],
    "窗口函数": ["row_number", "rank", "lag", "lead"]
}
