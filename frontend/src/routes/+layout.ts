// SPA として動かすための設定。
//
// ssr = false: サーバー側レンダリングを行わない。adapter-static で
//   静的ファイルとして書き出し、CloudFront + S3 から配信する。
// prerender = false: 内容が利用者ごとに異なるため事前描画しない。
//
// この2つは docs/tech-stack.md に記した「OGP を捨てる代わりに
// インフラを単純に保つ」という判断の実体である。
export const ssr = false;
export const prerender = false;
