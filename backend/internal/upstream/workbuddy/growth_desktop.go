package workbuddy

// Desktop/web maintenance wire follows workbuddy2api-panel desktop.go (MIT;
// LICENSE.workbuddy). No ordinary request headers or inference code is changed.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"codearts2api/internal/provider"
)

const growthDesktopUA = "WorkBuddy/5.5.6 WorkBuddy/5.5.6 CLI/2.137.1"

func growthDeviceID(a Credentials, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + a.UID))
	return hex.EncodeToString(sum[:18])
}
func (c *Client) growthDesktopHeaders(r *http.Request, a Credentials) {
	r.Header.Set("Authorization", "Bearer "+a.AccessToken)
	r.Header.Set("Accept", "application/json, text/plain, */*")
	r.Header.Set("Content-Type", "application/json;charset=UTF-8")
	r.Header.Set("User-Agent", growthDesktopUA)
	r.Header.Set("X-Domain", c.CNBase)
	r.Header.Set("X-Product", "SaaS")
	r.Header.Set("X-User-Id", a.UID)
	r.Header.Set("X-Request-ID", randomID())
}
func (c *Client) growthDesktopReport(ctx context.Context, raw json.RawMessage, events []growthDesktopEvent) error {
	if len(events) == 0 || len(events) > 20 {
		return fmt.Errorf("成长桌面事件组无效")
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	a, _ := decode(normalized)
	now := time.Now().UnixMilli()
	arr := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		m := map[string]any{"timezone": "Asia/Shanghai", "reportDelay": 2000, "userId": a.UID, "username": a.Nickname, "userNickname": a.Nickname, "product": "SaaS", "releaseDate": int64(1789036585355), "commit": "5f9692923c93033111c51ad7b003eb80204a9b75", "ideName": "WorkBuddy", "ideType": "WorkBuddy", "ideVersion": "5.5.6", "machineId": growthDeviceID(a, "machine"), "sessionId": growthDeviceID(a, "session"), "extName": "workbuddy-desktop", "extVersion": "5.5.6", "os": "win32", "arch": "x64", "osVersion": "10.0.26220", "cpuCores": 20, "memorySize": 24, "timestamp": now, "presentAt": now}
		for k, v := range ev {
			m[k] = v
		}
		arr = append(arr, m)
	}
	payload, _ := json.Marshal(arr)
	_, err = c.growthRequestWithHeaders(ctx, normalized, http.MethodPost, "/v2/report", payload, false, c.CNBase, c.growthDesktopHeaders)
	return err
}
func (c *Client) growthLibraryReport(ctx context.Context, raw json.RawMessage) error {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	a, _ := decode(normalized)
	const page = "https://www.workbuddy.cn/space/d/o0KWYeynteVv06UnAZqIFm"
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"
	payload, _ := json.Marshal([]map[string]any{{"eventCode": "web_element_click", "timestamp": time.Now().UnixMilli(), "reportDelay": 0, "pageURL": page, "elementId": "library_doc_intro_click", "elementName": "WorkBuddy资料库介绍", "os": "Win32", "arch": "", "osVersion": "10.0", "userAgent": ua, "machineId": growthDeviceID(a, "webmachine"), "userId": a.UID, "userNickname": a.Nickname, "enterpriseId": a.EnterpriseID}})
	_, err = c.growthRequestWithHeaders(ctx, normalized, http.MethodPost, "/v2/report", payload, true, c.CNWeb, func(r *http.Request, a Credentials) {
		r.Header.Set("Authorization", "Bearer "+a.AccessToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		r.Header.Set("X-Client-Platform", "web")
		r.Header.Set("Origin", c.CNWeb)
		r.Header.Set("Referer", page)
		r.Header.Set("User-Agent", ua)
		r.Header.Set("X-User-Id", a.UID)
	})
	return err
}
func growthDelay(ctx context.Context, gap time.Duration) error {
	timer := time.NewTimer(gap)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (c *Client) growthDesktopActivity(ctx context.Context, raw json.RawMessage, task provider.GrowthTask) error {
	conv, req := "wb2api-"+randomID(), randomID()
	var events []growthDesktopEvent
	switch task.Code {
	case "RichMeow_Chat":
		events = growthDesktopChatSequence(conv, req, "msg-"+req, "fast-model", "fast-model")
	case "Buddy_App", "Buddy_App_QQ":
		events = growthDesktopBuddyAppSequence("cb_y5Dy46tPQGGWtueMxXbe", "企鹅教师助手")
	case "automation_1":
		events = []growthDesktopEvent{growthDesktopAutomationCreateEvent("wb2api 自动化")}
	case "Library_read":
		return c.growthLibraryReport(ctx, raw)
	case "template_5":
		templates := [][2]string{{"1", "深度研究"}, {"2", "周报生成"}, {"3", "竞品分析"}, {"4", "活动策划"}, {"5", "代码评审"}}
		if task.Current < 0 || task.Current >= int64(len(templates)) {
			return fmt.Errorf("模板进度超出执行范围")
		}
		tp := templates[task.Current]
		events = growthDesktopTemplateUseSequence(conv, req, tp[0], tp[1])
	case "playbook_prompt":
		events = growthDesktopPlaybookPromptSequence(conv, req, "pm-gtm-launch-plan", "新产品上市 GTM 发布计划一页纸")
	case "create_canvas":
		events = growthDesktopDesignCanvasSequence(conv, req)
	case "Hp_Appearance":
		_, err := c.growthRequestWithHeaders(ctx, raw, http.MethodPost, "/v2/user-asset/appearance/set", []byte(`{"kind":"theme","resource_key":"theme-tkmw7j"}`), false, c.CNBase, c.growthDesktopHeaders)
		if err != nil {
			return fmt.Errorf("设置主题未确认：%w", err)
		}
		if err = growthDelay(ctx, 2*time.Second); err != nil {
			return err
		}
		events = []growthDesktopEvent{{"eventCode": "appearance_skin_apply", "action": "apply", "source": "settings_close", "id": "theme-tkmw7j", "vipLevel": 0, "series": "", "type": "unknown"}}
	default:
		return fmt.Errorf("当前任务尚未接入事件执行")
	}
	return c.growthDesktopReport(ctx, raw, events)
}
