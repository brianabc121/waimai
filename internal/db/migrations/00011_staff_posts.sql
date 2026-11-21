-- +goose Up
-- A staff member can now hold several posts in a branch (a kitchen station,
-- the packing counter, rider). The single role that used to describe both
-- "what you do" and "what you may open" is kept only for back-office access:
-- owner, manager, cashier, or plain staff.
CREATE TABLE staff_posts (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    branch_id   BIGINT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('station','packing','rider')),
    station_id  BIGINT,
    CHECK ((kind = 'station') = (station_id IS NOT NULL)),
    FOREIGN KEY (user_id, branch_id) REFERENCES user_branch_roles (user_id, branch_id) ON DELETE CASCADE,
    FOREIGN KEY (station_id, branch_id) REFERENCES kitchen_stations (id, branch_id)
);
CREATE UNIQUE INDEX staff_posts_unique ON staff_posts (user_id, branch_id, kind, COALESCE(station_id, 0));
CREATE INDEX staff_posts_branch_kind ON staff_posts (branch_id, kind);

-- Everyone keeps what they could do before: kitchen staff get every active
-- station plus packing, riders get the rider post.
INSERT INTO staff_posts (user_id, branch_id, kind, station_id)
    SELECT r.user_id, r.branch_id, 'station', s.id FROM user_branch_roles r
    JOIN kitchen_stations s ON s.branch_id = r.branch_id AND s.active WHERE r.role = 'kitchen';
INSERT INTO staff_posts (user_id, branch_id, kind)
    SELECT user_id, branch_id, 'packing' FROM user_branch_roles WHERE role = 'kitchen';
INSERT INTO staff_posts (user_id, branch_id, kind)
    SELECT user_id, branch_id, 'rider' FROM user_branch_roles WHERE role = 'rider';

ALTER TABLE user_branch_roles DROP CONSTRAINT user_branch_roles_role_check;
UPDATE user_branch_roles SET role = 'staff' WHERE role IN ('kitchen','rider');
ALTER TABLE user_branch_roles ADD CONSTRAINT user_branch_roles_role_check
    CHECK (role IN ('owner','manager','cashier','staff'));

-- Deleting a staff member hides the account and frees the username; orders
-- and audit entries keep pointing at the row.
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE user_branch_roles DROP CONSTRAINT user_branch_roles_role_check;
UPDATE user_branch_roles r SET role = CASE WHEN EXISTS (SELECT 1 FROM staff_posts p
    WHERE p.user_id = r.user_id AND p.branch_id = r.branch_id AND p.kind <> 'rider') THEN 'kitchen'
    WHEN EXISTS (SELECT 1 FROM staff_posts p WHERE p.user_id = r.user_id AND p.branch_id = r.branch_id) THEN 'rider'
    ELSE 'kitchen' END WHERE r.role = 'staff';
ALTER TABLE user_branch_roles ADD CONSTRAINT user_branch_roles_role_check
    CHECK (role IN ('owner','manager','cashier','kitchen','rider'));
DROP TABLE IF EXISTS staff_posts;
