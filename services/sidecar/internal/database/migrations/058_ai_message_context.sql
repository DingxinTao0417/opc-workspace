-- 058: persist the exact explicit Task/Project/Client snapshot attached to a
-- user message (ADR-008). The column remains inside the excluded AI
-- operational surface and is null for messages sent without context.
ALTER TABLE ai_messages
ADD COLUMN context_snapshot TEXT
    CHECK (
        context_snapshot IS NULL
        OR (
            json_valid(context_snapshot)
            AND json_type(context_snapshot) = 'object'
            AND length(CAST(context_snapshot AS BLOB)) <= 32768
        )
    );
