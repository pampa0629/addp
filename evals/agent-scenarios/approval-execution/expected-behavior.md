# 预期行为

首次 `workflow.run` 只创建 Develop approval，AgentRun 进入 `waiting`，execution 数仍为 0。Owner 批准后恢复同一 AgentRun，第二次调用只提交 approval ID 与请求指纹，最终只创建一个 execution ResultRef；Interaction、step 投影和 checkpoint 不保存完整 workflow payload。
