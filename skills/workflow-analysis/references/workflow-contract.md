# Workflow Definition 约束

只在构造或修复候选工作流时读取本文件。正式契约以 Develop API、Public Operator Spec 和 `addp.workflow/v1` 规范为准。

## 最小结构

```json
{
  "tasks": [
    {
      "id": "load_source",
      "operator": "load",
      "params": {
        "locator": "addp://engine/12/path/public/source?type=table"
      },
      "depends_on": []
    }
  ]
}
```

每个 task 必须显式包含唯一 `id`、有效 `operator`、对象类型 `params` 和字符串数组 `depends_on`。依赖必须指向已存在 task，且 DAG 不得成环。

## 资源参数

- 读取已有资源时使用 `locator`。
- 创建新资源时使用 `target_parent_locator + target_name`，并按 Public Operator Spec 提供 `write_mode`。
- 不构造尚不存在资源的虚拟 locator。
- 不在 definition 中保存 `connection_info`、密码或其他执行期连接事实。

## 算子与引用

- 只使用 `workflow.operators.list` 返回的算子。
- 只提交 `public_parameters` 中声明的公开参数。
- 上游结果通过运行时正式 `$ref` 结构引用；不要猜测输出端口，按 `output_ports` 选择。
- Spark 等运行时绑定资源放在 `workflow.run` 的执行配置中，不写入 task params。

## 校验循环

1. 提交候选 definition 到 `workflow.validate`。
2. 按返回的稳定错误逐项修正。
3. 重新校验，直到 `valid=true` 或发现必须由用户澄清的业务选择。
4. 不用删除输入、硬编码默认值或更换数据集来规避校验错误。
