-- 062: exact Provider-returned token counts. NULL means unavailable; the
-- application never estimates tokens from bytes or characters.
ALTER TABLE ai_run_steps ADD COLUMN input_tokens INTEGER
    CHECK (input_tokens IS NULL OR input_tokens >= 0);

ALTER TABLE ai_run_steps ADD COLUMN output_tokens INTEGER
    CHECK (output_tokens IS NULL OR output_tokens >= 0);

ALTER TABLE ai_run_steps ADD COLUMN token_source TEXT
    CHECK (
        (token_source IS NULL AND input_tokens IS NULL AND output_tokens IS NULL)
        OR (token_source = 'provider' AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL)
    );
