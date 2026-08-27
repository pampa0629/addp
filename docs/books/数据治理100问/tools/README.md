# 《数据治理100问》发行工具

Markdown 是全书唯一源稿。统一发行工具从同一批源文件生成分篇 DOCX、合订本 DOCX、PDF、EPUB、MOBI 和可离线阅读的 HTML 压缩包，产物默认写入 `发布稿/`，不反向修改源稿。

## 常用命令

在《数据治理100问》目录下执行：

```bash
# 校验源稿目录、编号和延伸阅读链接
make -C ../../.. test-book

# 生成并校验全部发行物
bash tools/publish.sh all

# 只生成一种发行物
bash tools/publish.sh docx
bash tools/publish.sh combined-docx
bash tools/publish.sh pdf
bash tools/publish.sh epub
bash tools/publish.sh mobi
bash tools/publish.sh html

# 只生成指定的分篇 DOCX
bash tools/publish.sh docx 目录.md 000-为什么要写这本书.md \
  01-总则与战略篇/001-数据治理的第一性原理.md

# 校验已经生成的完整发行目录
bash tools/publish.sh verify
```

可通过 `--output-dir` 指定其他输出目录。完整发行目录包括：

- `分篇DOCX/`：包含 `目录.docx`、`000-为什么要写这本书.docx` 和第 001—104 问的独立 DOCX；
- `数据治理100问-合订本.docx`；
- `数据治理100问.pdf`；
- `数据治理100问.epub`；
- `数据治理100问.mobi`；
- `数据治理100问-html.zip`，解压后打开 `index.html` 阅读。

## 链接规则

- 源稿中的目录和“延伸阅读”使用相对 Markdown 链接；
- 分篇 DOCX 统一存放在 `分篇DOCX/`，链接转换为该目录中的兄弟 DOCX，并在页尾增加上一篇、目录、下一篇导航；
- 合订本 DOCX、PDF、EPUB、MOBI 和 HTML 将链接转换为稳定锚点 `contents`、`q000`—`q104`；
- PDF 从合订本 DOCX 转换，保留目录、书签和内部跳转；
- EPUB、MOBI 和 HTML 适合连续阅读，其中 MOBI 由合订本 EPUB 转换，HTML 包可直接作为静态网站发布。

合订本 DOCX、PDF、EPUB、HTML 以及分篇的第000问使用作者笔名“攀爬”。为避免每问重复署名，`目录.docx` 和第001—104问不显示作者，也不写入作者元数据。

## 环境依赖

- Python 3、`python-docx`、Pillow；
- Pandoc；
- Graphviz（渲染 Mermaid 图）；
- LibreOffice（从合订本 DOCX 生成 PDF）；
- Calibre（使用 `ebook-convert` 从合订本 EPUB 生成 MOBI，并校验内部链接）；
- 可选 `pypdf`，安装后发行校验会进一步检查 PDF 页数、书签和链接。

`publish.sh` 会优先使用 Codex 工作区自带的 Python 运行时，其他环境使用 `PATH` 中的 `python3`。Pandoc、Graphviz 的 `dot`、LibreOffice 的 `soffice` 和 Calibre 的 `ebook-convert` 需要可执行。

转换器覆盖本书当前使用的 Mermaid `flowchart` 和 `sequenceDiagram` 语法。以后引入新的 Mermaid 语法时，必须同步扩展转换器和测试。
