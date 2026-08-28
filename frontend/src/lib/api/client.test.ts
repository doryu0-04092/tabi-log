import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	ApiError,
	NetworkError,
	getAccessToken,
	request,
	setAccessToken
} from './client';

function mockFetch(response: Response | Error) {
	// 引数を明示的に受けておく。省略すると呼び出し記録の型が空タプルになり、
	// mock.calls から init を取り出せない。
	const fn = vi.fn((_input: string, _init?: RequestInit) =>
		response instanceof Error ? Promise.reject(response) : Promise.resolve(response)
	);
	vi.stubGlobal('fetch', fn);
	return fn;
}

function jsonResponse(status: number, body: unknown) {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('request', () => {
	it('成功時は data の中身を返す（エンベロープを剥がす）', async () => {
		mockFetch(jsonResponse(200, { data: { status: 'ok' } }));

		await expect(request('/livez')).resolves.toEqual({ status: 'ok' });
	});

	it('/api を前置してリクエストする', async () => {
		const fetchMock = mockFetch(jsonResponse(200, { data: null }));

		await request('/livez');

		expect(fetchMock).toHaveBeenCalledWith('/api/livez', expect.anything());
	});

	it('エラー時は code と message を持つ ApiError を投げる', async () => {
		mockFetch(
			jsonResponse(503, {
				error: { code: 'dependency_unavailable', message: 'データベースへ接続できません' }
			})
		);

		await expect(request('/readyz')).rejects.toMatchObject({
			name: 'ApiError',
			status: 503,
			code: 'dependency_unavailable'
		});
	});

	// 502 や 504 ではインフラ側が HTML を返すことがある。
	// そこで JSON の解析エラーになると「何が起きたか分からない」状態になるため、
	// ステータスに基づくエラーとして扱えること。
	it('本文が JSON でなくても ApiError になる', async () => {
		mockFetch(new Response('<html>Bad Gateway</html>', { status: 502 }));

		const err: unknown = await request('/livez').catch((e: unknown) => e);

		expect(err).toBeInstanceOf(ApiError);
		expect((err as ApiError).status).toBe(502);
		expect((err as ApiError).code).toBe('unknown_error');
	});

	// 「応答は返ったが失敗」と「そもそも届かなかった」を呼び出し側が区別できること。
	it('通信自体に失敗した場合は NetworkError を投げる', async () => {
		mockFetch(new TypeError('Failed to fetch'));

		await expect(request('/livez')).rejects.toBeInstanceOf(NetworkError);
	});
});

// ---------------------------------------------------------------------------
// アクセストークンの付与と再取得
// ---------------------------------------------------------------------------

describe('アクセストークン', () => {
	afterEach(() => {
		setAccessToken(null);
	});

	it('トークンがあれば Authorization ヘッダを付ける', async () => {
		const fetchMock = mockFetch(jsonResponse(200, { data: null }));
		setAccessToken('my-token');

		await request('/auth/me');

		expect(fetchMock).toHaveBeenCalledWith(
			'/api/auth/me',
			expect.objectContaining({
				headers: expect.objectContaining({ Authorization: 'Bearer my-token' })
			})
		);
	});

	it('トークンが無ければ Authorization ヘッダを付けない', async () => {
		const fetchMock = mockFetch(jsonResponse(200, { data: null }));

		await request('/prefectures');

		const init = fetchMock.mock.calls[0]?.[1] as RequestInit | undefined;
		expect(init?.headers ?? {}).not.toHaveProperty('Authorization');
	});

	it('csrf を指定したときだけ X-Requested-With を付ける', async () => {
		const fetchMock = mockFetch(jsonResponse(200, { data: null }));

		await request('/auth/logout', { method: 'POST', csrf: true });

		expect(fetchMock).toHaveBeenCalledWith(
			'/api/auth/logout',
			expect.objectContaining({
				headers: expect.objectContaining({ 'X-Requested-With': 'tabi-log' })
			})
		);
	});

	it('401 を受けたらリフレッシュして1度だけ再試行する', async () => {
		let call = 0;
		const fetchMock = vi.fn(async (input: string) => {
			call++;
			if (input === '/api/auth/refresh') {
				return jsonResponse(200, { data: { accessToken: 'fresh-token', user: { id: 1 } } });
			}
			// 1回目は 401、リフレッシュ後の2回目は成功。
			return call === 1
				? jsonResponse(401, { error: { code: 'token_expired', message: '期限切れ' } })
				: jsonResponse(200, { data: { ok: true } });
		});
		vi.stubGlobal('fetch', fetchMock);

		await expect(request('/auth/me')).resolves.toEqual({ ok: true });

		// 元のリクエスト → リフレッシュ → 再試行 の3回。
		expect(fetchMock).toHaveBeenCalledTimes(3);
		expect(getAccessToken()).toBe('fresh-token');
	});

	// **同時に複数のリフレッシュを走らせないこと。**
	// サーバーはローテーション方式なので、2本目以降は「失効済みトークンの
	// 提示」になる。サーバー側にも猶予時間があるが、クライアントでも束ねる。
	it('同時に発生した401を1本のリフレッシュにまとめる', async () => {
		let refreshCalls = 0;
		const fetchMock = vi.fn(async (input: string) => {
			if (input === '/api/auth/refresh') {
				refreshCalls++;
				// 応答を1ティック遅らせ、同時実行の状況を作る。
				await new Promise((r) => setTimeout(r, 10));
				return jsonResponse(200, { data: { accessToken: 'fresh-token', user: { id: 1 } } });
			}
			return getAccessToken() === 'fresh-token'
				? jsonResponse(200, { data: { ok: true } })
				: jsonResponse(401, { error: { code: 'token_expired', message: '期限切れ' } });
		});
		vi.stubGlobal('fetch', fetchMock);

		await Promise.all([request('/a'), request('/b'), request('/c')]);

		expect(refreshCalls).toBe(1);
	});

	it('リフレッシュに失敗したら元のエラーを投げ、トークンを捨てる', async () => {
		setAccessToken('stale-token');
		const fetchMock = vi.fn(async (input: string) => {
			if (input === '/api/auth/refresh') {
				return jsonResponse(401, { error: { code: 'unauthenticated', message: '要ログイン' } });
			}
			return jsonResponse(401, { error: { code: 'token_expired', message: '期限切れ' } });
		});
		vi.stubGlobal('fetch', fetchMock);

		await expect(request('/auth/me')).rejects.toMatchObject({ name: 'ApiError', status: 401 });
		expect(getAccessToken()).toBeNull();
	});

	// リフレッシュ自体の 401 で再帰すると無限に呼び続ける。
	it('リフレッシュ自身の401では再試行しない', async () => {
		const fetchMock = mockFetch(
			jsonResponse(401, { error: { code: 'unauthenticated', message: '要ログイン' } })
		);

		await expect(request('/auth/refresh', { method: 'POST', csrf: true })).rejects.toBeInstanceOf(
			ApiError
		);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});
});

// 204 No Content には本文もエンベロープも無い。
// ここで .data を読もうとすると null 参照で落ち、
// **呼び出し元の後続処理（ログアウト後の画面遷移など）まで巻き添えになる。**
describe('本文の無い応答', () => {
	it('204 を例外にせず解決する', async () => {
		mockFetch(new Response(null, { status: 204 }));

		await expect(request('/auth/logout', { method: 'POST', csrf: true })).resolves.toBeUndefined();
	});
});
