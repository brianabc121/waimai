-- +goose Up
-- Role permissions (docs/STAFF_ACCESS_DESIGN.md §6): what a store's managers
-- and order takers may do, set by its owner. No row means the defaults.
CREATE TABLE branch_role_perms (
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('manager', 'cashier')),
    perms      TEXT[] NOT NULL,
    updated_by BIGINT REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (branch_id, role)
);

-- Every change of the table: who, when, which role, what was added and taken away.
CREATE TABLE role_perm_log (
    id         BIGSERIAL PRIMARY KEY,
    branch_id  BIGINT NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    actor_id   BIGINT REFERENCES users(id),
    added      TEXT[] NOT NULL,
    removed    TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX role_perm_log_branch ON role_perm_log (branch_id, id DESC);

-- +goose Down
DROP TABLE role_perm_log;
DROP TABLE branch_role_perms;
