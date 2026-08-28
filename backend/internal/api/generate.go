// Package api は OpenAPI 仕様から生成したコードを保持する。
//
// 生成物は internal/api/gen/api.gen.go にあり、直接編集しない。
// 仕様（docs/openapi.yaml）を変更してから再生成する。
// CI は再生成して差分が出ないことを検証する。
package api

//go:generate go tool oapi-codegen -config gen/config.yaml ../../../docs/openapi.yaml
