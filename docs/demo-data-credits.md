# 画像の出所

このプロジェクトで使っている画像の出どころと利用条件をまとめます。

**種類が2つあります。**

| 種類 | 置き場所 | 撤収したら |
|---|---|---|
| **リポジトリに含める画像** | `frontend/src/lib/assets/` | 残る（アプリの一部） |
| 動作確認用の写真 | 公開環境のみ | 残らない |

---

## 1. リポジトリに含める画像

### `frontend/src/lib/assets/japan-map.png` — デフォルメした日本地図

| 項目 | 内容 |
|---|---|
| 出所 | **ChatGPT が生成した画像**（本人が用意し、2026-09-01 に提供） |
| 加工 | 左上の題字「日本地図（47都道府県）」の囲みを削除。256色に減色（1178KB → 521KB） |
| 用途 | 都道府県制覇マップの下地（`ConquestMap.svelte`） |

**この絵に県名が焼き込まれています。** アプリ側は文字を描かず、
県名の位置に透明な当たり判定を重ねてリンクにしています
（座標は `frontend/src/lib/data/prefecture-hits.ts`）。

> **絵を差し替えるときは当たり判定も測り直すこと。** 座標は
> この画像から実測したもので、**絵と一組**です。

**既製の日本地図素材を使っていない理由。** 調べた範囲では、そのまま使える
日本地図 SVG は GFDL（コピーレフト）のものしか見つかりませんでした。
無料イラスト素材サイトのものは**加工・トレースした場合も含めて再配布を
禁止**しており、ソースコードを公開リポジトリに置く以上は使えません
（実際に1件検討し、規約を読んで見送っています）。

### `frontend/src/lib/assets/favicon.svg`

自作。

---

## 2. 動作確認用データの写真

公開環境（デモ）に入れてある投稿の写真は、**Wikimedia Commons から取得した
自由に利用できる画像**です。撮影者と利用条件を以下に記します。

**これらはリポジトリに含めていません。** 動作確認のために
公開環境へ投入したものであり、環境を撤収すれば残りません。

| 撮影者 | ライセンス | 出所 |
|---|---|---|
| Midori | CC BY 3.0 | [Lake Kawaguchiko Sakura Mount Fuji 1.JPG](https://commons.wikimedia.org/wiki/File:Lake_Kawaguchiko_Sakura_Mount_Fuji_1.JPG) |
| Bernard Gagnon | CC BY-SA 3.0 | [Torii and Itsukushima Shrine.jpg](https://commons.wikimedia.org/wiki/File:Torii_and_Itsukushima_Shrine.jpg) |
| Jaycangel | CC BY-SA 3.0 | [Kinkaku-ji the Golden Temple in Kyoto overloo](https://commons.wikimedia.org/wiki/File:Kinkaku-ji_the_Golden_Temple_in_Kyoto_overlooking_the_lake_-_high_rez.JPG) |
| tsuda from Tsushima, Aichi, Japan | CC BY-SA 2.0 | [Shirakawa-go 001.jpg](https://commons.wikimedia.org/wiki/File:Shirakawa-go_001.jpg) |
| Original uploader was Jovandavid at en.wikipedia | CC BY-SA 3.0 | [Fountain Kenrokuen Garden Kanazawa Japan.JPG](https://commons.wikimedia.org/wiki/File:Fountain_Kenrokuen_Garden_Kanazawa_Japan.JPG) |
| Gorgo | Public domain | [Himeji Castle 0804 1.jpg](https://commons.wikimedia.org/wiki/File:Himeji_Castle_0804_1.jpg) |
| 663highland | CC BY 2.5 | [View from Mount Hakodate Japan01o.jpg](https://commons.wikimedia.org/wiki/File:View_from_Mount_Hakodate_Japan01o.jpg) |
| Nagono | CC BY-SA 3.0 | [Daibutsuden of Todaiji Temple - panoramio.jpg](https://commons.wikimedia.org/wiki/File:Daibutsuden_of_Todaiji_Temple_-_panoramio.jpg) |
| 柑橘類 (talk) | CC BY-SA 3.0 | [Matsumoto Castle 1-1.jpg](https://commons.wikimedia.org/wiki/File:Matsumoto_Castle_1-1.jpg) |
| Japanexperterna.se | CC BY-SA 3.0 | [Dogo onsen honkan long exposure.jpg](https://commons.wikimedia.org/wiki/File:Dogo_onsen_honkan_long_exposure.jpg) |
| Bobo12345 at English Wikipedia | CC BY-SA 3.0 | [MountAso Nakadake crater.jpg](https://commons.wikimedia.org/wiki/File:MountAso_Nakadake_crater.jpg) |
| 藤谷良秀 | CC BY-SA 3.0 | [Tsunoshima ohashi.JPG](https://commons.wikimedia.org/wiki/File:Tsunoshima_ohashi.JPG) |
| Toto-artist (talk) | CC BY-SA 3.0 | [Zao juhyo.jpg](https://commons.wikimedia.org/wiki/File:Zao_juhyo.jpg) |
| E-190 | CC BY-SA 3.0 | [KouriOhashi-200703.jpg](https://commons.wikimedia.org/wiki/File:KouriOhashi-200703.jpg) |
| Daderot | Public domain | [Kaminarimon (outer gate), Sensoji Temple, Aka](https://commons.wikimedia.org/wiki/File:Kaminarimon_(outer_gate),_Sensoji_Temple,_Akakusa,_Tokyo.jpg) |
| OKJaguar | CC BY-SA 4.0 | [Oirase Stream, Towada-Hachimantai National Pa](https://commons.wikimedia.org/wiki/File:Oirase_Stream,_Towada-Hachimantai_National_Park,_Japan.jpg) |
| Syohei Arai | CC BY-SA 4.0 | [Kegon falls-2006-03-21 2.jpg](https://commons.wikimedia.org/wiki/File:Kegon_falls-2006-03-21_2.jpg) |
| TANAKA Juuyoh (talk) | CC BY 3.0 | [Sakurajima55.jpg](https://commons.wikimedia.org/wiki/File:Sakurajima55.jpg) |
| Qurren | CC BY-SA 3.0 | [Kurobe Dam survey.jpg](https://commons.wikimedia.org/wiki/File:Kurobe_Dam_survey.jpg) |
| Pete Stewart from Perth, Australia | CC BY-SA 2.0 | [Flickr - Shinrya - Mt Fuji from Chureito Pago](https://commons.wikimedia.org/wiki/File:Flickr_-_Shinrya_-_Mt_Fuji_from_Chureito_Pagoda.jpg) |
| Daderot at English Wikipedia | CC BY-SA 3.0 | [Kenrokuen linterna fall.JPG](https://commons.wikimedia.org/wiki/File:Kenrokuen_linterna_fall.JPG) |
| Bernard Gagnon | CC BY-SA 3.0 | [Gassho-zukuri farmhouse-03.jpg](https://commons.wikimedia.org/wiki/File:Gassho-zukuri_farmhouse-03.jpg) |
| 663highland | CC BY 2.5 | [Motomachi Catholic Church in Hakodate Hokkaid](https://commons.wikimedia.org/wiki/File:Motomachi_Catholic_Church_in_Hakodate_Hokkaido_Japan01n.jpg) |
| 663highland | CC BY 2.5 | [Hakodate Asaichi Hokkaido Japan05bs5.jpg](https://commons.wikimedia.org/wiki/File:Hakodate_Asaichi_Hokkaido_Japan05bs5.jpg) |
| decade_null | CC BY 2.0 | [Trip to Miyajima; October 2008 (05).jpg](https://commons.wikimedia.org/wiki/File:Trip_to_Miyajima;_October_2008_(05).jpg) |
| Kusakabe Kimbei | Public domain | [Kusakabe Kimbei 1248 Kasuga Nara.JPG](https://commons.wikimedia.org/wiki/File:Kusakabe_Kimbei_1248_Kasuga_Nara.JPG) |
| Corpse Reviver | CC BY-SA 3.0 | [East side of white heron castle.jpg](https://commons.wikimedia.org/wiki/File:East_side_of_white_heron_castle.jpg) |
| 継之助 | CC BY-SA 3.0 | [Kanazawa Castle Gate.JPG](https://commons.wikimedia.org/wiki/File:Kanazawa_Castle_Gate.JPG) |
| Moyan Brenn from Italy | CC BY 2.0 | [Japan (16230424045).jpg](https://commons.wikimedia.org/wiki/File:Japan_(16230424045).jpg) |
| Jordan Emery | CC BY 2.0 | [A cherry blossom bloom near five-stories pago](https://commons.wikimedia.org/wiki/File:A_cherry_blossom_bloom_near_five-stories_pagoda_On_Miyajima_Island_Japan.jpg) |
| Bernard Gagnon | CC BY-SA 3.0 | [Ogimachi Village-01.jpg](https://commons.wikimedia.org/wiki/File:Ogimachi_Village-01.jpg) |
| Martin Falbisoner | CC BY-SA 4.0 | [Kiyomizu-dera, Kyoto, November 2016 -01.jpg](https://commons.wikimedia.org/wiki/File:Kiyomizu-dera,_Kyoto,_November_2016_-01.jpg) |
| Paul Vlaar | CC BY-SA 3.0 | [KyotoFushimiInariLarge.jpg](https://commons.wikimedia.org/wiki/File:KyotoFushimiInariLarge.jpg) |
| Hiroaki Kaneko | CC BY-SA 3.0 | [嵐山の竹林の小径 (The bamboo grove in Arashiyama) 10 ](https://commons.wikimedia.org/wiki/File:%E5%B5%90%E5%B1%B1%E3%81%AE%E7%AB%B9%E6%9E%97%E3%81%AE%E5%B0%8F%E5%BE%84_(The_bamboo_grove_in_Arashiyama)_10_Jul,_2010_-_panoramio.jpg) |
| 663highland | CC BY 2.5 | [Gero-onsen01s3200.jpg](https://commons.wikimedia.org/wiki/File:Gero-onsen01s3200.jpg) |
| Zairon | CC BY 4.0 | [Nagato Motonosumi-Inari-jinja Row of Torii fr](https://commons.wikimedia.org/wiki/File:Nagato_Motonosumi-Inari-jinja_Row_of_Torii_from_above_10.jpg) |

## なぜ手元の写真を使わなかったか

手元にあった写真の多くは **Windows のロック画面に配信される壁紙**でした。
これは提供元に権利のある素材で、公開する場所に置くのは適切ではありません。
また撮影地が日本国外のものが含まれており、**都道府県を必須にしている
このアプリの前提に合いません**（実在しない紐づけを作ることになります）。

## 取得と選別

Wikimedia Commons の API で検索し、**ライセンスと撮影者が取得できたものだけ**を
採用しました（CC BY / CC BY-SA / CC0 / パブリックドメイン）。

**モンタージュ（複数写真を1枚に合成した画像）を除いています。**
Commons には都市の紹介用に合成された画像があり、検索の上位に出ます。
名前だけ見て採ったところ「1枚のはずが何枚も写っている」状態になりました。
ファイル名（montage / collage / composite）と縦横比の2つで弾いています。

**1つの投稿には同じ都道府県の写真だけを使っています。**
県をまたぐ写真を1つの投稿にまとめると、都道府県との紐づけが実態と合いません。
