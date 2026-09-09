-- 060: validated assistant citation snapshots. The payload contains only
-- server-rebuilt references to knowledge chunks that were explicitly
-- confirmed for the generation; raw model control blocks are never stored.
ALTER TABLE ai_messages ADD COLUMN citations_snapshot TEXT
    CHECK (
        citations_snapshot IS NULL
        OR (
            role = 'assistant'
            AND status = 'completed'
            AND length(citations_snapshot) <= 16384
            AND json_valid(citations_snapshot)
            AND json_type(citations_snapshot) = 'object'
            AND json_extract(citations_snapshot, '$.version') = 1
            AND json_extract(citations_snapshot, '$.status') IN ('validated', 'no_evidence', 'missing', 'invalid')
            AND json_type(citations_snapshot, '$.items') = 'array'
            AND json_array_length(citations_snapshot, '$.items') <= 3
        )
    );
