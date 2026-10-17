# ADDP 模块与引擎注册治理后续验收

模块生命周期、Alive/Ready、System 控制面依赖、注册租约与引擎身份规则已进入[术语表](../concepts/addp术语表.md)、[模块架构图](../concepts/addp模块架构图.md)、[新模块开发指南](../spec/addp新模块开发指南.md)及[测试与验收规范](../spec/addp测试与验收规范.md)。本专题不再复制实现清单、旧健康路径或各轮 CI 记录。

## 剩余证据

- [ ] `make test-online ONLINE_SUITE=module-registry-recovery`：专用部署真实验证模块先启动、System/Gateway 晚启动、System 中断、同 instance_id 恢复、路由收敛、注销和零残留。
- [ ] `make test-online ONLINE_SUITE=consumer-engine-recovery`：全量启动后 Configuration/Manager/Service 首次访问成功，同一 Manager iframe 随业务 Engine offline/online 自动恢复，消费方 PID 不变。

两套场景的固定断言见测试规范；进程乱序不能用 API 或人工操作替代，页面恢复不能借用模块租约报告。已有人工联调和确定性门禁只证明其对应范围，尚未取得上述专用 Runner 的真实通过报告。历史 queued 运行不再作为接力对象。

部署准备与 profile 以[Online 后续事项](ADDP统一测试与Online验收体系方案.md)及当前 workflow 为准。取得两套真实报告、核对失败清理并回写最终边界后删除本专题，不保留完成历史。
