ALTER TABLE idempotency_keys
ADD COLUMN request_hash TEXT;

ALTER TABLE idempotency_keys
ADD COLUMN response_body TEXT;

ALTER TABLE idempotency_keys
ADD COLUMN response_status INTEGER
CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 599);
