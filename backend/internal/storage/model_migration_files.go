package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const migrationPending = ".model-migration.pending.json"

func checkManagedModelMigration(path string) error {
	root := filepath.Dir(filepath.Dir(path))
	raw, err := os.ReadFile(filepath.Join(root, "schema-migration.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("受管迁移记录不可读，拒绝启动")
	}
	var record struct {
		Phase   string `json:"phase"`
		Release string `json:"new_release"`
	}
	if json.Unmarshal(raw, &record) != nil || record.Phase != "starting" {
		return fmt.Errorf("受管数据迁移尚未完成，拒绝启动")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	expected, err := filepath.EvalSymlinks(filepath.Join(record.Release, "codearts2api-panel"))
	if err != nil {
		return fmt.Errorf("迁移版本路径无效")
	}
	actual, err := filepath.EvalSymlinks(exe)
	if err != nil || actual != expected {
		return fmt.Errorf("当前程序不是迁移指定版本，拒绝启动")
	}
	return nil
}

func noPathLinks(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("迁移路径不能包含符号链接")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
func directoryHashes(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("迁移目录包含符号链接")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("迁移目录包含非普通文件")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, err
}
func copyMigrationFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// Only the isolated output is opened for journal replay and atomic conversion.
func MigrateStateDirectory(input, output string) (ModelMigrationReport, error) {
	var report ModelMigrationReport
	src, err := filepath.Abs(input)
	if err != nil {
		return report, err
	}
	dst, err := filepath.Abs(output)
	if err != nil {
		return report, err
	}
	if err = noPathLinks(src); err != nil {
		return report, err
	}
	if err = noPathLinks(filepath.Dir(dst)); err != nil {
		return report, err
	}
	for _, pair := range [][2]string{{src, dst}, {dst, src}} {
		rel, _ := filepath.Rel(pair[0], pair[1])
		if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return report, fmt.Errorf("迁移输入输出路径不能重叠")
		}
	}
	if _, err = os.Lstat(dst); !os.IsNotExist(err) {
		return report, fmt.Errorf("迁移输出目录必须不存在")
	}
	if err = checkMigrationWriters(src); err != nil {
		return report, err
	}
	before, err := directoryHashes(src)
	if err != nil {
		return report, err
	}
	if _, ok := before[migrationPending]; ok {
		return report, fmt.Errorf("输入目录有未完成的迁移")
	}
	report, err = CheckStateMigration(filepath.Join(src, "state.json"))
	if err != nil {
		return report, err
	}
	if !report.Ready {
		return report, fmt.Errorf("迁移预检查存在冲突")
	}
	if err = os.Mkdir(dst, 0700); err != nil {
		return report, err
	}
	marker := filepath.Join(dst, migrationPending)
	if err = writeInstallationFile(marker, []byte(`{"phase":"copying"}`), 0600); err != nil {
		return report, err
	}
	for name := range before {
		if err = copyMigrationFile(filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
			return report, err
		}
	}
	after, err := directoryHashes(src)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(before, after) {
		return report, fmt.Errorf("迁移期间输入发生变化")
	}
	if err = checkMigrationWriters(src); err != nil {
		return report, err
	}
	st, err := openState(filepath.Join(dst, "state.json"), true)
	if err != nil {
		return report, err
	}
	if err = st.Save(); err != nil {
		return report, err
	}
	if err = validateUnifiedState(st.st); err != nil {
		return report, err
	}
	data, _ := json.Marshal(report)
	if err = os.WriteFile(filepath.Join(dst, "model-migration-report.json"), data, 0600); err != nil {
		return report, err
	}
	if err = filepath.WalkDir(dst, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return syncUsageDirectory(path)
		}
		f, e := os.OpenFile(path, os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		e = f.Sync()
		ce := f.Close()
		if e == nil {
			e = ce
		}
		return e
	}); err != nil {
		return report, err
	}
	if err = os.Remove(marker); err != nil {
		return report, err
	}
	if err = syncUsageDirectory(dst); err != nil {
		return report, err
	}
	return report, nil
}
