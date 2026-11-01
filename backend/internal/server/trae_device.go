package server

import (
	"net/http"

	"codearts2api/internal/trae"
)

func (s *Server) handleTraeDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET", "invalid_request_error")
		return
	}
	devices, warnings := trae.ReadLocalDevices()
	message := "读取的是面板运行电脑上的 Trae 官方客户端记录。"
	if len(devices) == 0 {
		message = "未找到完整的 Trae 官方客户端设备信息。请在已登录客户端的电脑执行读取命令。"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": devices, "warnings": warnings, "message": message})
}
