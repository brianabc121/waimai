-- +goose Up
-- What a store counts as a slow dish and as a detour in the work summaries.
ALTER TABLE branches
    ADD COLUMN kitchen_slow_minutes INT NOT NULL DEFAULT 15 CHECK (kitchen_slow_minutes BETWEEN 1 AND 120),
    -- a trip is a detour when it is this much longer than planned, in share and in metres
    ADD COLUMN detour_percent       INT NOT NULL DEFAULT 30 CHECK (detour_percent BETWEEN 5 AND 300),
    ADD COLUMN detour_meters        INT NOT NULL DEFAULT 1000 CHECK (detour_meters BETWEEN 100 AND 20000);

-- +goose Down
ALTER TABLE branches DROP COLUMN kitchen_slow_minutes, DROP COLUMN detour_percent, DROP COLUMN detour_meters;
