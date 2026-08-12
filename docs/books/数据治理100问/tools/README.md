# Markdown 转 DOCX 工具

本目录中的工具用于将《数据治理100问》的 Markdown 源稿转换为统一版式的 Word 发布稿。Markdown 始终是唯一源稿，DOCX 统一输出到 `发布稿/`。

## 常用命令

在《数据治理100问》目录下执行：

```bash
# 转换大纲和所有已经编写的问题
bash tools/convert.sh --all

# 转换单篇文章
bash tools/convert.sh 01-总则与战略篇/001-数据治理的第一性原理.md

# 同时转换指定的多个文件
bash tools/convert.sh 大纲.md 02-概念辨析篇/014-数据治理和BI.md
```

默认输出目录为 `发布稿/`，所有文章采用扁平化文件名，便于逐篇发布。大纲和文章中的本地 Markdown 链接会转换为发布稿目录下对应的 DOCX 链接。

## 环境依赖

- Python 3
- `python-docx`
- Pillow
- Pandoc
- Graphviz（用于把文中的 Mermaid `flowchart` 转换为图片）

在 Codex 工作区中，`convert.sh` 会优先使用工作区自带的 Python 运行时；其他环境会使用 `PATH` 中的 `python3`。Pandoc 和 Graphviz 的 `dot` 命令需要位于 `PATH` 中。

当前支持本书使用的 Mermaid 流程图语法，包括 `flowchart LR`、`flowchart TB`、普通箭头、双向箭头以及带文字的虚线箭头。如果以后引入更复杂的 Mermaid 图形，需要同步扩展转换器。
