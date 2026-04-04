#!/bin/bash
# Neo4j 测试数据脚本
# 创建一个技术人才知识图谱：人员、公司、技术栈、项目及其关系

set -e

CONTAINER="business-neo4j"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASS="${NEO4J_PASS:-neo4j_password}"

run_cypher() {
  docker exec "$CONTAINER" cypher-shell -u "$NEO4J_USER" -p "$NEO4J_PASS" "$1"
}

echo "=== Neo4j 测试数据初始化开始 ==="

# ── 1. 清空旧测试数据 ─────────────────────────────────────────────────────────
echo "→ 清空旧数据..."
run_cypher "MATCH (n) DETACH DELETE n"

# ── 2. 创建人员节点 ───────────────────────────────────────────────────────────
echo "→ 创建 Person 节点..."
run_cypher "
CREATE
  (:Person {id:'P001', name:'张伟',   age:32, city:'北京', email:'zhang.wei@example.com',   role:'架构师'}),
  (:Person {id:'P002', name:'李娜',   age:28, city:'上海', email:'li.na@example.com',        role:'后端工程师'}),
  (:Person {id:'P003', name:'王磊',   age:35, city:'深圳', email:'wang.lei@example.com',     role:'数据工程师'}),
  (:Person {id:'P004', name:'陈静',   age:26, city:'杭州', email:'chen.jing@example.com',   role:'前端工程师'}),
  (:Person {id:'P005', name:'刘洋',   age:40, city:'北京', email:'liu.yang@example.com',    role:'CTO'}),
  (:Person {id:'P006', name:'赵敏',   age:30, city:'成都', email:'zhao.min@example.com',    role:'算法工程师'}),
  (:Person {id:'P007', name:'孙鹏',   age:29, city:'上海', email:'sun.peng@example.com',    role:'DevOps工程师'})
"

# ── 3. 创建公司节点 ───────────────────────────────────────────────────────────
echo "→ 创建 Company 节点..."
run_cypher "
CREATE
  (:Company {id:'C001', name:'星云科技',     industry:'云计算',  size:'large',  city:'北京', founded:2015}),
  (:Company {id:'C002', name:'数链智能',     industry:'AI',       size:'medium', city:'上海', founded:2018}),
  (:Company {id:'C003', name:'海图数据',     industry:'大数据',  size:'medium', city:'深圳', founded:2016}),
  (:Company {id:'C004', name:'远航互联',     industry:'互联网',  size:'large',  city:'杭州', founded:2012})
"

# ── 4. 创建技术节点 ───────────────────────────────────────────────────────────
echo "→ 创建 Technology 节点..."
run_cypher "
CREATE
  (:Technology {id:'T001', name:'Go',         type:'语言',   level:'backend'}),
  (:Technology {id:'T002', name:'Python',     type:'语言',   level:'fullstack'}),
  (:Technology {id:'T003', name:'Vue.js',     type:'框架',   level:'frontend'}),
  (:Technology {id:'T004', name:'PostgreSQL', type:'数据库', level:'backend'}),
  (:Technology {id:'T005', name:'Neo4j',      type:'数据库', level:'backend'}),
  (:Technology {id:'T006', name:'Kubernetes', type:'运维',   level:'infra'}),
  (:Technology {id:'T007', name:'Spark',      type:'框架',   level:'data'}),
  (:Technology {id:'T008', name:'LangChain',  type:'框架',   level:'ai'}),
  (:Technology {id:'T009', name:'React',      type:'框架',   level:'frontend'}),
  (:Technology {id:'T010', name:'Redis',      type:'数据库', level:'backend'})
"

# ── 5. 创建项目节点 ───────────────────────────────────────────────────────────
echo "→ 创建 Project 节点..."
run_cypher "
CREATE
  (:Project {id:'PRJ001', name:'全域数据平台',   status:'active',    budget:5000000, start_date:'2024-01-01'}),
  (:Project {id:'PRJ002', name:'智能推荐引擎',   status:'active',    budget:2000000, start_date:'2024-06-01'}),
  (:Project {id:'PRJ003', name:'实时数仓建设',   status:'completed', budget:3000000, start_date:'2023-03-01'}),
  (:Project {id:'PRJ004', name:'AI对话助手',     status:'active',    budget:1500000, start_date:'2025-01-01'})
"

# ── 6. 建立关系 ───────────────────────────────────────────────────────────────
echo "→ 建立 WORKS_AT 关系（人员 → 公司）..."
run_cypher "MATCH (p:Person {id:'P001'}), (c:Company {id:'C001'}) CREATE (p)-[:WORKS_AT {since:2020, position:'首席架构师'}]->(c)"
run_cypher "MATCH (p:Person {id:'P002'}), (c:Company {id:'C002'}) CREATE (p)-[:WORKS_AT {since:2021, position:'高级工程师'}]->(c)"
run_cypher "MATCH (p:Person {id:'P003'}), (c:Company {id:'C003'}) CREATE (p)-[:WORKS_AT {since:2019, position:'数据负责人'}]->(c)"
run_cypher "MATCH (p:Person {id:'P004'}), (c:Company {id:'C004'}) CREATE (p)-[:WORKS_AT {since:2022, position:'前端工程师'}]->(c)"
run_cypher "MATCH (p:Person {id:'P005'}), (c:Company {id:'C001'}) CREATE (p)-[:WORKS_AT {since:2015, position:'CTO'}]->(c)"
run_cypher "MATCH (p:Person {id:'P006'}), (c:Company {id:'C002'}) CREATE (p)-[:WORKS_AT {since:2020, position:'算法专家'}]->(c)"
run_cypher "MATCH (p:Person {id:'P007'}), (c:Company {id:'C001'}) CREATE (p)-[:WORKS_AT {since:2021, position:'DevOps工程师'}]->(c)"

echo "→ 建立 KNOWS 关系（人员社交网络）..."
run_cypher "MATCH (a:Person {id:'P001'}), (b:Person {id:'P002'}) CREATE (a)-[:KNOWS {since:2019, strength:'strong'}]->(b)"
run_cypher "MATCH (a:Person {id:'P001'}), (b:Person {id:'P003'}) CREATE (a)-[:KNOWS {since:2020, strength:'medium'}]->(b)"
run_cypher "MATCH (a:Person {id:'P001'}), (b:Person {id:'P005'}) CREATE (a)-[:KNOWS {since:2020, strength:'strong'}]->(b)"
run_cypher "MATCH (a:Person {id:'P002'}), (b:Person {id:'P006'}) CREATE (a)-[:KNOWS {since:2021, strength:'medium'}]->(b)"
run_cypher "MATCH (a:Person {id:'P003'}), (b:Person {id:'P007'}) CREATE (a)-[:KNOWS {since:2022, strength:'weak'}]->(b)"
run_cypher "MATCH (a:Person {id:'P004'}), (b:Person {id:'P002'}) CREATE (a)-[:KNOWS {since:2023, strength:'medium'}]->(b)"
run_cypher "MATCH (a:Person {id:'P005'}), (b:Person {id:'P006'}) CREATE (a)-[:KNOWS {since:2018, strength:'strong'}]->(b)"

echo "→ 建立 SKILLED_IN 关系（人员 → 技术）..."
run_cypher "MATCH (p:Person {id:'P001'}), (t:Technology {id:'T001'}) CREATE (p)-[:SKILLED_IN {years:6, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P001'}), (t:Technology {id:'T004'}) CREATE (p)-[:SKILLED_IN {years:8, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P001'}), (t:Technology {id:'T006'}) CREATE (p)-[:SKILLED_IN {years:4, level:'advanced'}]->(t)"
run_cypher "MATCH (p:Person {id:'P002'}), (t:Technology {id:'T001'}) CREATE (p)-[:SKILLED_IN {years:3, level:'advanced'}]->(t)"
run_cypher "MATCH (p:Person {id:'P002'}), (t:Technology {id:'T010'}) CREATE (p)-[:SKILLED_IN {years:4, level:'advanced'}]->(t)"
run_cypher "MATCH (p:Person {id:'P002'}), (t:Technology {id:'T004'}) CREATE (p)-[:SKILLED_IN {years:3, level:'intermediate'}]->(t)"
run_cypher "MATCH (p:Person {id:'P003'}), (t:Technology {id:'T002'}) CREATE (p)-[:SKILLED_IN {years:7, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P003'}), (t:Technology {id:'T007'}) CREATE (p)-[:SKILLED_IN {years:5, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P003'}), (t:Technology {id:'T004'}) CREATE (p)-[:SKILLED_IN {years:6, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P004'}), (t:Technology {id:'T003'}) CREATE (p)-[:SKILLED_IN {years:2, level:'advanced'}]->(t)"
run_cypher "MATCH (p:Person {id:'P004'}), (t:Technology {id:'T009'}) CREATE (p)-[:SKILLED_IN {years:1, level:'intermediate'}]->(t)"
run_cypher "MATCH (p:Person {id:'P005'}), (t:Technology {id:'T001'}) CREATE (p)-[:SKILLED_IN {years:10, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P005'}), (t:Technology {id:'T006'}) CREATE (p)-[:SKILLED_IN {years:8, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P006'}), (t:Technology {id:'T002'}) CREATE (p)-[:SKILLED_IN {years:5, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P006'}), (t:Technology {id:'T008'}) CREATE (p)-[:SKILLED_IN {years:2, level:'advanced'}]->(t)"
run_cypher "MATCH (p:Person {id:'P007'}), (t:Technology {id:'T006'}) CREATE (p)-[:SKILLED_IN {years:4, level:'expert'}]->(t)"
run_cypher "MATCH (p:Person {id:'P007'}), (t:Technology {id:'T010'}) CREATE (p)-[:SKILLED_IN {years:3, level:'advanced'}]->(t)"

echo "→ 建立 PARTICIPATES_IN 关系（人员 → 项目）..."
run_cypher "MATCH (p:Person {id:'P001'}), (prj:Project {id:'PRJ001'}) CREATE (p)-[:PARTICIPATES_IN {role:'技术负责人', join_date:'2024-01-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P002'}), (prj:Project {id:'PRJ001'}) CREATE (p)-[:PARTICIPATES_IN {role:'后端开发', join_date:'2024-01-15'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P003'}), (prj:Project {id:'PRJ001'}) CREATE (p)-[:PARTICIPATES_IN {role:'数据架构', join_date:'2024-02-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P004'}), (prj:Project {id:'PRJ001'}) CREATE (p)-[:PARTICIPATES_IN {role:'前端开发', join_date:'2024-03-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P006'}), (prj:Project {id:'PRJ002'}) CREATE (p)-[:PARTICIPATES_IN {role:'算法负责人', join_date:'2024-06-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P002'}), (prj:Project {id:'PRJ002'}) CREATE (p)-[:PARTICIPATES_IN {role:'后端开发', join_date:'2024-06-15'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P003'}), (prj:Project {id:'PRJ003'}) CREATE (p)-[:PARTICIPATES_IN {role:'数仓负责人', join_date:'2023-03-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P006'}), (prj:Project {id:'PRJ004'}) CREATE (p)-[:PARTICIPATES_IN {role:'AI研发', join_date:'2025-01-01'}]->(prj)"
run_cypher "MATCH (p:Person {id:'P005'}), (prj:Project {id:'PRJ004'}) CREATE (p)-[:PARTICIPATES_IN {role:'项目发起人', join_date:'2025-01-01'}]->(prj)"

echo "→ 建立 USES_TECH 关系（项目 → 技术）..."
run_cypher "MATCH (prj:Project {id:'PRJ001'}), (t:Technology {id:'T001'}) CREATE (prj)-[:USES_TECH {purpose:'核心服务'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ001'}), (t:Technology {id:'T003'}) CREATE (prj)-[:USES_TECH {purpose:'前端界面'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ001'}), (t:Technology {id:'T004'}) CREATE (prj)-[:USES_TECH {purpose:'元数据存储'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ001'}), (t:Technology {id:'T005'}) CREATE (prj)-[:USES_TECH {purpose:'知识图谱'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ001'}), (t:Technology {id:'T006'}) CREATE (prj)-[:USES_TECH {purpose:'容器编排'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ002'}), (t:Technology {id:'T002'}) CREATE (prj)-[:USES_TECH {purpose:'算法开发'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ002'}), (t:Technology {id:'T008'}) CREATE (prj)-[:USES_TECH {purpose:'LLM集成'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ003'}), (t:Technology {id:'T007'}) CREATE (prj)-[:USES_TECH {purpose:'批处理'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ003'}), (t:Technology {id:'T004'}) CREATE (prj)-[:USES_TECH {purpose:'数仓存储'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ004'}), (t:Technology {id:'T008'}) CREATE (prj)-[:USES_TECH {purpose:'对话引擎'}]->(t)"
run_cypher "MATCH (prj:Project {id:'PRJ004'}), (t:Technology {id:'T002'}) CREATE (prj)-[:USES_TECH {purpose:'模型推理'}]->(t)"

echo "→ 建立 INVESTED_IN 关系（公司 → 项目）..."
run_cypher "MATCH (c:Company {id:'C001'}), (prj:Project {id:'PRJ001'}) CREATE (c)-[:INVESTED_IN {amount:5000000, share:1.0}]->(prj)"
run_cypher "MATCH (c:Company {id:'C002'}), (prj:Project {id:'PRJ002'}) CREATE (c)-[:INVESTED_IN {amount:2000000, share:1.0}]->(prj)"
run_cypher "MATCH (c:Company {id:'C003'}), (prj:Project {id:'PRJ003'}) CREATE (c)-[:INVESTED_IN {amount:3000000, share:1.0}]->(prj)"
run_cypher "MATCH (c:Company {id:'C001'}), (prj:Project {id:'PRJ004'}) CREATE (c)-[:INVESTED_IN {amount:1000000, share:0.67}]->(prj)"
run_cypher "MATCH (c:Company {id:'C002'}), (prj:Project {id:'PRJ004'}) CREATE (c)-[:INVESTED_IN {amount:500000,  share:0.33}]->(prj)"

# ── 7. 创建索引 ───────────────────────────────────────────────────────────────
echo "→ 创建约束和索引..."
run_cypher "CREATE CONSTRAINT person_id   IF NOT EXISTS FOR (n:Person)     REQUIRE n.id IS UNIQUE"
run_cypher "CREATE CONSTRAINT company_id  IF NOT EXISTS FOR (n:Company)    REQUIRE n.id IS UNIQUE"
run_cypher "CREATE CONSTRAINT tech_id     IF NOT EXISTS FOR (n:Technology) REQUIRE n.id IS UNIQUE"
run_cypher "CREATE CONSTRAINT project_id  IF NOT EXISTS FOR (n:Project)    REQUIRE n.id IS UNIQUE"

# ── 8. 验证数据 ───────────────────────────────────────────────────────────────
echo ""
echo "=== 验证 Neo4j 测试数据 ==="
run_cypher "
MATCH (p:Person)     WITH count(p) AS c RETURN '人员节点' AS label, c AS count
UNION ALL
MATCH (c:Company)    WITH count(c) AS c RETURN '公司节点', c
UNION ALL
MATCH (t:Technology) WITH count(t) AS c RETURN '技术节点', c
UNION ALL
MATCH (p:Project)    WITH count(p) AS c RETURN '项目节点', c
UNION ALL
MATCH ()-[r]->()     WITH count(r) AS c RETURN '关系总数', c
"

echo ""
echo "→ 示例查询：张伟认识的人及其所在公司"
run_cypher "
MATCH (p:Person {name:'张伟'})-[:KNOWS]->(friend:Person)-[:WORKS_AT]->(c:Company)
RETURN friend.name AS 朋友, friend.role AS 职位, c.name AS 公司
"

echo ""
echo "→ 示例查询：全域数据平台项目成员及其技术栈"
run_cypher "
MATCH (person:Person)-[:PARTICIPATES_IN]->(prj:Project {name:'全域数据平台'})
MATCH (person)-[:SKILLED_IN]->(tech:Technology)
RETURN person.name AS 成员, person.role AS 角色, collect(tech.name) AS 技术栈
ORDER BY 成员
"

echo ""
echo "=== Neo4j 测试数据初始化完成 ==="
