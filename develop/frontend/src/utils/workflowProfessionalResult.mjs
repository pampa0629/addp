// Adapt the sole professional table contract; history remains a separate summary.
export function workflowProfessionalResult(execution, expectedID) {
  const result = execution?.metadata?.result
  const table = result?.final_result
  const deadline = result?.professional_result_expires_at
  if (execution?.execution_id !== expectedID || execution?.task_type !== 'workflow'
    || execution?.status !== 'success' || table?.schema_version !== 'addp.workflow.table-result/v1'
    || !Array.isArray(table.columns) || !Array.isArray(table.rows)
    || table.rows.length !== table.row_count || !Number.isFinite(Date.parse(deadline))) {
    throw new Error('workflow_result_unavailable')
  }
  return {
    execution_id: expectedID, plaintext_expires_at: deadline,
    success: true, status: 'success', columns: table.columns,
    rows: table.rows, rows_count: table.row_count, truncated: false
  }
}

export function workflowNumericColumns(result) {
  return (result?.columns || []).filter(column => result.rows.length > 0 && result.rows.every(row => (
    typeof row[column] === 'number' && Number.isFinite(row[column])
    && (!Number.isInteger(row[column]) || Number.isSafeInteger(row[column]))
  )))
}

// Compensated summation keeps mixed magnitudes useful without coercing text or NULL.
export function workflowColumnSum(result, column) {
  if (!workflowNumericColumns(result).includes(column)) return null
  let sum = 0, correction = 0
  for (const row of result.rows) {
    const value = row[column], next = sum + value
    correction += Math.abs(sum) >= Math.abs(value) ? (sum - next) + value : (value - next) + sum
    sum = next
  }
  const total = sum + correction
  return Number.isFinite(total) ? total : null
}
