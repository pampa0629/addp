# 调整 DolphinScheduler CPU 阈值配置

## 放宽 CPU 限制

```bash
# 1. 进入容器
docker exec -it dolphin-standalone bash

# 2. 编辑配置文件
vi /opt/dolphinscheduler/conf/common.properties

# 3. 找到以下配置并修改:

# 禁用 Master CPU 负载检查（慎用）
master.max.cpu.load.avg=-1

# 或者提高 CPU 使用率阈值（默认 0.7，可改为 0.95）
master.max.cpu.usage.percentage=0.95
worker.max.cpu.usage.percentage=0.95

# 4. 重启容器使配置生效
exit
docker restart dolphin-standalone

# 5. 等待约 1 分钟让服务完全启动
sleep 60
docker logs dolphin-standalone --tail 20
```

## 注意事项

- 放宽 CPU 限制可能导致系统不稳定
- 建议先尝试重新运行工作流
- 如果频繁 CPU 过载，考虑优化任务或增加资源
