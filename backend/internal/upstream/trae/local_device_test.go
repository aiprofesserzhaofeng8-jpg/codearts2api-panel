package trae

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDeviceStorage(t *testing.T, root, client, content string) string {
	t.Helper()
	path := filepath.Join(root, client, "User", "globalStorage", "storage.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalDeviceReadsClientRecordWithoutExportingCredentials(t *testing.T) {
	root := t.TempDir()
	path := writeDeviceStorage(t, root, "TRAE SOLO CN", `{"telemetry.machineId":"machine-fixture","telemetry.devDeviceId":"uuid-not-a-registered-device","iCubeAuthInfo://icube-dc:1234567890123456":{"token":"private-token"},"refresh_token":"private-refresh"}`)
	writeDeviceStorage(t, root, "UnrelatedApp", `{"telemetry.machineId":"other","iCubeAuthInfo://icube-dc:9999999999999999":{}}`)
	devices, warnings := readLocalDevices([]string{root, root})
	if len(devices) != 1 || len(warnings) != 0 || devices[0].MachineID != "machine-fixture" || devices[0].DeviceID != "1234567890123456" || devices[0].Source != path {
		t.Fatalf("wrong local device detection: %+v, %v", devices, warnings)
	}
	raw, _ := json.Marshal(devices)
	if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "uuid-not") {
		t.Fatal("local account data exported")
	}
}

func TestLocalDeviceDoesNotInventMissingIdentifiers(t *testing.T) {
	root := t.TempDir()
	writeDeviceStorage(t, root, "Trae CN", `{"telemetry.machineId":"machine-fixture","telemetry.devDeviceId":"1234567890123456"}`)
	writeDeviceStorage(t, root, "Trae MissingMachine", `{"iCubeAuthInfo://icube-dc:1234567890123456":{}}`)
	writeDeviceStorage(t, root, "Trae Broken", `{"secret-token":`)
	devices, warnings := readLocalDevices([]string{root, filepath.Join(root, "missing")})
	if len(devices) != 0 || len(warnings) != 3 {
		t.Fatalf("invalid records accepted or hidden: %+v, %v", devices, warnings)
	}
}

func TestLocalDeviceReturnsAllCandidatesInStableOrder(t *testing.T) {
	root := t.TempDir()
	writeDeviceStorage(t, root, "Trae CN", "\ufeff"+`{"telemetry.machineId":"machine-fixture","iCubeAuthInfo://icube-dc:2222222222222222":{},"iCubeAuthInfo://icube-dc:1111111111111111":{}}`)
	devices, warnings := readLocalDevices([]string{root})
	if len(devices) != 2 || len(warnings) != 0 || devices[0].DeviceID != "1111111111111111" || devices[1].DeviceID != "2222222222222222" {
		t.Fatal("multiple devices not preserved")
	}
}
