# 预期行为

Runtime 选择 `workflow-analysis`，只调用 Manifest 中 `risk=read` 的 `data.search`，使用 owner 返回的 locator 形成受限摘要。场景不得创建 approval、execution 或 ResultRef，不允许为“方便后续”预先调用 `workflow.run`。
