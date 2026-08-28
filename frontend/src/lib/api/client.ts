// バックエンド API を呼ぶ薄いクライアント。
//
// 前回プロジェクトではこの層を TanStack Query が担っていた。今回は
// 追加ライブラリを入れず、必要な機能（エンベロープの解釈、エラーの型付け、
// アクセストークンの付与と再取得）だけを自前で持つ。

/** API のベース URL。開発時は Vite の proxy 経由、本番は CloudFront が同一オリジンで振り分ける。 */
const BASE_URL = '/api';

/**
 * CSRF 対策のヘッダー。Cookie を使うエンドポイントがこの値を要求する。
 *
 * カスタムヘッダーは単純リクエストの条件を外れるため、クロスオリジンから
 * 送るには CORS のプリフライトが通る必要がある。フォーム送信のような
 * プリフライトを伴わない経路では付けられない。
 */
const CSRF_HEADER = 'X-Requested-With';
const CSRF_VALUE = 'tabi-log';

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

// ---------------------------------------------------------------------------
// アクセストークンの保持
//
// **モジュール内の変数にのみ持ち、localStorage には保存しない。**
// localStorage は同一オリジンの JavaScript から読めるため、XSS が1つでも
// あればトークンを持ち出される。メモリなら、リロードで消える代わりに
// 持ち出しの経路が減る。リロード後は Cookie のリフレッシュトークンから
// 取り直す（restoreSession）。
//
// リアクティブな $state ではなく素の変数にしているのは、これが画面に
// 描画される値ではないためである。画面に出すのは利用者情報の方で、
// そちらは session.svelte.ts が持つ。
// ---------------------------------------------------------------------------

let accessToken: string | null = null;

export function setAccessToken(token: string | null): void {
	accessToken = token;
}

export function getAccessToken(): string | null {
	return accessToken;
}

// ---------------------------------------------------------------------------
// トークンの再取得（単一化）
// ---------------------------------------------------------------------------

/**
 * 進行中のリフレッシュ。
 *
 * **同時に複数のリフレッシュを走らせない**ことが要点である。
 * 画面表示時に複数の API 呼び出しが同時に 401 を受けると、それぞれが
 * リフレッシュを試みる。サーバー側はローテーション方式なので、
 * 後発は「失効済みトークンの提示」になる。サーバーにも猶予時間の
 * 仕組みはあるが、クライアント側でも1本にまとめて二重に防ぐ。
 */
let refreshInFlight: Promise<boolean> | null = null;

/** リフレッシュ成功時に呼ばれる。session.svelte.ts が利用者情報を更新する。 */
type RefreshListener = (user: unknown | null) => void;
let onRefreshed: RefreshListener = () => {};

export function setRefreshListener(fn: RefreshListener): void {
	onRefreshed = fn;
}

/**
 * リフレッシュトークン（Cookie）から新しいアクセストークンを取得する。
 *
 * 同時に呼ばれた場合は、進行中の1本を共有する。
 */
export function refreshAccessToken(): Promise<boolean> {
	refreshInFlight ??= performRefresh().finally(() => {
		refreshInFlight = null;
	});
	return refreshInFlight;
}

async function performRefresh(): Promise<boolean> {
	try {
		const data = await rawRequest<{ accessToken: string; user: unknown }>('/auth/refresh', {
			method: 'POST',
			csrf: true
		});
		setAccessToken(data.accessToken);
		onRefreshed(data.user);
		return true;
	} catch {
		// 失敗の理由（未ログイン・期限切れ・盗用検知）に関わらず、
		// クライアント側でできることは同じ＝ログアウト状態に戻すこと。
		setAccessToken(null);
		onRefreshed(null);
		return false;
	}
}

// ---------------------------------------------------------------------------
// リクエスト
// ---------------------------------------------------------------------------

export type RequestOptions = {
	method?: string;
	body?: unknown;
	signal?: AbortSignal;
	/** CSRF 対策のヘッダーを付ける。Cookie を使うエンドポイントで必要。 */
	csrf?: boolean;
	/** 401 のときにリフレッシュして再試行するかどうか。 */
	retryOnUnauthorized?: boolean;
};

/**
 * API を呼び、`data` の中身を返す。
 *
 * 401 を受けた場合は一度だけリフレッシュして再試行する。
 * エラー時は ApiError か NetworkError を投げる。呼び出し側が
 * 「レスポンスは返ってきたが失敗だった」のか「そもそも届かなかった」のかを
 * 区別できるようにするためである。
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
	const { retryOnUnauthorized = true, ...rest } = options;

	try {
		return await rawRequest<T>(path, rest);
	} catch (e) {
		const shouldRetry =
			retryOnUnauthorized &&
			e instanceof ApiError &&
			e.status === 401 &&
			// リフレッシュ自体の 401 で再帰しない。
			!path.startsWith('/auth/refresh');

		if (!shouldRetry) throw e;

		if (!(await refreshAccessToken())) throw e;
		// 再試行は1回だけ。無限に繰り返さない。
		return await rawRequest<T>(path, rest);
	}
}

async function rawRequest<T>(path: string, options: Omit<RequestOptions, 'retryOnUnauthorized'>): Promise<T> {
	const { method = 'GET', body, signal, csrf = false } = options;

	const headers: Record<string, string> = {};
	if (body !== undefined) headers['Content-Type'] = 'application/json';
	if (csrf) headers[CSRF_HEADER] = CSRF_VALUE;
	if (accessToken) headers['Authorization'] = `Bearer ${accessToken}`;

	let response: Response;
	try {
		response = await fetch(`${BASE_URL}${path}`, {
			method,
			signal,
			headers: Object.keys(headers).length > 0 ? headers : undefined,
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

	// 204 No Content には本文が無く、エンベロープも存在しない。
	// ここで .data を読むと null 参照で落ちる。
	// **ログアウトがこの経路であり、例外になると画面遷移まで巻き添えになる。**
	if (payload === null) return undefined as T;

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
