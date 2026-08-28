package store

// sqlc が db/queries/*.sql と db/migrations/*.sql から生成する。
// 生成物は internal/store/dbgen/ にあり、直接編集しない。
// CI は再生成して差分が出ないことを検証する。
//
//go:generate go tool sqlc generate -f ../../sqlc.yaml
