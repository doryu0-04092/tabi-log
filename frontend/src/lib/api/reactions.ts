// いいねとコメントの API を呼ぶ。
//
// 型は docs/openapi.yaml から生成したものを使う。手で書くと、
// 仕様を変えたときに気づかないまま食い違う。

import { request } from '$lib/api/client';
import type { components } from '$lib/api/gen';

export type Comment = components['schemas']['Comment'];

type CommentPage = { comments: Comment[]; nextCursor?: string | null };

/**
 * いいねする。**既にいいねしていても成功する（冪等）。**
 *
 * サーバー側が PUT を冪等に受けるため、連打や再送で失敗しない。
 * 画面側で「既に押したか」を厳密に管理しなくてよい。
 */
export function likePost(postId: number | string): Promise<void> {
	return request<void>(`/posts/${postId}/likes`, { method: 'PUT' });
}

/** いいねを取り消す。いいねしていなくても成功する（冪等）。 */
export function unlikePost(postId: number | string): Promise<void> {
	return request<void>(`/posts/${postId}/likes`, { method: 'DELETE' });
}

/**
 * コメントを古い順に取得する。cursor は前回の nextCursor をそのまま渡す。
 *
 * フィードと違い古い順なのは、会話は上から読むものだからである。
 */
export function listComments(
	postId: number | string,
	cursor?: string | null
): Promise<CommentPage> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<CommentPage>(`/posts/${postId}/comments${query}`);
}

export function createComment(postId: number | string, body: string): Promise<Comment> {
	return request<Comment>(`/posts/${postId}/comments`, { method: 'POST', body: { body } });
}

export function deleteComment(commentId: number | string): Promise<void> {
	return request<void>(`/comments/${commentId}`, { method: 'DELETE' });
}

/** コメントの最大文字数。**サーバー側の上限と揃える。** */
export const MAX_COMMENT_LENGTH = 500;
