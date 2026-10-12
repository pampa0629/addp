-- A continuous execution UUID identifies one runtime session. Earlier continuous
-- claims recorded started_at but did not increment the shared attempt counter.
-- Normalize only executions with evidence that the session actually started;
-- pending / unstarted cancellations and bounded attempts retain their facts.
UPDATE common.task_executions
SET attempt = 1
WHERE execution_boundary = 'continuous'
  AND status IN ('running', 'success', 'failed', 'cancelled', 'timeout')
  AND started_at IS NOT NULL
  AND attempt = 0;
