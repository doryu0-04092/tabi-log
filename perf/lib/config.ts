/*
 * 負荷試験の共通設定。
 *
 * **閾値は「理想値」ではなく「守れなければ問題だと判断する線」を置く。**
 * 厳しすぎる数字は、達成できないたびに無視されるようになり、
 * 最後は誰も見なくなる。
 *
 * 数字の出どころは docs/requirements.md 4.5:
 *
 *   - 想定ピーク 50 req/s
 *   - 読み取りの応答 p95 < 300ms
 *
 * **平均ではなく p95 で見る。** 平均は少数の極端に遅い応答に引きずられず、
 * 「ほとんどの人にとっては速い」ように見えてしまう。
 * p99 まで求めると、外れ値1件で落ちる不安定な基準になる。
 */

/** 対象のベース URL。既定はローカルの docker compose。 */
export const BASE_URL = __ENV.PERF_BASE_URL || 'http://localhost:8080';

/**
 * 試験用データにつける印。
 *
 * **後片付けはこの印で行う。** 印が無いと、試験で作ったものと
 * もともとあったものを見分けられず、消せなくなる。
 */
export const PREFIX = 'perf_';

/** 応答時間の閾値。シナリオごとに使い分ける。 */
export const THRESHOLDS = {
	/** 通常の負荷で満たすべき線。要件の p95 < 300ms がこれ。 */
	normal: {
		http_req_duration: ['p(95)<300'],
		http_req_failed: ['rate<0.01'],
		checks: ['rate>0.99']
	},
	/**
	 * 限界を探すときの線。
	 *
	 * **応答時間では落とさない。** 限界まで上げるのが目的なので、
	 * 遅くなること自体は想定内である。見るのは
	 * **「壊れずに遅くなるか」**（エラーにならないか）である。
	 */
	stress: {
		http_req_failed: ['rate<0.05'],
		checks: ['rate>0.95']
	},
	/**
	 * 急に増えたときの線。
	 *
	 * 一時的に遅くなるのは許すが、**落ちてはいけない。**
	 */
	spike: {
		http_req_failed: ['rate<0.05'],
		http_req_duration: ['p(95)<3000']
	}
} as const;

/** 認証情報。run.mjs が用意して環境変数で渡す。 */
export type Credential = { email: string; password: string; handle: string };

export function credentials(): Credential[] {
	const raw = __ENV.PERF_USERS;
	if (!raw) {
		throw new Error(
			'PERF_USERS が渡っていない。perf/run.mjs から実行すること（README.md 参照）'
		);
	}
	return JSON.parse(raw) as Credential[];
}
