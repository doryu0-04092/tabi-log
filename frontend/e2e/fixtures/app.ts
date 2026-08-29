// E2E で繰り返し使う操作をまとめる。
//
// 「登録する」「投稿を1件作る」は3つのテストファイルで必要になったため
// ここへ切り出した。各ファイルに写しを置くと、画面の文言を変えたときに
// 直し忘れた側だけが落ちる。

import { expect, type APIRequestContext, type Page } from '@playwright/test';
import { PNG } from './png';

export type TestUser = { email: string; handle: string; password: string };

/**
 * テストごとに衝突しない識別子を作る。
 *
 * prefix はどのテストが作った利用者かをデータベース上で見分けるためのもの。
 */
export function unique(prefix = 'e2e'): TestUser {
	const n = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
	return {
		email: `${prefix}${n}@example.test`,
		handle: `${prefix}_${n}`.slice(0, 30),
		password: 'password12345'
	};
}

/**
 * 新規登録してログイン状態にする。
 *
 * **表示名で本人確認まで行う。** 「ログアウト」ボタンが見えるだけだと、
 * 既存のセッションが復元されただけの状態と区別できない。
 */
export async function signup(
	page: Page,
	opts: { user?: TestUser; displayName?: string } = {}
): Promise<TestUser> {
	const user = opts.user ?? unique();
	const displayName = opts.displayName ?? 'たびびと';

	await page.goto('/signup');
	await page.getByLabel('メールアドレス').fill(user.email);
	await page.getByLabel('ハンドル').fill(user.handle);
	await page.getByLabel('表示名').fill(displayName);
	await page.getByLabel('パスワード').fill(user.password);
	await page.getByRole('button', { name: '登録する' }).click();

	await expect(
		page.getByRole('navigation', { name: 'アカウント' }).getByText(displayName)
	).toBeVisible();

	return user;
}

/** 投稿を1件作る。作成後の投稿詳細ページに遷移した状態で返る。 */
export async function createPost(
	page: Page,
	opts: { body: string; prefecture: string }
): Promise<void> {
	await page.goto('/posts/new');

	// 画像を選ぶ。実ファイルを置かずにメモリ上のバイト列を渡す。
	await page.setInputFiles('#photos', {
		name: 'photo.png',
		mimeType: 'image/png',
		buffer: PNG
	});

	// **送信が終わっても、まだ投稿には使えない。**
	// 形式の検証と EXIF の除去がサーバー側で走り、
	// 完了して初めて「使えます」になる。
	await expect(page.getByText('使えます')).toBeVisible({ timeout: 30_000 });

	await page.getByLabel('都道府県（必須）').selectOption({ label: opts.prefecture });
	await page.getByLabel('訪問日').fill('2026-05-03');
	await page.getByLabel('本文（必須）').fill(opts.body);
	await page.getByRole('button', { name: '投稿する' }).click();

	await expect(page).toHaveURL(/\/posts\/\d+$/);
}

/**
 * フォローを切り替え、**サーバーが受け付けるまで待つ。**
 *
 * ボタンは応答を待たずに見た目を変える（押してから数百ミリ秒あとに
 * 変わると二度押しされるため）。つまり `aria-pressed` が変わったことは
 * **サーバーに届いた証拠にならない。** 待たずに画面を移ると、
 * 送信中のリクエストが打ち切られることがある。
 */
export async function toggleFollow(page: Page, displayName: string, want: boolean): Promise<void> {
	const button = page.getByRole('button', { name: new RegExp(`^${displayName}を`) });
	const accepted = page.waitForResponse(
		(r) =>
			r.url().includes('/follow') &&
			r.request().method() === (want ? 'PUT' : 'DELETE') &&
			r.status() === 204
	);
	await button.click();
	await accepted;
	await expect(button).toHaveAttribute('aria-pressed', String(want));
}

/** いいねを切り替え、**サーバーが受け付けるまで待つ。** 理由は toggleFollow と同じ。 */
export async function toggleLike(page: Page, want: boolean): Promise<void> {
	const button = page.getByRole('button', { name: /^いいね/ });
	const accepted = page.waitForResponse(
		(r) =>
			r.url().includes('/likes') &&
			r.request().method() === (want ? 'PUT' : 'DELETE') &&
			r.status() === 204
	);
	await button.click();
	await accepted;
	await expect(button).toHaveAttribute('aria-pressed', String(want));
}

/*
 * ------------------------------------------------------------------
 * ここから下は API を直接叩く補助。
 *
 * **画面を経由せずに用意するのは「量が要る前提」を作るときだけ**にする。
 * 画面から作れるものを API で作ると、画面が壊れていても気づけない。
 * 無限スクロールのように 1 ページ（20件）を超える件数が要る場合、
 * 画面から 21 件作ると1テストで数分かかり、検証したい対象と関係のない
 * 待ち時間で不安定になる。
 *
 * **Cookie はブラウザのものを共有している**（`api` を使うため）。
 * API で登録すればリフレッシュ用の Cookie がブラウザに載り、
 * そのまま `page.goto('/')` でログイン済みとして開ける。
 * ------------------------------------------------------------------
 */

/** API で登録し、アクセストークンを得る。画面は開かない。 */
export async function signupViaApi(
	api: APIRequestContext,
	opts: { user?: TestUser; displayName?: string } = {}
): Promise<{ user: TestUser; token: string }> {
	const user = opts.user ?? unique();
	const response = await api.post('/api/auth/signup', {
		data: {
			email: user.email,
			handle: user.handle,
			displayName: opts.displayName ?? 'たびびと',
			password: user.password
		}
	});
	expect(response.status(), await response.text()).toBe(201);
	const body = await response.json();
	return { user, token: body.data.accessToken };
}

/**
 * 画像を1枚アップロードし、使える状態になった mediaId を返す。
 *
 * 画面側の `uploadImage` と同じ3段階（presign → S3 へ PUT → 完了待ち）を辿る。
 * **完了待ちを省けない**のは、処理が S3 のイベントで非同期に走るためである。
 */
async function uploadViaApi(api: APIRequestContext, token: string): Promise<number> {
	const auth = { Authorization: `Bearer ${token}` };

	const presigned = await api.post('/api/media/presign', {
		headers: auth,
		data: { contentType: 'image/png', contentLength: PNG.length }
	});
	expect(presigned.status(), await presigned.text()).toBe(201);
	const { mediaId, uploadUrl } = (await presigned.json()).data;

	const put = await api.put(uploadUrl, {
		headers: { 'Content-Type': 'image/png' },
		data: PNG
	});
	expect(put.ok(), `S3 への PUT が失敗した: ${put.status()}`).toBeTruthy();

	// 完了するまで問い合わせる。上限を切って、無限に待たない。
	// **画面側の待ち時間（30秒）に合わせる。** テストが並列に走ると
	// 画像処理が重なり、10秒では足りずに落ちた（2026-08-29）。
	for (let i = 0; i < 120; i++) {
		await new Promise((r) => setTimeout(r, 250));
		const state = await api.get(`/api/media/${mediaId}`, { headers: auth });
		const status = (await state.json()).data.status;
		if (status === 'processed') return mediaId;
		if (status === 'failed') throw new Error(`media ${mediaId} の処理が失敗した`);
	}
	throw new Error(`media ${mediaId} の処理が終わらなかった`);
}

/** API で投稿を1件作り、投稿 ID を返す。 */
export async function createPostViaApi(
	api: APIRequestContext,
	token: string,
	opts: { body: string; prefectureCode?: string }
): Promise<number> {
	const mediaId = await uploadViaApi(api, token);
	const response = await api.post('/api/posts', {
		headers: { Authorization: `Bearer ${token}` },
		data: {
			body: opts.body,
			prefectureCode: opts.prefectureCode ?? '01',
			visitedOn: null,
			tags: [],
			media: [{ mediaId }]
		}
	});
	expect(response.status(), await response.text()).toBe(201);
	return (await response.json()).data.id;
}

/**
 * API で投稿をまとめて作る。
 *
 * **並列にしすぎない。** 画像処理が同時に走る数を絞らないと、
 * 実行環境によっては処理待ちが上限を超える。
 */
export async function createPostsViaApi(
	api: APIRequestContext,
	token: string,
	count: number,
	label: string
): Promise<void> {
	// **並列度を上げすぎない。** 画像処理は1件ずつコンテナで動くため、
	// 同時に投げるほど1件あたりの完了が遅くなる。
	const CONCURRENCY = 2;
	for (let start = 0; start < count; start += CONCURRENCY) {
		const batch = [];
		for (let i = start; i < Math.min(start + CONCURRENCY, count); i++) {
			batch.push(createPostViaApi(api, token, { body: `${label} ${i + 1}` }));
		}
		await Promise.all(batch);
	}
}

/** API でフォローする。フォロー中フィードを他テストの投稿から隔離するために使う。 */
export async function followViaApi(
	api: APIRequestContext,
	token: string,
	handle: string
): Promise<void> {
	const response = await api.put(`/api/users/${handle}/follow`, {
		headers: { Authorization: `Bearer ${token}` }
	});
	expect(response.status(), await response.text()).toBe(204);
}
