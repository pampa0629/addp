# Nginx 配置目录说明

## 模块定位

`nginx/` 保存 ADDP 生产或容器化部署时的反向代理配置，负责前端静态资源、模块前端路径和 API 到 Gateway 的代理。

## 重要文件

```text
nginx/
├── nginx.conf
├── nginx.prod.conf
├── Dockerfile
└── README.md
```

## 开发规则

- API 代理统一指向 Gateway，避免绕过网关认证、限流和审计。
- 新增前端模块生产路径时，同步检查 Console、模块前端 `base` 配置和 Nginx location。
- 修改生产代理配置后，用 `nginx -t` 或容器启动验证语法。
- 不要在 Nginx 配置中硬编码敏感密钥。

## 验证

```bash
nginx -t
docker build -t addp-nginx ./nginx
```

如本机未安装 Nginx，可使用 Docker 容器验证配置。

## 相关文档

- `nginx/README.md`
- `gateway/CLAUDE.md`
- `gateway/docs/gateway架构说明.md`
- `docs/guide/addp部署和开发步骤.md`
