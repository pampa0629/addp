要把 ADDP 部署到完全离线的内网，需要先把依赖打包好，再在内网搭出本地镜像库。可以按下面步骤操作：

1. 盘点并拉取所有镜像

外网环境下执行：

进入仓库根目录，运行 docker compose --profile full config，罗列出所有需要的镜像（Postgres、Redis、MinIO 这些基础镜像，以及我们自己 build 的系统、门户、元数据等镜像）。
对每个镜像执行 docker pull <镜像名>:<tag> 拉下来。
用 docker save 打包成 tar 包，例如：
docker save \
  postgres:15-alpine \
  redis:6.2.19-alpine \
  minio/minio:latest \
  addp-system-backend:latest \
  addp-system-frontend:latest \
  addp-manager-backend:latest \
  addp-manager-frontend:latest \
  addp-meta-backend:latest \
  addp-meta-frontend:latest \
  addp-gateway:latest \
  > addp-images.tar
同时把这些镜像对应的 Dockerfile、入口脚本、.env、docker-compose.yml 复制出来，后续在内网直接用。
如果需要在内网重新构建镜像（如 Go 包、npm 包未提前拉取），可以在外网离线下载 go mod、npm 依赖并打包；不过推荐直接在外网完成镜像构建，内网只用现成镜像。

2. 传输镜像、程序包和数据

把 addp-images.tar、项目代码、配置文件打包，用移动介质带到内网。若还有初始化数据（如业务库 SQL、MinIO 文件），一并准备。

3. 搭建内网私有镜像库

在内网某台服务器上部署 Docker Registry：

docker run -d --restart=always --name registry \
  -p 5000:5000 \
  -v /opt/registry/data:/var/lib/registry \
  registry:2
这台机器将作为统一镜像源（例如 registry.intra.company:5000）。若内网对 HTTPS 有要求，可在这个步骤加证书。

4. 导入镜像到本地仓库

在内网环境：

把 addp-images.tar 拷贝到服务器，执行：
docker load < addp-images.tar
给每个镜像重新打 tag 指向本地仓库：
docker tag addp-system-backend:latest registry.intra.company:5000/addp-system-backend:latest
# 依次处理所有镜像
推送到私服：
docker push registry.intra.company:5000/addp-system-backend:latest
# 其他镜像同理
推送成功后，内网所有节点只需访问 registry.intra.company:5000 就能拉到镜像。

5. 修改 compose / 部署脚本指向私服

在内网部署前，把 docker-compose.yml 中的镜像地址替换成私服地址，例如：

image: registry.intra.company:5000/addp-system-backend:latest
或添加 image: 字段，避免每次都现 build。然后运行：

docker compose --profile full pull  # 从私服拉镜像
docker compose --profile full up -d
没有网络时不能用 docker compose build，因此推荐直接引用已 push 的镜像。

6. 准备离线依赖

若还需要 go 工具链、node/npm 等，可提前在外网下载对应的 tar 包（go 官方安装包、Node LTS pkg），内网解压配置。
对于 PostgreSQL 初始化脚本、MinIO bucket、业务库数据，最好提前准备 SQL、CSV 等离线文件。
7. 后续维护

每次外网更新镜像后，重复“pull → save → load → tag → push”的流程同步到内网。
若需要更规范的流程，可使用 Harbor、Nexus 这类带权限管理/漏洞扫描的企业级镜像库替代 registry:2。
这样一来，内网完全不用访问公网，直接从私服获取镜像，照常 docker compose up 即可部署 ADDP。