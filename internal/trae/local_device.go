package trae

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type LocalDevice struct {
	Client    string `json:"client"`
	Source    string `json:"source"`
	MachineID string `json:"machine_id"`
	DeviceID  string `json:"device_id"`
}

var registeredDeviceKey = regexp.MustCompile(`^iCubeAuthInfo://icube-dc:([0-9]{8,32})$`)
var machineIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)

// ReadLocalDevices reads client records on the gateway host. It never exports
// account values or substitutes OS identifiers or saved gateway credentials.
func ReadLocalDevices() ([]LocalDevice, []string) {
	var roots []string
	if dir, err := os.UserConfigDir(); err == nil {
		roots = append(roots, dir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".config"), filepath.Join(home, "Library", "Application Support"))
	}
	for _, key := range []string{"APPDATA", "LOCALAPPDATA"} {
		if dir := os.Getenv(key); dir != "" {
			roots = append(roots, dir)
		}
	}
	return readLocalDevices(roots)
}

func readLocalDevices(roots []string) ([]LocalDevice, []string) {
	devices, warnings := []LocalDevice{}, []string{}
	seenPaths, seenDevices := map[string]bool{}, map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, "无法读取客户端配置目录："+root)
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.Contains(strings.ToLower(entry.Name()), "trae") {
				continue
			}
			path := filepath.Join(root, entry.Name(), "User", "globalStorage", "storage.json")
			if seenPaths[path] {
				continue
			}
			seenPaths[path] = true
			f, err := os.Open(path)
			if err != nil {
				if !os.IsNotExist(err) {
					warnings = append(warnings, entry.Name()+"：无法读取设备记录")
				}
				continue
			}
			const limit = 16 << 20
			raw, err := io.ReadAll(io.LimitReader(f, limit+1))
			f.Close()
			var storage map[string]json.RawMessage
			raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
			if err != nil || len(raw) > limit || json.Unmarshal(raw, &storage) != nil || storage == nil {
				warnings = append(warnings, entry.Name()+"：设备记录格式无效或文件过大")
				continue
			}
			var machine string
			if json.Unmarshal(storage["telemetry.machineId"], &machine) != nil || !machineIdentifier.MatchString(machine) {
				warnings = append(warnings, entry.Name()+"：没有完整的机器标识")
				continue
			}
			var ids []string
			for key := range storage {
				if match := registeredDeviceKey.FindStringSubmatch(key); match != nil {
					ids = append(ids, match[1])
				}
			}
			sort.Strings(ids)
			if len(ids) == 0 {
				warnings = append(warnings, entry.Name()+"：没有官方客户端的数字设备号")
			}
			for _, id := range ids {
				key := machine + ":" + id
				if !seenDevices[key] {
					seenDevices[key] = true
					devices = append(devices, LocalDevice{Client: entry.Name(), Source: path, MachineID: machine, DeviceID: id})
				}
			}
		}
	}
	return devices, warnings
}
