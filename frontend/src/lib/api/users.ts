// プロフィールとフォローの API を呼ぶ。
//
// 型は docs/openapi.yaml から生成したものを使う。手で書くと、
// 仕様を変えたときに気づかないまま食い違う。

import { request } from '$lib/api/client';
import type { components } from '$lib/api/gen';
import type { Post } from '$lib/api/posts';

export type UserProfile = components['schemas']['UserProfile'];
export type UserSummary = components['schemas']['UserSummary'];

type UserPage = { users: UserSummary[]; nextCursor?: string | null };
type PostPage = { posts: Post[]; nextCursor?: string | null };

export function getUserProfile(handle: string): Promise<UserProfile> {
	return request<UserProfile>(`/users/${encodeURIComponent(handle)}`);
}

export function listUserPosts(handle: string, cursor?: string | null): Promise<PostPage> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<PostPage>(`/users/${encodeURIComponent(handle)}/posts${query}`);
}

/**
 * フォローする。**既にフォローしていても成功する（冪等）。**
 *
 * いいねと同じ理由で PUT を使う。連打や再送で失敗しない。
 */
export function followUser(handle: string): Promise<void> {
	return request<void>(`/users/${encodeURIComponent(handle)}/follow`, { method: 'PUT' });
}

/** フォローを解除する。フォローしていなくても成功する（冪等）。 */
export function unfollowUser(handle: string): Promise<void> {
	return request<void>(`/users/${encodeURIComponent(handle)}/follow`, { method: 'DELETE' });
}

export function listFollowers(handle: string, cursor?: string | null): Promise<UserPage> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<UserPage>(`/users/${encodeURIComponent(handle)}/followers${query}`);
}

export function listFollowing(handle: string, cursor?: string | null): Promise<UserPage> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<UserPage>(`/users/${encodeURIComponent(handle)}/following${query}`);
}

/** 都道府県の総数。制覇率の分母。 */
export const PREFECTURE_TOTAL = 47;

/** 利用者を探す。ハンドルと表示名を対象に部分一致で探す。 */
export function searchUsers(q: string, cursor?: string | null): Promise<UserPage> {
	const params = new URLSearchParams({ q });
	if (cursor) params.set('cursor', cursor);
	return request<UserPage>(`/search/users?${params}`);
}

export type PrefectureCount = components['schemas']['PrefectureCount'];

/**
 * 都道府県ごとの投稿数を47件すべて取る。
 *
 * **投稿が無い県も含まれる。** 制覇マップは全県のマスを描くため、
 * 画面側で都道府県マスタと突き合わせずに済ませている。
 */
export function listUserPrefectures(handle: string): Promise<{ prefectures: PrefectureCount[] }> {
	return request<{ prefectures: PrefectureCount[] }>(
		`/users/${encodeURIComponent(handle)}/prefectures`
	);
}

/**
 * プロフィールを編集する。
 *
 * **送った項目だけが変わる。** 自己紹介を消すときは空文字を送る。
 */
export function updateProfile(input: {
	displayName?: string;
	bio?: string;
}): Promise<components['schemas']['User']> {
	return request<components['schemas']['User']>('/users/me', { method: 'PATCH', body: input });
}

/**
 * パスワードを変更する。
 *
 * **成功すると全リフレッシュトークンが失効する。** 呼び出した側も
 * 入り直しになるため、画面はログインへ送る。
 */
export function changePassword(input: {
	currentPassword: string;
	newPassword: string;
}): Promise<void> {
	return request<void>('/users/me/password', { method: 'PUT', body: input, csrf: true });
}

/** 退会する。**取り消せない。** */
export function deleteAccount(currentPassword: string): Promise<void> {
	return request<void>('/users/me', {
		method: 'DELETE',
		body: { currentPassword },
		csrf: true
	});
}

/** 旅行履歴（訪問日順）を取る。カーソルは「訪問日_投稿ID」の形。 */
export function listUserTravels(
	handle: string,
	cursor?: string | null
): Promise<{ posts: Post[]; nextCursor?: string | null }> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<{ posts: Post[]; nextCursor?: string | null }>(
		`/users/${encodeURIComponent(handle)}/travels${query}`
	);
}

/**
 * アバターを設定する。
 *
 * **画像は投稿と同じ経路で先にアップロードする**
 * （`uploadImage` が presign → S3 → 処理の完了待ちまで行う）。
 * アバターにも EXIF の除去が要るため、経路を分けない。
 */
export function setAvatar(mediaId: number): Promise<void> {
	return request<void>('/users/me/avatar', { method: 'PUT', body: { mediaId } });
}

/** アバターを外す。設定していなくても成功する（冪等）。 */
export function clearAvatar(): Promise<void> {
	return request<void>('/users/me/avatar', { method: 'DELETE' });
}
