// 通知の API を呼ぶ。
//
// **作成する呼び出しは無い。** 通知はいいね・コメント・フォローと同じ
// トランザクションでサーバー側が作る。画面からは読むことと既読にすることだけ。

import { request } from '$lib/api/client';
import type { components } from '$lib/api/gen';

export type Notification = components['schemas']['Notification'];

type NotificationPage = { notifications: Notification[]; nextCursor?: string | null };

export function listNotifications(cursor?: string | null): Promise<NotificationPage> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<NotificationPage>(`/notifications${query}`);
}

/**
 * 未読の件数だけを取る。
 *
 * **一覧を取らずに数だけ分かる必要がある。** 見出しの数のために
 * 毎回20件ぶんの本体を取ってくるのは無駄が大きい。
 */
export function getUnreadCount(): Promise<{ unreadCount: number }> {
	return request<{ unreadCount: number }>('/notifications/unread-count');
}

/** 1件を既読にする。既に既読でも成功する（冪等）。 */
export function markNotificationRead(id: number): Promise<void> {
	return request<void>(`/notifications/${id}/read`, { method: 'PUT' });
}

/** すべて既読にする。未読が無くても成功する（冪等）。 */
export function markAllNotificationsRead(): Promise<void> {
	return request<void>('/notifications/read', { method: 'PUT' });
}
