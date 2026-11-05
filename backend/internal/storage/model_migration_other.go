//go:build !linux

package storage

import "fmt"

func checkMigrationWriters(string) error {
	return fmt.Errorf("离线目录迁移目前需要 Linux 进程检查；其他平台可在已停止服务后使用副本启动迁移")
}
