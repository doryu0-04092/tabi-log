// ログイン状態を保持する。
//
// アクセストークン本体は client.ts がモジュール変数として持ち、ここでは
// 画面に描画する利用者情報だけをリアクティブに持つ。
// 秘密と表示用データを分けておくと、うっかり画面に出す経路ができにくい。

import {
	ApiError,
	request,
	refreshAccessToken,
	setAccessToken,
	setRefreshListener
} from '$lib/api/client';
import type { components } from '$lib/api/gen';

export type User = components['schemas']['User'];
type AuthPayload = { accessToken: string; expiresIn: number; user: User };

let currentUser = $state<User | null>(null);

/**
 * 起動時の復元が終わったかどうか。
 *
 * これが false の間に「未ログイン」として画面を出すと、
 * **リロードのたびに一瞬ログイン画面が見える**。復元の完了を待ってから描画する。
 */
let restored = $state(false);

export const session = {
	get user(): User | null {
		return currentUser;
	},
	get isAuthenticated(): boolean {
		return currentUser !== null;
	},
	get restored(): boolean {
		return restored;
	}
};

// リフレッシュはクライアント側でも起きる（401 を受けた時）。
// そのとき利用者情報も一緒に返るので、ここへ反映する。
setRefreshListener((user) => {
	currentUser = (user as User | null) ?? null;
});

/**
 * 起動時にセッションを復元する。
 *
 * アクセストークンはメモリにしか無いためリロードで消える。
 * Cookie のリフレッシュトークンは残っているので、そこから取り直す。
 * 未ログインなら失敗するだけで、例外にはしない。
 */
export async function restoreSession(): Promise<void> {
	if (restored) return;
	try {
		await refreshAccessToken();
	} finally {
		restored = true;
	}
}

export async function signup(input: {
	email: string;
	handle: string;
	displayName: string;
	password: string;
}): Promise<void> {
	const data = await request<AuthPayload>('/auth/signup', {
		method: 'POST',
		body: input
	});
	applySession(data);
}

export async function login(email: string, password: string): Promise<void> {
	const data = await request<AuthPayload>('/auth/login', {
		method: 'POST',
		body: { email, password }
	});
	applySession(data);
}

/**
 * ログアウトする。
 *
 * サーバー側の失効に失敗しても、**クライアント側の状態は必ず消す**。
 * 「ログアウトを押したのにログインしたままに見える」のは、
 * 共用の端末では実害につながる。
 */
export async function logout(): Promise<void> {
	try {
		await request<null>('/auth/logout', {
			method: 'POST',
			csrf: true,
			retryOnUnauthorized: false
		});
	} catch (e) {
		// 既にトークンが無効な場合など。利用者に見せる必要はない。
		if (!(e instanceof ApiError)) throw e;
	} finally {
		setAccessToken(null);
		currentUser = null;
	}
}

function applySession(data: AuthPayload): void {
	setAccessToken(data.accessToken);
	currentUser = data.user;
	restored = true;
}

/**
 * 保持している利用者情報を差し替える。
 *
 * プロフィールを編集したあとに呼ぶ。**呼ばないとヘッダーの表示名が
 * 古いまま残り、保存できたのか分からなくなる。**
 */
export function setSessionUser(user: User): void {
	currentUser = user;
}

/**
 * 未読の件数を 0 として扱うよう、購読している画面へ知らせる。
 *
 * **通知の一覧を開いた時点でヘッダーの印を消すために使う。**
 * ヘッダーは画面を移るたびに件数を取り直すが、同じ画面に留まったままでは
 * 取り直されない。既読にした側から明示的に伝える。
 */
let unreadListener: (() => void) | null = null;

export function onUnreadCleared(fn: () => void): void {
	unreadListener = fn;
}

export function clearUnreadBadge(): void {
	unreadListener?.();
}
