DROP INDEX notifications_failed;
DROP INDEX notifications_created;
ALTER TABLE notifications DROP COLUMN failed_at;
