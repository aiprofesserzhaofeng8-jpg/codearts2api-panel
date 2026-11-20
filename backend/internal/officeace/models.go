package officeace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

// Fallback models known to be callable when rates table cannot be reached.
var FallbackModels = map[string]struct {
	Name    string
	Context int
	Output  int
}{
	"glm-5.3":                 {Name: "GLM-5.3", Context: 1000000, Output: 128000},
	"glm-5.3-flash":           {Name: "GLM-5.3-Flash", Context: 1000000, Output: 128000},
	"glm-5.2":                 {Name: "GLM-5.2", Context: 1000000, Output: 128000},
	"glm-5.1":                 {Name: "GLM-5.1", Context: 1000000, Output: 128000},
	"deepseek-v4.1-flash":     {Name: "DeepSeek-V4.1-Flash", Context: 1000000, Output: 128000},
	"deepseek-v4-flash-0731":  {Name: "DeepSeek-V4-Flash-0731", Context: 1000000, Output: 128000},
	"kimi-k3":                 {Name: "Kimi-K3", Context: 1000000, Output: 128000},
	"kimi-k3.1-pro-highspeed": {Name: "Kimi-K3.1-Pro-HighSpeed", Context: 1000000, Output: 128000},
	"openpangu-2.1-pro":       {Name: "openPangu-2.1-Pro", Context: 1000000, Output: 128000},
}

// Registry IDs that always return 404 ModelArts.81009 Invalid model on chat.
var UnavailableIDs = map[string]bool{
	"deepseek-v4.1-flash-1017": true,
	"deepseek-v4.1-flash-bnt":  true,
	"deepseek-v4-pro":          true,
}

func (c *Client) maasBase(cred Credentials) string {
	raw := strings.TrimSpace(cred.MaaSBaseURL)
	if raw == "" {
		return c.MaaSBase + "/v2"
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return strings.TrimSuffix(raw, "/")
	}
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, "/v2")
	return "https://" + raw + "/v2"
}

func basicAuthHeader(appKey, appSecret string) string {
	auth := appKey + ":" + appSecret
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

// Token rates caching
func (c *Client) fetchTokenRates(ctx context.Context, cred Credentials) (map[string]float64, error) {
	cacheKey := cred.AccountID
	if cacheKey == "" {
		cacheKey = cred.AccessKeyID
	}

	c.ratesMu.RLock()
	entry, ok := c.rates[cacheKey]
	c.ratesMu.RUnlock()

	if ok && time.Since(entry.at) < RatesTTL && len(entry.data) > 0 {
		return entry.data, nil
	}

	ratesURL := c.SubBase + TokenRatesPath
	headers, err := signV11(cred.AccessKeyID, cred.SecretAccessKey, cred.SecurityToken, cred.ProjectID, http.MethodGet, ratesURL, nil, nil, "")
	if err != nil {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ratesURL, nil)
	if err != nil {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, err
	}

	// Retry once after 400ms if empty response body and no existing cache
	if len(bytes.TrimSpace(bodyBytes)) == 0 && (!ok || len(entry.data) == 0) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		reqRetry, errRetry := http.NewRequestWithContext(ctx, http.MethodGet, ratesURL, nil)
		if errRetry == nil {
			for k, v := range headers {
				reqRetry.Header.Set(k, v)
			}
			if respRetry, errDo := c.HTTP.Do(reqRetry); errDo == nil {
				defer respRetry.Body.Close()
				if respRetry.StatusCode == 200 {
					if retryBytes, errRead := io.ReadAll(io.LimitReader(respRetry.Body, 1<<20)); errRead == nil && len(bytes.TrimSpace(retryBytes)) > 0 {
						bodyBytes = retryBytes
					}
				}
			}
		}
	}

	var rawData json.RawMessage
	if err := json.Unmarshal(bodyBytes, &rawData); err != nil {
		if ok && len(entry.data) > 0 {
			return entry.data, nil
		}
		return nil, providerhttp.Broken("OfficeAce")
	}

	type rateItem struct {
		ModelName       string  `json:"model_name"`
		ModelNameCamel  string  `json:"modelName"`
		Model           string  `json:"model"`
		Name            string  `json:"name"`
		CostCoefficient float64 `json:"cost_coefficient"`
		CostCoeffCamel  float64 `json:"costCoefficient"`
		Rate            float64 `json:"rate"`
	}

	var items []rateItem
	if err := json.Unmarshal(rawData, &items); err != nil {
		// Try wrapper object: {"models": [...]}, {"data": [...]}, {"rates": [...]}
		var obj map[string]json.RawMessage
		if json.Unmarshal(rawData, &obj) == nil {
			for _, key := range []string{"models", "data", "rates"} {
				if sub, ok := obj[key]; ok && len(sub) > 0 && string(sub) != "null" {
					if json.Unmarshal(sub, &items) == nil && len(items) > 0 {
						break
					}
				}
			}
		}
	}

	if len(items) == 0 && ok && len(entry.data) > 0 {
		return entry.data, nil
	}

	ratesMap := make(map[string]float64)
	for _, item := range items {
		name := item.ModelName
		if name == "" {
			name = item.ModelNameCamel
		}
		if name == "" {
			name = item.Model
		}
		if name == "" {
			name = item.Name
		}
		name = strings.Trim(strings.TrimSpace(name), "\r\n")
		if name == "" {
			continue
		}
		coeff := item.CostCoefficient
		if coeff == 0 {
			coeff = item.CostCoeffCamel
		}
		if coeff == 0 {
			coeff = item.Rate
		}
		ratesMap[name] = coeff
	}

	c.ratesMu.Lock()
	c.rates[cacheKey] = ratesCacheEntry{at: time.Now(), data: ratesMap}
	c.ratesMu.Unlock()

	return ratesMap, nil
}

// Models fetches dynamic models from MaaS /v2/models intersected with token-rates.
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	norm, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return nil, err
	}

	// If missing model app key, heal via client-permission-validate. The answer is
	// kept because it also tells an un-provisioned account from a lost gateway.
	var info ModelInfo
	var infoErr error
	if cred.ModelAppKey == "" {
		info, infoErr = c.fetchModelInfo(ctx, cred)
		if infoErr == nil && info.AppKey != "" {
			cred.ModelAppKey = info.AppKey
			cred.ModelAppSecret = info.AppSecret
			if info.BaseURL != "" {
				cred.MaaSBaseURL = info.BaseURL
			}
		}
	}

	if cred.ModelAppKey == "" {
		// No MaaS credentials means this account owns no model. Listing the
		// fallback catalog here would advertise models that every call is about
		// to reject, and it would hide an un-provisioned account behind a
		// healthy-looking source. Say what is actually missing instead.
		reason := UnsubscribedReason
		if infoErr == nil && info.Subscribed {
			reason = "云果办账号已开通订阅，但云端未下发模型网关凭据：请在账号菜单点「探活」重新获取，或重新授权后再同步目录"
		}
		return nil, &provider.Error{
			Status:  403,
			Code:    "OFFICEACE_NOT_PROVISIONED",
			Message: reason,
		}
	}

	base := c.maasBase(cred)
	modelsURL := base + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", basicAuthHeader(cred.ModelAppKey, cred.ModelAppSecret))
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, &provider.Error{
			Status:       resp.StatusCode,
			AccountFault: true,
			Message:      "OfficeAce MaaS 凭据失效或未开通，请重新登录",
		}
	}
	if resp.StatusCode != 200 {
		return c.staticFallbackModels(), nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return c.staticFallbackModels(), nil
	}

	var catResp struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &catResp); err != nil {
		return c.staticFallbackModels(), nil
	}

	// Fetch token rates for filtering
	rates, _ := c.fetchTokenRates(ctx, cred)

	var out []provider.Model
	seen := make(map[string]bool)

	for _, m := range catResp.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || UnavailableIDs[id] || seen[id] {
			continue
		}
		// If rates table exists, model must be present in rates
		if len(rates) > 0 {
			if _, ok := rates[id]; !ok {
				continue
			}
		} else {
			// If rates table was empty/unreachable, filter to known fallback models
			if _, ok := FallbackModels[id]; !ok {
				continue
			}
		}

		seen[id] = true
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = id
		}
		if rate, ok := rates[id]; ok && rate != 1 && rate > 0 {
			name = fmt.Sprintf("%s · x%g", name, rate)
		}

		ctxLimit := 1000000
		outLimit := 128000
		if def, ok := FallbackModels[id]; ok {
			ctxLimit = def.Context
			outLimit = def.Output
		}

		out = append(out, provider.Model{
			ID:          id,
			Name:        name,
			Provider:    ProviderID,
			Scope:       "cn",
			UpstreamID:  id,
			Context:     ctxLimit,
			MaxOutput:   outLimit,
			Source:      "upstream",
			StreamOnly:  true,
			ToolCalling: "supported",
		})
	}

	if len(out) == 0 {
		return c.staticFallbackModels(), nil
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (c *Client) staticFallbackModels() []provider.Model {
	var out []provider.Model
	for id, def := range FallbackModels {
		out = append(out, provider.Model{
			ID:          id,
			Name:        def.Name,
			Provider:    ProviderID,
			Scope:       "cn",
			UpstreamID:  id,
			Context:     def.Context,
			MaxOutput:   def.Output,
			Source:      "OfficeAce 预置目录",
			StreamOnly:  true,
			ToolCalling: "supported",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}
