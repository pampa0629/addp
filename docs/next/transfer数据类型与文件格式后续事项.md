# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-15

只保留未决事项。

1. Transfer 不能继续只按 connector type 或具体格式名路由，后续要稳定成 `TransferPlanner -> engine capability -> common/resource -> FormatPlugin / reader -> pipeline` 的链路。
2. `ExecutionEngineService` 里的 connector 推断逻辑还要继续迁入 planner。
3. `TransferPlan` 还要把 source / target 拆成 engine、resource、data_type、format、spatial、policy。
4. `mode` 断裂问题还要继续归入 planner，不能只靠测试或局部字段修补。
5. `geojson` 口径还要保持为 `format=json + spatial.target_encoding=geojson`，不要恢复顶层 `format=geojson`。
