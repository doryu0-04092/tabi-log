import { sleep } from 'k6';
import { THRESHOLDS } from '../lib/config.ts';
import { browse, search, setupTokens, type Session } from '../lib/flows.ts';

/*
限界を探す。

**応答時間では落とさない。** 上げ切るのが目的なので、遅くなること
自体は想定内である。見るのは「壊れずに遅くなるか」— つまり
**エラーを返し始める点がどこか**である。

読むときの観点:

  - どの段階から p95 が跳ねるか
  - エラーが出始めるのはどの段階か
  - そのとき詰まっているのは接続プールか、CPU か、索引か
    （backend のログと MySQL の状態を併せて見ないと分からない）
*/

export const options = {
	stages: [
		{ duration: '1m', target: 50 },
		{ duration: '1m', target: 100 },
		{ duration: '1m', target: 200 },
		{ duration: '1m', target: 300 },
		{ duration: '30s', target: 0 }
	],
	thresholds: THRESHOLDS.stress
};

export function setup() {
	return { sessions: setupTokens() };
}

export default function (data: { sessions: Session[] }) {
	const session = data.sessions[__VU % data.sessions.length];
	browse(session, data.sessions.map((s) => s.handle));
	if (__ITER % 3 === 0) search(session);
	sleep(0.5);
}
