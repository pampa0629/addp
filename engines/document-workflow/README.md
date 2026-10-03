# Document Workflow

`document_workflow` 是 ADDP 文档转换专用工作流运行时，遵循 `addp.workflow/v1` HTTP 协议。LibreOffice 是运行时镜像内部依赖，不是 ADDP Engine Instance、任务 owner 或公共 API。

第一阶段只声明经过真实大体量样例验证的 `pptx -> pdf`，算子 `document_to_pdf` 同时支持 Develop workflow 和 Manager direct 调用。Runtime Operator Spec 消费 `addp.workflow.access-plan/v1`；Manager direct 调用选择 infra PDF 目标并登记 `manager.pptx_pdf`，Develop workflow 选择 Business 目标。Runtime 不解析 ResourceLocator，也不保存 Manager 任务定义。

转换时使用独立临时目录和 LibreOffice profile，默认移除 PPTX 中不会进入静态 PDF 的嵌入音视频流，保留缩略图和海报图片。输出只有通过 PDF 文件头和页数校验后才按目标访问计划发布。

## 启动

```bash
bash scripts/dev/start.sh -document-workflow
bash scripts/dev/restart.sh -document-workflow
```

运行时默认端口为 `8105`，默认单实例并发为 `1`。容器镜像固定安装 LibreOffice 与 Noto 字体，并以非 root 用户运行。主要配置：

- `DOCUMENT_WORKFLOW_PORT`：宿主机开发端口。
- `DOCUMENT_LIBREOFFICE_BIN`：运行时内部 LibreOffice 绝对路径，默认 `/usr/bin/soffice`。
- `DOCUMENT_WORK_DIR`：临时转换工作目录，默认 `/work/document`。
- `DOCUMENT_CONVERSION_TIMEOUT_SECONDS`：单次转换超时，默认 `600`。
- `DOCUMENT_CONVERSION_CONCURRENCY`：单 Runtime 转换并发，默认 `1`。
- `DOCUMENT_OBJECT_STORE_LOOPBACK_HOST`：容器访问宿主机对象存储时替换 loopback host。

普通本地开发容器使用 bridge 网络，经 `host.docker.internal` 访问宿主机 System 和对象存储。Hosted Online 原生 Linux 使用宿主网络，通过 `WORKFLOW_BIND_HOST=127.0.0.1` 只监听回环地址与 `DOCUMENT_WORKFLOW_PORT`，System 和对象存储均通过 `127.0.0.1` 访问，两套 MinIO 继续各自只发布回环端口。非 root、只读根文件系统及临时目录限制继续由启动入口执行。完整契约见 [端口分配规范](../../docs/spec/addp端口分配.md)。

Hosted 开发入口从镜像读取非 root 用户的 UID，并将当次 Document 工作目录的所有者设为该 UID，使 Linux bind mount 可供转换进程写入；不使用放开所有用户写权限的方式。

## 测试

```bash
python3 -m venv engines/document-workflow/.venv
engines/document-workflow/.venv/bin/pip install -r engines/document-workflow/requirements-dev.txt -e common-python
make test-document-workflow
```
