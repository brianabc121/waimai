-- +goose Up
-- Customers no longer pick a delivery area from a list: the area follows the
-- pin of the delivery address. Every store works that way now. (Where the
-- platform has no map key there is no pin, and the list is still what is used.)
ALTER TABLE branches ALTER COLUMN zone_mode SET DEFAULT 'map';
UPDATE branches SET zone_mode = 'map' WHERE zone_mode <> 'map';

-- +goose Down
ALTER TABLE branches ALTER COLUMN zone_mode SET DEFAULT 'list';
