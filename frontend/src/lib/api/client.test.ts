import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, NetworkError, request } from './client';

function mockFetch(response: Response | Error) {
	const fn = vi.fn(() => (response instanceof Error ? Promise.reject(response) : Promise.resolve(response)));
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
