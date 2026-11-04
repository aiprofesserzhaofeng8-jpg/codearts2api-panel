package teleagent

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	path := CreditPath
	h := identityHeaders(a)
	key := "current_points_balance"
	if a.OperationToken != "" {
		path = PortalPath
		key = "pointsBalance"
		h = http.Header{"X-Token": {a.OperationToken}, "Userid": {a.AccountID}, "Accept": {"application/json"}}
		cookie := "operationToken=" + a.OperationToken
		if a.OperToken != "" {
			cookie += "; operToken=" + a.OperToken
		}
		h.Set("Cookie", cookie)
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "TeleAgent", http.MethodGet, c.Origin+path, h, nil)
	if e != nil {
		return nil, e
	}
	o, e := unwrap(resp)
	if e != nil {
		return nil, e
	}
	if id := accountField(o, "accountId", "account_id", "userId"); id != "" && id != a.AccountID {
		return nil, invalid("积分返回身份与当前账号不一致")
	}
	balance, e := accounts.QuotaNumber(o, key)
	if e != nil || balance != nil && *balance < 0 {
		return nil, providerhttp.Broken("TeleAgent")
	}
	info := accounts.CreditInfo{Kind: "credits", Unit: "credits", UpdatedAt: time.Now().Unix(), PackageName: providerhttp.Field(o, "plan_display_name", "planName")}
	if balance != nil {
		info.Available = true
		info.Windows = []accounts.QuotaWindow{{ID: "balance", Name: "积分余额", Unit: "credits", Remaining: balance}}
	} else {
		info.Reason = "上游未返回 TeleAgent 积分余额，无法推断剩余额度"
	}
	return json.Marshal(info)
}
