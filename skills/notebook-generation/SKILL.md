---
name: notebook-generation
description: 基于 ADDP 当前 Notebook Session 已授权的数据引擎和 Catalog，理解自然语言分析需求、确认输入数据源并生成可插入但不自动执行的 Python 单元。用于用户要求在 Notebook 中分析、计算、可视化或生成 Python 代码，尤其是数据源未明确、名称跨中英文或存在多个候选时。
---

# Notebook Generation

## 工作流

1. 只使用当前 Notebook Session 可访问的 Engine descriptor 和实时 Catalog；不得改用租户级 `data.search` 扩大范围。
2. 用户已给出明确数据源时直接校验；未给出时先从需求提取独立输入角色及中英文常用检索词，再对 Session Catalog 做粗筛。
3. 让大模型只对 Develop 已验证候选排序。每个角色有多个候选或来源存在歧义时，列出候选并等待用户逐项确认；不得构造或改写 Catalog path。
4. 确认后由 Develop 重新校验 Engine/path，并读取字段、几何列、几何类型和 CRS 等 Catalog facts。
5. 调用 `notebook.draft.generate`，传入 `python3` Kernel 和已验证事实。生成代码必须通过 `addp_common.notebook.engines` 访问数据，不得建立旁路连接或读取 Token。
6. 展示候选代码供用户确认后插入新代码单元。不得自动执行。

## 代码约束

- 使用具体 Engine 的原生门面，如 `engines.client(engine_id).table(...)`、`sql(...)`、`collection(...)` 或 `graph(...)`。
- Notebook Copilot 的表分析统一使用 `table(...).to_pandas(...)`；空间表使用共享 `table(...).to_geopandas(...)`。不得生成 `engine.sql(...)`，查询语言生成应进入查询工作台。
- 不假定 PostgreSQL、`public`、`geom`、`geometry`、字段名、表名或 CRS；全部从已验证事实推导。
- 完整读取必须给出显式 `memory_limit`；查询必须给出 `max_rows` 和 `timeout`。
- 距离和面积计算必须使用适合的投影坐标系。可能重叠的同类图形先合并，避免重复计量。
- 最终 DataFrame 列名、图例和坐标轴等用户可见标签跟随用户请求语言；面积、距离等指标在标签中标明单位，不直接展示 `area_sqm`、`area_hectares` 等内部英文标识。
- 不使用 `requests`、`httpx`、数据库驱动、连接信息、ResourceLocator 或环境变量绕过 Session 门面。

## 无法生成时

数据源未确认、字段或 CRS 事实不足、Kernel 不支持，或当前 Session 没有所需资源时，返回澄清原因，不生成带占位符的伪代码。
