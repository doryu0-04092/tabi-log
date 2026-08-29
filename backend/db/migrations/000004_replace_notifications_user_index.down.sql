CREATE INDEX ix_notifications_user_created ON notifications (user_id, created_at DESC, id DESC);
DROP INDEX ix_notifications_user_id ON notifications;
