# 预期行为

原 AgentRun 在 owner 拒绝后收到稳定 `approval_rejected`，不得创建 execution。其他 AgentRun 重放同一 approval 必须优先收到 `approval_forbidden`，不得因 approval 已拒绝或已消费而泄露终态；两个阶段均不得生成 ResultRef 或写副作用。
