import { sleep } from 'k6';
import { THRESHOLDS } from '../lib/config.ts';
import {
	browse,
	notifications,
	pickPostId,
	search,
	setupTokens,
	toggleLike,
	type Session
} from '../lib/flows.ts';

/*
通常の負荷。**要件の想定ピーク（50 req/s）を満たせるかを見る。**

段階を踏んで上げているのは、**いきなり最大にすると
「立ち上がりが遅いだけ」なのか「その負荷に耐えられない」のかが
区別できない**ためである。

一巡りにつき 8〜10 本のリクエストが出る。50 VU で
1秒に1巡すれば、おおよそ 50 req/s の想定に乗る。
*/

export const options = {
	stages: [
		{ duration: '30s', target: 10 }, // 立ち上げ
		{ duration: '1m', target: 50 }, // 想定ピークまで上げる
		{ duration: '2m', target: 50 }, // 維持して測る
		{ duration: '30s', target: 0 } // 下げる
	],
	thresholds: THRESHOLDS.normal
};

export function setup() {
	return { sessions: setupTokens() };
}

export default function (data: { sessions: Session[] }) {
	const session = data.sessions[__VU % data.sessions.length];
	const handles = data.sessions.map((s) => s.handle);

	browse(session, handles);

	// **毎回すべてを叩かない。** 実際の使われ方に近い割合にする。
	// 検索と通知は、一覧を見る回数よりは少ない。
	if (__ITER % 3 === 0) search(session);
	if (__ITER % 4 === 0) notifications(session);
	if (__ITER % 5 === 0) {
		const postId = pickPostId(session);
		if (postId > 0) toggleLike(session, postId);
	}

	// 人は次の操作まで少し間を置く。間を置かないと、
	// 実際にはあり得ない密度の負荷を測ることになる。
	sleep(1);
}
