//go:build linux

package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func checkMigrationWriters(dir string) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return fmt.Errorf("无法检查本机写进程")
	}
	inside := func(path string) bool {
		rel, e := filepath.Rel(dir, path)
		return e == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."))
	}
	for _, p := range entries {
		pid, e := strconv.Atoi(p.Name())
		if e != nil || pid == os.Getpid() {
			continue
		}
		root := filepath.Join("/proc", p.Name())
		raw, e := os.ReadFile(filepath.Join(root, "cmdline"))
		if e != nil {
			continue
		}
		args := strings.Split(string(raw), "\x00")
		for i, arg := range args {
			value := ""
			if (arg == "-data" || arg == "--data") && i+1 < len(args) {
				value = args[i+1]
			} else if strings.HasPrefix(arg, "-data=") || strings.HasPrefix(arg, "--data=") {
				value = strings.SplitN(arg, "=", 2)[1]
			}
			if value != "" {
				if !filepath.IsAbs(value) {
					cwd, _ := os.Readlink(filepath.Join(root, "cwd"))
					value = filepath.Join(cwd, value)
				}
				if inside(value) {
					return fmt.Errorf("数据目录仍有进程使用，请先停止服务")
				}
			}
		}
		fds, _ := os.ReadDir(filepath.Join(root, "fd"))
		for _, fd := range fds {
			target, e := os.Readlink(filepath.Join(root, "fd", fd.Name()))
			if e == nil && inside(target) {
				return fmt.Errorf("数据目录仍有进程打开文件，请先停止服务")
			}
		}
	}
	return nil
}
