# Claude Code Skills 问题排查

## 当前状态

✅ Skills 已正确安装到：`~/.claude/skills/`
✅ 共有 **840 个 skills** 可用
✅ Skills 文件格式正确（SKILL.md）

## 问题

VSCode 扩展版本的 Claude Code 无法识别 `/brainstorming` 等 skills 命令。

## 解决方案

### 方案 1：重启 VSCode（推荐）

1. **完全退出 VSCode**（不是只关闭窗口）
   - macOS: `Cmd+Q` 退出 VSCode
   - 或在菜单栏选择 `Code > Quit`

2. **重新打开 VSCode**

3. **重新打开 Claude Code 面板**

4. **测试 skill**：
   ```
   >> /brainstorming 帮我规划一个功能
   ```

### 方案 2：重新加载窗口

如果不想完全退出 VSCode：

1. 按 `Cmd+Shift+P` 打开命令面板
2. 输入 `Reload Window`
3. 选择 `Developer: Reload Window`
4. 重新打开 Claude Code
5. 测试 skill

### 方案 3：查看可用 Skills

尝试输入：
```
>> /help
```

这应该会列出所有可用的 skills 和命令。

### 方案 4：使用不同的调用方式

如果斜杠命令不工作，尝试自然语言调用：

```
请使用 brainstorming skill 帮我规划一个功能
```

或：

```
Use the @brainstorming skill to help me plan a feature
```

## 验证 Skills 安装

运行以下命令验证 skills 已正确安装：

```bash
# 检查 skills 目录
ls -la ~/.claude/skills/ | head -20

# 检查 brainstorming skill
cat ~/.claude/skills/brainstorming/SKILL.md | head -20

# 统计 skills 数量
ls ~/.claude/skills/ | wc -l
```

预期输出：
- skills 目录存在
- brainstorming/SKILL.md 文件存在
- 约 840 个 skills

## 测试用的简单 Skills

### 测试 1: brainstorming
```
>> /brainstorming 帮我规划 ADDP 平台的用户权限管理功能
```

预期行为：Claude 会引导你通过结构化的头脑风暴过程，而不是直接开始编码。

### 测试 2: architecture
```
>> /architecture 分析 ADDP 的微服务架构设计
```

预期行为：Claude 会使用架构决策框架分析系统设计。

### 测试 3: docs-architect
```
>> /docs-architect 为 system 模块生成文档
```

预期行为：Claude 会分析代码库并生成技术文档。

## 如果仍然不工作

### 检查 Claude Code 版本

在终端运行：
```bash
claude --version
```

确保使用的是最新版本的 Claude Code。

### 检查是否禁用了 slash commands

检查命令行参数中是否有 `--disable-slash-commands`：
```bash
ps aux | grep claude | grep disable-slash-commands
```

如果找到这个参数，说明 skills 被禁用了。

### 手动测试 CLI 版本

尝试直接使用 CLI 而不是 VSCode 扩展：

```bash
claude
>> /brainstorming 测试
```

如果 CLI 版本可以工作，说明问题在 VSCode 扩展配置。

## VSCode 扩展特定配置

检查 VSCode 设置（`settings.json`）中是否有 Claude Code 相关配置：

1. 打开 VSCode 设置（`Cmd+,`）
2. 搜索 "claude"
3. 查找与 skills 或 slash commands 相关的设置

## 相关文件路径

- Skills 安装路径: `~/.claude/skills/`
- 原始仓库路径: `~/.claude/plugins/repos/antigravity-awesome-skills/`
- Skills 使用指南: `~/code/addp/docs/Claude-Skills使用指南.md`
- Skills 目录: `~/.claude/skills/README.md`

## 下一步

如果以上方案都不工作，请尝试：

1. **卸载并重新安装 Claude Code VSCode 扩展**
2. **使用纯 CLI 版本的 Claude Code**
3. **检查 Claude Code 官方文档**关于 skills 的最新说明

## 联系支持

如果问题持续存在，可以：
- 查看官方文档: https://docs.anthropic.com/claude/docs/claude-code
- 报告问题: https://github.com/anthropics/claude-code/issues
