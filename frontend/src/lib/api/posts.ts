// 投稿と画像の API を呼ぶ。
//
// 型は docs/openapi.yaml から生成したものを使う。手で書くと、
// 仕様を変えたときに気づかないまま食い違う。

import { request } from '$lib/api/client';
import type { components } from '$lib/api/gen';

export type Post = components['schemas']['Post'];
export type PostMedia = components['schemas']['Media'];
export type Prefecture = components['schemas']['Prefecture'];

type Feed = { posts: Post[]; nextCursor?: string | null };

/** 新着フィードを取得する。cursor は前回の nextCursor をそのまま渡す。 */
export function listPosts(cursor?: string | null): Promise<Feed> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<Feed>(`/posts${query}`);
}

/**
 * フォロー中フィードを取得する。
 *
 * **自分の投稿は含まれない。** 自分自身はフォローできないためである。
 */
export function listFollowingFeed(cursor?: string | null): Promise<Feed> {
	const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
	return request<Feed>(`/feed/following${query}`);
}

export function getPost(postId: number | string): Promise<Post> {
	return request<Post>(`/posts/${postId}`);
}

export function deletePost(postId: number | string): Promise<void> {
	return request<void>(`/posts/${postId}`, { method: 'DELETE' });
}

export function listPrefectures(): Promise<Prefecture[]> {
	return request<Prefecture[]>('/prefectures');
}

export type NewPost = {
	body: string;
	prefectureCode: string;
	spotName?: string | null;
	visitedOn: string;
	tags: string[];
	media: { mediaId: number; altText: string }[];
};

export function createPost(input: NewPost): Promise<Post> {
	return request<Post>('/posts', { method: 'POST', body: input });
}

/**
 * 画像を1枚アップロードし、投稿に使える mediaId を返す。
 *
 * **画像はサーバーを経由せず S3 へ直接送る。** 大きなファイルで
 * バックエンドの帯域とタイムアウトがボトルネックになるのを避けるためである。
 *
 * 送信後、サーバー側（S3 のイベントで起動する処理）が形式を検証し、
 * EXIF を除去して表示用の変換物を作る。**その完了を待つ必要がある。**
 * 完了前に投稿しようとしても、サーバーが受け付けない。
 */
export async function uploadImage(
	file: File,
	onProgress?: (phase: UploadPhase) => void
): Promise<number> {
	onProgress?.('presigning');
	const { mediaId, uploadUrl } = await request<{
		mediaId: number;
		uploadUrl: string;
	}>('/media/presign', {
		method: 'POST',
		body: { contentType: file.type, contentLength: file.size }
	});

	onProgress?.('uploading');
	const response = await fetch(uploadUrl, {
		method: 'PUT',
		// 署名に焼き込まれた値と一致させる必要がある。
		// 違うと S3 が拒否する。
		headers: { 'Content-Type': file.type },
		body: file
	});
	if (!response.ok) {
		throw new Error('画像のアップロードに失敗しました');
	}

	// **S3 への送信が終わっても、まだ投稿には使えない。**
	// 形式の検証・EXIF の除去・変換がサーバー側で非同期に走る。その完了まで待つ。
	onProgress?.('processing');
	await waitUntilProcessed(mediaId);
	return mediaId;
}

/**
 * 画像の処理が終わるまで待つ。
 *
 * 処理は S3 のイベントで起動するため、完了の通知はクライアントに届かない。
 * 状態を問い合わせて確認するしかない。
 *
 * 間隔を少しずつ延ばしているのは、**小さい画像はすぐ終わるのに
 * 一定の長い間隔だと待たされる**一方、大きい画像に短い間隔を続けると
 * 無駄な問い合わせが増えるためである。
 */
async function waitUntilProcessed(mediaId: number): Promise<void> {
	const delays = [300, 500, 800, 1200, 1500, 2000, 2000, 2000, 3000, 3000, 3000, 3000];

	for (const delay of delays) {
		await new Promise((r) => setTimeout(r, delay));
		const result = await request<{ mediaId: number; status: string }>(`/media/${mediaId}`);
		if (result.status === 'processed') return;
		if (result.status === 'failed') {
			throw new Error('この画像は使えませんでした。別の画像をお試しください');
		}
	}
	// 待っても終わらない場合。利用者には「今は使えない」と伝えるしかない。
	throw new Error('画像の処理に時間がかかっています。しばらくしてからやり直してください');
}

export type UploadPhase = 'presigning' | 'uploading' | 'processing';

/** アップロードできる形式。サーバー側の許可と揃える。 */
export const ACCEPTED_IMAGE_TYPES = ['image/jpeg', 'image/png', 'image/webp'];

/** アップロードできる最大バイト数。サーバー側の上限と揃える。 */
export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

/** 投稿に付けられる画像の最大枚数。 */
export const MAX_IMAGES_PER_POST = 4;

/** 投稿を探すときの絞り込み。指定したものだけが効く。 */
export type SearchQuery = {
	q?: string;
	prefectureCode?: string;
	region?: string;
	tag?: string;
	handle?: string;
	visitedFrom?: string;
	visitedTo?: string;
	since?: string;
	sort?: 'latest' | 'popular';
	cursor?: string | null;
};

/**
 * 投稿を探す。
 *
 * **カーソルは並び順ごとに形が違う**（新着は id、人気順は いいね数_id）。
 * 並び順を変えたら、前のカーソルは渡さず先頭から取り直すこと。
 */
export function searchPosts(query: SearchQuery): Promise<Feed> {
	const params = new URLSearchParams();
	for (const [key, value] of Object.entries(query)) {
		if (value !== undefined && value !== null && value !== '') {
			params.set(key, String(value));
		}
	}
	const suffix = params.toString();
	return request<Feed>(`/search/posts${suffix ? `?${suffix}` : ''}`);
}
