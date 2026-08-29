import { sleep } from 'k6';
import { THRESHOLDS } from '../lib/config.ts';
import { browse, setupTokens, type Session } from '../lib/flows.ts';

/*
急に増えたときの挙動。

**一時的に遅くなるのは許すが、落ちてはいけない。**
SNS では「誰かの投稿が広まって一気に人が来る」形の増え方が起きる。
少しずつ増える負荷では、この形は再現できない。

閾値を p95 < 3000ms と緩めているのは、
**この試験で見たいのが速さではなく「戻ってこられるか」**だからである。
山を越えたあとに元の応答時間へ戻ること（＝詰まったまま引きずらないこと）を
レポートの時系列で確認する。
*/

export const options = {
	stages: [
		{ duration: '30s', target: 10 }, // ふだんの状態
		{ duration: '10s', target: 200 }, // 一気に増える
		{ duration: '1m', target: 200 }, // 山が続く
		{ duration: '10s', target: 10 }, // 引く
		{ duration: '1m', target: 10 } // 戻ったかを見る
	],
	thresholds: THRESHOLDS.spike
};

export function setup() {
	return { sessions: setupTokens() };
}

export default function (data: { sessions: Session[] }) {
	const session = data.sessions[__VU % data.sessions.length];
	browse(session, data.sessions.map((s) => s.handle));
	sleep(1);
}
