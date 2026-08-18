// Package migrations 嵌入 SQL 迁移文件，供启动时执行（golang-migrate iofs 源）。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
