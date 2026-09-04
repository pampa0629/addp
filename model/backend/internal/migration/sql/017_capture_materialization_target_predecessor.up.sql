ALTER TABLE model.materialization_batches
    ADD COLUMN expected_target_marker TEXT;

COMMENT ON COLUMN model.materialization_batches.expected_target_marker IS
    'Exact Model ownership marker observed on the target during prepare; NULL means the target did not exist';
