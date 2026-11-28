-- +goose Up
-- An approval remembers the request it was spent on (its Idempotency-Key):
-- sending that same request again finds the original result instead of
-- "the approval was used" (docs/CODE_REVIEW_FIX_PLAN.md M08).
ALTER TABLE approvals ADD COLUMN used_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE approvals DROP COLUMN used_key;
