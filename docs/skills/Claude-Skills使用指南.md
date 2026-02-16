# Claude Code Skills 使用指南

## 概述

本项目已成功安装 **Antigravity Awesome Skills** - 一个包含 **845+ 个高性能 AI Agent Skills** 的集合，专为 Claude Code、Cursor、Gemini CLI 等 AI 编码助手设计。

## 安装位置

Skills 已安装到：
```
~/.claude/plugins/repos/antigravity-awesome-skills/
```

## Skills 分类

本 Skills 库包含 9 大分类，共 845 个 skills：

| 分类 | Skills 数量 | 说明 |
|------|------------|------|
| **architecture** | 64 | 软件架构、设计模式、微服务、事件驱动架构等 |
| **business** | 38 | 商业分析、市场营销、SEO、内容创作等 |
| **data-ai** | 153 | AI/ML、数据分析、RAG、Agent 框架等 |
| **development** | 124 | 前后端开发、框架使用、代码重构等 |
| **general** | 134 | 通用工具、文档编写、项目管理等 |
| **infrastructure** | 101 | DevOps、云服务、容器编排、监控等 |
| **security** | 126 | 安全测试、渗透测试、合规审计等 |
| **testing** | 24 | 测试策略、自动化测试、TDD 等 |
| **workflow** | 81 | 工作流编排、CI/CD、任务自动化等 |

## ⚠️ 重要：首次使用需要重启

**Skills 已安装完成，但需要重启 VSCode 才能生效！**

### 重启步骤

1. **完全退出 VSCode**（macOS: `Cmd+Q`）
2. **重新打开 VSCode 和项目**
3. **打开 Claude Code 面板**
4. **测试命令**: `>> /brainstorming 测试`

如果仍然不工作，请查看 [Skills问题排查.md](./Skills问题排查.md)

---

## 如何使用 Skills

### 1. 基本用法

在 Claude Code 中，使用斜杠命令调用 skill：

```
>> /skill-name 帮我完成某个任务
```

或使用自然语言：
```
请使用 skill-name skill 帮我完成某个任务
```

### 2. 推荐的入门 Skills

#### 通用开发
- `/brainstorming` - 在任何创意或构建工作之前使用，帮助规划功能、组件、架构
- `/architecture` - 架构决策框架，分析权衡、记录 ADR
- `/code-refactoring-refactor-clean` - 代码重构专家，遵循 SOLID 原则
- `/testing-patterns` - Jest 测试模式、工厂函数、TDD 工作流

#### 文档和设计
- `/docs-architect` - 从现有代码库创建全面的技术文档
- `/c4-architecture-c4-architecture` - 生成 C4 架构文档
- `/api-documenter` - OpenAPI 3.1 API 文档，生成交互式文档

#### AI 和数据
- `/ai-engineer` - 构建生产级 LLM 应用、RAG 系统、智能 Agent
- `/agent-tool-builder` - 设计 AI Agent 可以有效使用的工具
- `/autonomous-agent-patterns` - 构建自主编码 Agent 的设计模式

#### 前端开发
- `/angular` - 现代 Angular (v20+) 专家，Signals、Standalone 组件
- `/tailwind-patterns` - Tailwind CSS v4 原则和设计模式
- `/react-patterns` - React 最佳实践和设计模式

#### 后端和基础设施
- `/docker-patterns` - Docker 容器化最佳实践
- `/kubernetes-patterns` - K8s 部署、服务网格、Helm charts
- `/postgres-query-optimizer` - PostgreSQL 查询优化

#### 安全
- `/security-audit-patterns` - 安全审计和漏洞扫描
- `/penetration-testing` - 渗透测试方法和工具

### 3. 按角色推荐的 Bundles

根据你的角色，可以重点关注以下 skills：

#### Web 开发者
- `/brainstorming`
- `/angular` 或 `/react-patterns`
- `/tailwind-patterns`
- `/api-documenter`
- `/testing-patterns`

#### 后端工程师
- `/architecture`
- `/postgres-query-optimizer`
- `/docker-patterns`
- `/kubernetes-patterns`
- `/api-documenter`

#### DevOps/SRE
- `/docker-patterns`
- `/kubernetes-patterns`
- `/terraform-patterns`
- `/monitoring-patterns`
- `/incident-response`

#### 安全工程师
- `/security-audit-patterns`
- `/penetration-testing`
- `/vulnerability-scanner`
- `/compliance-patterns`

#### AI/ML 工程师
- `/ai-engineer`
- `/agent-tool-builder`
- `/autonomous-agent-patterns`
- `/rag-patterns`

## 浏览所有 Skills

### 查看完整目录

完整的 Skills 目录位于：
```bash
~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md
```

你可以使用以下命令查看：
```bash
cat ~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md | less
```

### 按分类浏览

```bash
# 查看所有架构相关的 skills
grep -A 100 "## architecture" ~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md | head -70

# 查看所有 AI 相关的 skills
grep -A 200 "## data-ai" ~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md | head -160
```

### 搜索特定 Skill

```bash
# 搜索包含 "postgres" 的 skills
cat ~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md | grep -i "postgres"

# 搜索包含 "react" 的 skills
cat ~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md | grep -i "react"
```

## 高级用法

### 1. 组合使用多个 Skills

你可以在同一个会话中组合使用多个 skills：

```
>> 先用 /brainstorming 帮我规划一个用户认证系统
>> 然后用 /architecture 帮我设计架构
>> 最后用 /security-audit-patterns 检查安全问题
```

### 2. 自定义 Skill

如果需要创建自己的 skill，可以使用：
```
>> /skill-developer 帮我创建一个新的 skill
```

### 3. 更新 Skills

定期更新 skills 库以获取最新功能：
```bash
cd ~/.claude/plugins/repos/antigravity-awesome-skills/
git pull origin main
```

## 常见问题

### Q: Skills 无法识别？
**A:** 确保 Skills 安装在正确的目录。Claude Code 的默认路径是 `~/.claude/plugins/repos/` 或 `~/.claude/skills/`。

### Q: 如何知道某个 Skill 的具体用法？
**A:** 每个 skill 都有详细的文档。你可以查看：
```bash
cat ~/.claude/plugins/repos/antigravity-awesome-skills/skills/<skill-name>/SKILL.md
```

或者直接询问 Claude：
```
>> /skill-name 的详细用法是什么？
```

### Q: Skills 太多了，如何选择？
**A:** 建议：
1. 先阅读本文档的"推荐入门 Skills"部分
2. 根据你的角色查看"按角色推荐的 Bundles"
3. 在 CATALOG.md 中按分类浏览
4. 使用搜索功能找到你需要的 skill

### Q: 可以同时使用多个 Skills 吗？
**A:** 可以。Claude Code 支持在同一个会话中调用多个 skills。

## 参考资源

- **完整文档**: [~/.claude/plugins/repos/antigravity-awesome-skills/README.md](file://~/.claude/plugins/repos/antigravity-awesome-skills/README.md)
- **Skills 目录**: [~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md](file://~/.claude/plugins/repos/antigravity-awesome-skills/CATALOG.md)
- **入门指南**: [~/.claude/plugins/repos/antigravity-awesome-skills/docs/GETTING_STARTED.md](file://~/.claude/plugins/repos/antigravity-awesome-skills/docs/GETTING_STARTED.md)
- **Bundles 推荐**: [~/.claude/plugins/repos/antigravity-awesome-skills/docs/BUNDLES.md](file://~/.claude/plugins/repos/antigravity-awesome-skills/docs/BUNDLES.md)
- **官方仓库**: https://github.com/sickn33/antigravity-awesome-skills

## 贡献

如果你创建了有用的自定义 skills，可以考虑贡献回上游仓库。详见：
```bash
~/.claude/plugins/repos/antigravity-awesome-skills/CONTRIBUTING.md
```

---

**安装时间**: 2026-02-12
**版本**: v5.0.0 Workflows Edition
**Skills 总数**: 845
