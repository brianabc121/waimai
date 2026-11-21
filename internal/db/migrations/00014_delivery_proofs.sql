-- +goose Up
-- An optional photo the rider takes at the door. Kept in the database so it
-- is covered by the same backups and needs no shared disk; removed after 30
-- days by the worker's cleanup.
CREATE TABLE delivery_proofs (
    task_id     BIGINT PRIMARY KEY REFERENCES delivery_tasks(id) ON DELETE CASCADE,
    branch_id   BIGINT NOT NULL,
    rider_id    BIGINT NOT NULL REFERENCES users(id),
    image       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX delivery_proofs_created_idx ON delivery_proofs (created_at);

-- +goose Down
DROP TABLE delivery_proofs;
