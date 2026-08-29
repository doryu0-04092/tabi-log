import { THRESHOLDS } from '../lib/config.ts';
import { browse, notifications, search, setupTokens, type Session } from '../lib/flows.ts';

/*
動作確認だけを行う短い実行。

**本番の測定ではない。** シナリオが最後まで通るか、
種データが入っているかを1分未満で確かめるためのものである。

**「時間がかかるので短い方だけ流す」を常態にしない。**
短い実行で通ることは、負荷に耐えることの根拠にならない。
load / stress / spike は必ず最後まで回す（README.md）。
*/

export const options = {
	vus: 1,
	iterations: 5,
	thresholds: THRESHOLDS.normal
};

export function setup() {
	return { sessions: setupTokens() };
}

export default function (data: { sessions: Session[] }) {
	const session = data.sessions[0];
	browse(session, data.sessions.map((s) => s.handle));
	search(session);
	notifications(session);
}
