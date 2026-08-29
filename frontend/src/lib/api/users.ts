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
