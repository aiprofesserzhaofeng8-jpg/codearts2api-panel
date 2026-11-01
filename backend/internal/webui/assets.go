// Package webui embeds the production frontend. Run the frontend build before
// compiling Go; an absent dist intentionally makes the build fail.
package webui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

//go:embed all:dist
var files embed.FS

func Read(name string) ([]byte, bool) {
	if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
		return nil, false
	}
	if name != "index.html" && name != "theme.js" && name != "favicon.svg" && name != "favicon.ico" && !strings.HasPrefix(name, "assets/") {
		return nil, false
	}
	data, err := files.ReadFile("dist/" + name)
	return data, err == nil
}

func Index() []byte {
	data, ok := Read("index.html")
	if !ok {
		panic("frontend index missing: run frontend build")
	}
	return data
}
func BuildID() string {
	data, err := files.ReadFile("dist/build.json")
	if err != nil {
		panic("frontend build metadata missing")
	}
	var meta struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &meta) != nil || meta.ID == "" {
		panic("invalid frontend build metadata")
	}
	return meta.ID
}
func ContentType(name string) string {
	switch path.Ext(name) {
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".html":
		return "text/html; charset=utf-8"
	}
	return "application/octet-stream"
}
