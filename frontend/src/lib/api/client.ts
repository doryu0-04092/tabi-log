// バックエンド API を呼ぶ薄いクライアント。
//
// 前回プロジェクトではこの層を TanStack Query が担っていた。今回は
// 追加ライブラリを入れず、必要な機能（エンベロープの解釈とエラーの型付け）だけを
// 自前で持つ。キャッシュが必要になった時点で、必要な範囲だけ足す。

/** API のベース URL。開発時は Vite の proxy 経由、本番は CloudFront が同一オリジンで振り分ける。 */
const BASE_URL = '/api';

/** 成功レスポンスの外枠。バックエンドは常に data で包んで返す。 */
type SuccessEnvelope<T> = { data: T };

/** エラーレスポンスの外枠。 */
type ErrorEnvelope = { error: { code: string; message: string } };

/**
 * API が返したエラーを表す。
 *
 * `code` で分岐すること。`message` は利用者への表示専用であり、
 * 文言の変更で分岐が壊れないようにする。
 */
export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

/** ネットワークに到達できなかった場合を表す。サーバーの応答とは区別する。 */
export class NetworkError extends Error {
	constructor(cause: unknown) {
		super('サーバーに接続できませんでした');
		this.name = 'NetworkError';
		this.cause = cause;
	}
}

export type RequestOptions = {
	method?: string;
	body?: unknown;
	signal?: AbortSignal;
};

/**
 * API を呼び、`data` の中身を返す。
 *
 * エラー時は ApiError か NetworkError を投げる。呼び出し側が
 * 「レスポンスは返ってきたが失敗だった」のか「そもそも届かなかった」のかを
 * 区別できるようにするためである。
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
	const { method = 'GET', body, signal } = options;

	let response: Response;
	try {
		response = await fetch(`${BASE_URL}${path}`, {
			method,
			signal,
			headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body)
		});
	} catch (cause) {
		// AbortError は呼び出し側が意図的に中断したものなのでそのまま伝える。
		if (cause instanceof DOMException && cause.name === 'AbortError') throw cause;
		throw new NetworkError(cause);
	}

	const payload = await parseJson(response);

	if (!response.ok) {
		const err = (payload as ErrorEnvelope | null)?.error;
		throw new ApiError(
			response.status,
			err?.code ?? 'unknown_error',
			err?.message ?? `リクエストが失敗しました (HTTP ${response.status})`
		);
	}

	return (payload as SuccessEnvelope<T>).data;
}

/**
 * レスポンス本文を JSON として読む。
 *
 * 本文が空、あるいは JSON でない場合に例外で落とさない。
 * 502 や 504 のようにインフラ側が HTML を返す場合があり、
 * そこで解析エラーになると「何が起きたか分からない」状態になるためである。
 */
async function parseJson(response: Response): Promise<unknown> {
	const text = await response.text();
	if (text === '') return null;
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}
