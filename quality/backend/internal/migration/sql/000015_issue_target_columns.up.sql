-- Multi-column rule targets must not lose issue evidence to a single-name limit.
ALTER TABLE quality.issues ALTER COLUMN column_name TYPE TEXT;
