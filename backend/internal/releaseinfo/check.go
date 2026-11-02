// Package releaseinfo reads public release metadata, outside inference paths.
package releaseinfo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Repository = "aiprofesserzhaofeng8-jpg/codearts2api-panel"
const ReleasesURL = "https://github.com/" + Repository + "/releases"
const endpoint = "https://api.github.com/repos/" + Repository + "/releases/latest"

var stable = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	State     string `json:"state"`
	URL       string `json:"url"`
	CheckedAt string `json:"checked_at,omitempty"`
	Message   string `json:"message"`
}
type Checker struct {
	Current string
	Client  *http.Client
	mu      sync.Mutex
	cached  Status
	expires time.Time
}

func New(current string) *Checker {
	return &Checker{Current: current, Client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Checker) Status() Status {
	return Status{Current: c.Current, State: "unchecked", URL: ReleasesURL, Message: "点击检查更新，查询正式发布版本"}
}
func parse(v string) ([3]uint64, bool) {
	var out [3]uint64
	m := stable.FindStringSubmatch(v)
	if m == nil {
		return out, false
	}
	for i := range out {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
func (c *Checker) Check(ctx context.Context) Status {
	// TryLock avoids unbounded waiters during an unavailable GitHub request.
	if !c.mu.TryLock() {
		s := c.Status()
		s.State = "busy"
		s.Message = "正在检查更新，请稍后重试"
		return s
	}
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.cached
	}
	s := c.Status()
	s.State = "unavailable"
	s.Message = "暂时无法检查更新，请稍后重试"
	checked, err := c.fetch(ctx)
	if err == nil {
		s = checked
	}
	s.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	c.cached = s
	c.expires = time.Now().Add(time.Minute)
	return s
}
func (c *Checker) fetch(ctx context.Context) (Status, error) {
	s := c.Status()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return s, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "codearts2api-update-check")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		s.State = "limited"
		s.Message = "GitHub 拒绝或限制了更新检查，请稍后重试；服务器可配置 GITHUB_TOKEN 提高查询额度"
		return s, nil
	}
	if resp.StatusCode == 404 {
		s.State = "unpublished"
		s.Message = "尚未发布正式版本"
		return s, nil
	}
	if resp.StatusCode != 200 {
		return s, errors.New("release metadata unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 {
		return s, errors.New("invalid metadata size")
	}
	var r struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Draft || r.Prerelease {
		return s, errors.New("invalid release")
	}
	target, ok := parse(r.Tag)
	if !ok || !strings.HasPrefix(r.Tag, "v") {
		return s, errors.New("invalid version")
	}
	s.Latest = r.Tag
	s.URL = ReleasesURL + "/tag/" + r.Tag
	needed := map[string]bool{"SHA256SUMS": false, "panelctl.py": false, "codearts2api-panel-" + r.Tag + "-linux-" + runtime.GOARCH + ".tar.gz": false}
	for _, a := range r.Assets {
		if found, wanted := needed[a.Name]; wanted {
			if found || a.URL != ReleasesURL+"/download/"+r.Tag+"/"+a.Name {
				return s, errors.New("invalid asset")
			}
			needed[a.Name] = true
		}
	}
	for _, found := range needed {
		if !found {
			s.State = "incomplete"
			s.Message = "正式版本尚缺安装附件，暂不可通过安装工具升级"
			return s, nil
		}
	}
	current, ok := parse(c.Current)
	if !ok {
		s.State = "development"
		s.Message = "当前为开发或定制版本，请人工核对发布说明后迁移"
		return s, nil
	}
	s.State = "current"
	s.Message = "当前已是最新正式版本"
	for i := range current {
		if current[i] < target[i] {
			s.State = "available"
			s.Message = "有新版本；请阅读发布说明，通过部署工具检查兼容性并升级"
			break
		}
		if current[i] > target[i] {
			s.State = "ahead"
			s.Message = "当前版本高于最新正式发布"
			break
		}
	}
	return s, nil
}
