# Document Workflow

`document_workflow` 是 ADDP 文档转换专用工作流运行时，遵循 `addp.workflow/v1` HTTP 协议。LibreOffice 是运行时内部依赖，不是 ADDP Engine Instance、任务 owner 或公共 API。

第一阶段只声明经过真实大体量样例验证的 `pptx -> pdf`，算子 `document_to_pdf` 同时支持 Develop workflow 和 Manager direct 调用。Runtime Operator Spec 消费 `addp.workflow.access-plan/v1`；Manager direct 调用选择 infra PDF 目标并登记 `manager.pptx_pdf`，Develop workflow 选择 Business 目标。Runtime 不解析 ResourceLocator，也不保存 Manager 任务定义。

转换时使用独立临时目录和 LibreOffice profile，默认移除 PPTX 中不会进入静态 PDF 的嵌入音视频流，保留缩略图和海报图片。输出只有通过 PDF 文件头和页数校验后才按目标访问计划发布。

## 启动

```bash
bash scripts/dev/start.sh -document-workflow
bash scripts/dev/restart.sh -document-workflow
```

运行时默认端口为 `8105`，默认单实例并发为 `1`。开发态只处理可信文件，以当前非 root 用户运行；独立 Python venv、LibreOffice profile 和临时目录不提供文件系统沙箱。生产 Compose 继续使用非 root、只读根文件系统、能力限制和私有临时目录的正式镜像。

开发态通过 `scripts/dev/document-workflow.sh` 统一准备和启动。Python 3.12 使用独立 `venv`，完整依赖沿用平台安装锁与指纹。官方 LibreOffice 26.8.0.3 和 Noto CJK Sans 2.004 下载包逐项核对 SHA-256，安装到 `.dev-state/document-native`，不依赖宿主机全局 LibreOffice 或系统字体。私有 Fontconfig 配置绑定包内字体与固定中文字体；每次转换仍使用独立 profile。原生包当前支持 macOS arm64/x86_64 和 Linux x86_64；Linux 需要发行版提供的图形/字体系统库，Hosted Runner 由既有环境准备入口安装。

start/restart/stop 只管理原生 PID。准备失败在停止旧进程之前退出，运行中的依赖只允许校验；需要更新时先停止再启动。首次迁移由用户停止并删除旧 `document-workflow-engine` 开发容器，脚本不会接管该容器，也不保留开发镜像备选路径。直接使用 NFS 的宿主机路径，通过回环地址访问 System 和对象存储；不需要 Docker host gateway 改写。

主要配置：

- `DOCUMENT_WORKFLOW_PORT`：已分配的开发端口，回环监听。
- `DOCUMENT_LIBREOFFICE_BIN`：由开发入口绑定私有官方包中的绝对路径；生产镜像为 `/usr/bin/soffice`。
- `DOCUMENT_WORK_HOST_PATH`：开发临时转换目录，默认 `data/document-work`。
- `DOCUMENT_CONVERSION_TIMEOUT_SECONDS`：单次转换超时，默认 `600`。
- `DOCUMENT_CONVERSION_CONCURRENCY`：单 Runtime 转换并发，默认 `1`。
- `DOCUMENT_OBJECT_STORE_LOOPBACK_HOST`：仅供容器部署使用，原生开发入口清除该变量。

正式启动前的准备与真实转换预检（不启动服务）：

```bash
ROOT_DIR="$PWD"
source scripts/dev/ports.sh
source scripts/dev/lifecycle-lock.sh
source scripts/dev/document-workflow.sh
addp_prepare_document_workflow
```

## 测试

```bash
python3 -m venv engines/document-workflow/.venv
engines/document-workflow/.venv/bin/pip install -r engines/document-workflow/requirements-dev.txt -e common-python
make test-document-workflow
```
