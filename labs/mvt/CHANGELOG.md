# Changelog

All notable changes to the MVT project will be documented in this file.

## [Unreleased]

### restart.sh 优化 (2025-11-11)

#### 新增功能
- ✅ **自动端口清理**: 启动前自动检测并清理 8090/5180 端口占用
- ✅ **依赖智能检测**: 前端启动前强制检查 `node_modules` 和关键依赖（vite）
- ✅ **进程存活检查**: 启动后验证进程是否真实运行，避免启动假成功
- ✅ **健康检查增强**: 后端 HTTP 健康检查 + 前端日志关键字检测
- ✅ **错误日志显示**: 启动失败时自动显示最后 20 行日志
- ✅ **状态查询优化**: 双重验证（进程存在 + 端口监听/健康检查）

#### 改进
- ⚡ 增加超时保护：后端 15 秒，前端 15 秒
- ⚡ 移除 `set -e`：允许部分失败（如停止不存在的进程）
- ⚡ 改进错误提示：区分"进程不存在"和"健康检查失败"
- ⚡ 状态显示带 ✓ 标记，一目了然

#### 修复
- 🐛 修复端口被占用导致启动失败的问题
- 🐛 修复前端依赖缺失时启动失败的问题
- 🐛 修复健康检查误报（进程已退出但显示"已启动"）
- 🐛 修复状态查询不准确的问题

#### 文档
- 📝 新增 [RESTART_GUIDE.md](RESTART_GUIDE.md) - 详细使用指南
- 📝 更新 [CLAUDE.md](CLAUDE.md) - 补充脚本优化说明

---

## [0.1.0] - 2025-01-09

### 初始版本
- ✨ 实现 MVT 海量空间数据快显系统
- ✨ PostGIS + Redis 架构
- ✨ MapLibre GL JS 前端渲染
- ✨ Go + Gin 后端
- ✨ 基础启动脚本 restart.sh

---

## 语义化版本说明

- **MAJOR**: 不兼容的 API 变更
- **MINOR**: 向后兼容的功能新增
- **PATCH**: 向后兼容的问题修正

遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)

