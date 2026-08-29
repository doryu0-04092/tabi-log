package httpapi

// 仕様書の写しを置く。
//
// **docs/openapi.yaml が唯一の正である。** ここに置くのは
// 実行時に配るための写しであり、go:embed が package の外を
// 参照できないために必要になる。
//
// CI の「生成物の一致検証」が go generate を回して差分を見るため、
// 写しが古いまま残ることはない。
//
//go:generate go run ../../cmd/copyspec -from ../../../docs/openapi.yaml -to openapi.yaml
