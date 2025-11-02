# 🚀 ADDP 快速开始

## 一条命令启动

```bash
./scripts/start-prod.sh
```

就这么简单！

---

## 停止服务

```bash
./scripts/stop-prod.sh
```

---

## 访问系统

启动成功后，在浏览器打开：

**http://localhost:8090**

登录：
- Username: `SuperAdmin`
- Password: `20251001#SuperAdmin`

---

## 常见问题

### Q: Registry 错误？

**A:** 启动 registry:
```bash
docker run -d -p 5001:5000 --restart=always --name registry registry:2
```

### Q: 端口被占用？

**A:** 启动脚本会自动检测并提示您清理

### Q: 服务无法启动？

**A:** 查看日志:
```bash
docker compose -f docker-compose.prod.yml logs -f
```

---

## 更多帮助

查看 [QUICK_START.md](QUICK_START.md) 获取详细说明
