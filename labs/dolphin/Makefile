.PHONY: help start stop restart logs shell web clean stats demo demo-test demo-stop

# 默认目标
help:
	@echo "DolphinScheduler + 空间分析工作流 - Make 命令"
	@echo ""
	@echo "🚀 快速体验空间分析工作流:"
	@echo "  make demo          - 启动演示环境（DolphinScheduler + 空间算子引擎）"
	@echo "  make demo-test     - 测试空间算子工作流引擎"
	@echo "  make demo-stop     - 停止演示环境"
	@echo ""
	@echo "🎨 UI 拖拽式编排 (推荐给最终用户):"
	@echo "  make api-start     - 启动空间算子 API 服务"
	@echo "  make api-test      - 测试 API 服务"
	@echo "  make api-list      - 查看可用算子列表"
	@echo ""
	@echo "基础命令:"
	@echo "  make start         - 启动 DolphinScheduler 容器"
	@echo "  make web           - 打开 Web UI (浏览器)"
	@echo "  make logs          - 查看实时日志"
	@echo "  make stats         - 查看资源占用（CPU/内存）"
	@echo "  make stop          - 停止服务"
	@echo "  make restart       - 重启服务"
	@echo "  make clean         - 删除所有容器和数据（危险）"
	@echo ""
	@echo "访问信息:"
	@echo "  Web UI:   http://localhost:12345/dolphinscheduler/ui"
	@echo "  默认账号: admin / dolphinscheduler123"
	@echo ""
	@echo "📚 文档:"
	@echo "  UI_BASED_WORKFLOW_GUIDE.md     - UI 拖拽式编排指南 (推荐)"
	@echo "  QUICKSTART.md                  - 5 分钟快速入门"
	@echo "  DOLPHIN_INTEGRATION_GUIDE.md   - 完整集成指南"
	@echo "  WORKFLOW_ENGINE_GUIDE.md       - 工作流引擎使用指南"

# 启动服务
start:
	@echo "==> 启动 DolphinScheduler..."
	docker-compose up -d
	@echo ""
	@echo "==> 等待服务启动（大约 30-60 秒）..."
	@sleep 10
	@echo ""
	@echo "✓ DolphinScheduler 正在启动"
	@echo ""
	@echo "访问地址:"
	@echo "  http://localhost:12345/dolphinscheduler/ui"
	@echo ""
	@echo "默认登录:"
	@echo "  用户名: admin"
	@echo "  密码:   dolphinscheduler123"
	@echo ""
	@echo "提示: 首次启动需要初始化数据库，请等待约 1 分钟"
	@echo "      使用 'make logs' 查看启动进度"

# 停止服务
stop:
	@echo "==> 停止 DolphinScheduler..."
	docker-compose down
	@echo "✓ 服务已停止"

# 重启服务
restart:
	@echo "==> 重启 DolphinScheduler..."
	docker-compose restart
	@echo "✓ 服务已重启"

# 查看日志
logs:
	@echo "==> 查看 DolphinScheduler 日志（Ctrl+C 退出）..."
	docker-compose logs -f

# 进入容器
shell:
	@echo "==> 进入 DolphinScheduler 容器..."
	docker exec -it dolphin-standalone bash

# 打开 Web UI
web:
	@echo "==> 打开 DolphinScheduler Web UI..."
	@echo "如果浏览器未自动打开，请访问:"
	@echo "  http://localhost:12345/dolphinscheduler/ui"
	@open http://localhost:12345/dolphinscheduler/ui || xdg-open http://localhost:12345/dolphinscheduler/ui || echo "请手动打开浏览器访问"

# 清理所有数据（危险操作）
clean:
	@echo "警告: 这将删除所有容器、数据卷和网络！"
	@echo "按 Ctrl+C 取消，或按 Enter 继续..."
	@read confirm
	@echo "==> 停止并删除容器..."
	docker-compose down -v
	@echo "==> 删除所有相关资源..."
	@docker volume ls -q | grep dolphin | xargs -r docker volume rm || true
	@echo "✓ 清理完成"

# 检查服务状态
.PHONY: status health stats
status:
	@echo "==> DolphinScheduler 服务状态:"
	@docker-compose ps

health:
	@echo "==> 检查健康状态..."
	@curl -f http://localhost:12345/dolphinscheduler/ui > /dev/null 2>&1 && \
		echo "✓ 服务正常运行" || \
		echo "✗ 服务未响应（可能还在启动中）"

# 查看资源占用
stats:
	@echo "==> DolphinScheduler 资源占用情况:"
	@docker stats dolphin-standalone --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}"
	@echo ""
	@echo "内存配置: 1GB 堆内存（学习环境优化）"
	@echo "详细说明: docs/MEMORY_OPTIMIZATION.md"

# =============================================================================
# 演示环境命令（DolphinScheduler + 空间算子工作流引擎）
# =============================================================================

demo:
	@echo "==> 启动空间分析工作流演示环境..."
	@bash scripts/start-demo.sh

demo-test:
	@echo "==> 测试空间算子工作流引擎..."
	@docker-compose -f docker-compose-demo.yml exec dolphinscheduler bash /scripts/test-in-container.sh

demo-stop:
	@echo "==> 停止演示环境..."
	@docker-compose -f docker-compose-demo.yml down
	@echo "✓ 演示环境已停止"

demo-logs:
	@echo "==> 查看演示环境日志（Ctrl+C 退出）..."
	@docker-compose -f docker-compose-demo.yml logs -f

demo-clean:
	@echo "警告: 这将删除演示环境的所有容器和数据！"
	@echo "按 Ctrl+C 取消，或按 Enter 继续..."
	@read confirm
	@echo "==> 停止并删除演示环境..."
	@docker-compose -f docker-compose-demo.yml down -v
	@echo "✓ 演示环境清理完成"

# 本地运行示例（不需要 Docker）
demo-examples:
	@echo "==> 运行空间算子工作流示例..."
	@cd backend && python3 examples/comprehensive_demo.py

demo-performance:
	@echo "==> 运行性能对比测试..."
	@cd backend && python3 examples/performance_test.py

# =============================================================================
# API 服务命令（用于 UI 拖拽式编排）
# =============================================================================

api-start:
	@echo "==> 启动空间算子 API 服务..."
	@bash scripts/start-api.sh

api-test:
	@echo "==> 测试 API 服务..."
	@curl -f http://localhost:5001/health && echo "" && echo "✅ API 服务正常运行"

api-list:
	@echo "==> 获取可用算子列表..."
	@curl -s http://localhost:5001/operators | python3 -m json.tool

