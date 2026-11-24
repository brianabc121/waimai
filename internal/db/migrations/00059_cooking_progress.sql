-- +goose Up
-- Cooking progress in the back office (docs/COOKING_PROGRESS_DESIGN.md §8, §9).
-- How long an order should take in the kitchen: the timers on the progress
-- page and the kitchen screen turn yellow at two thirds of it and red after it.
ALTER TABLE branches ADD COLUMN cook_target_minutes INT NOT NULL DEFAULT 15
    CHECK (cook_target_minutes BETWEEN 5 AND 90);

-- The new "see cooking progress" permission is on by default; stores whose
-- owner already set the table get it wherever "see orders" is on.
UPDATE branch_role_perms SET perms = array_append(perms, 'order.progress')
    WHERE 'order.view' = ANY(perms) AND NOT 'order.progress' = ANY(perms);

-- +goose Down
UPDATE branch_role_perms SET perms = array_remove(perms, 'order.progress');
ALTER TABLE branches DROP COLUMN cook_target_minutes;
