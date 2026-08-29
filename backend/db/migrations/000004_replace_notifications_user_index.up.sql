-- 通知の一覧のための索引を、カーソルの形に合わせて置き換える。
--
-- 一覧は WHERE user_id = ? AND id < ? ORDER BY id DESC で引く
-- （カーソルを id だけにする方針はフィードと共通）。
-- ix_notifications_user_created (user_id, created_at DESC, id DESC) は
-- 絞り込みには使えるが並びを解決できず、EXPLAIN に Using filesort が出る
-- （実測、2026-08-29）。
--
-- **足すのではなく置き換えるのは、created_at 順で引く経路が他に無いためである。**
-- 先頭の列が同じ索引を2本持つと、書き込みのたびに両方を更新することになる。
--
-- 000003（posts）と同じ形の修正である。カーソルを id にするなら、
-- 絞り込みの列と id を並べた索引が要る。
CREATE INDEX ix_notifications_user_id ON notifications (user_id, id DESC);
DROP INDEX ix_notifications_user_created ON notifications;
