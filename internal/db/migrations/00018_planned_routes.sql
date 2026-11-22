-- +goose Up
-- The planned route of a trip can come from Google. planned_tries counts the
-- calls that failed, so a trip falls back to the straight-line estimate
-- instead of being asked for again and again. planned_return_m is the last
-- leg, back to the store: it is left out when a rider was not seen coming back.
ALTER TABLE rider_trips
    ADD COLUMN planned_tries    INT NOT NULL DEFAULT 0,
    ADD COLUMN planned_return_m INT;

-- +goose Down
ALTER TABLE rider_trips DROP COLUMN planned_tries, DROP COLUMN planned_return_m;
