# 插件化架构重构总结

本文档记录了 ADDP 各模块插件目录的统一重构。

## 重构目标

1. **统一目录结构**：所有模块将插件相关代码统一放在 `<module>/backend/plugins` 或 `<module>/frontend/plugins` 目录
2. **避免代码重复**：提取共享的格式解析逻辑到 `common/format/parsers`
3. **提升可扩展性**：用户可以更容易地在 plugins 目录添加自定义插件

## 新的目录结构总览

| 模块 | 旧路径 | 新路径 |
|------|--------|--------|
| Meta extractors | `internal/scanner/extractors` | `backend/plugins/extractors` |
| Meta office/video | `internal/plugins/{office,video}extractor` | `backend/plugins/extractors/{office,video}` |
| Meta scanners | `internal/scanner/*_scanner.go` | `backend/plugins/scanners` |
| Manager loaders | `internal/service/*loader.go` | `backend/plugins/loaders` |
| Manager providers | `internal/service/preview_provider_*.go` | `backend/plugins/providers` |
| Manager frontend | `frontend/public/plugins` | `frontend/plugins` |
| Transfer readers | `internal/connector/*_reader.go` | `backend/plugins/readers` |
| Transfer writers | `internal/connector/*_writer.go` | `backend/plugins/writers` |

## 重构完成日期

2025-01-29
