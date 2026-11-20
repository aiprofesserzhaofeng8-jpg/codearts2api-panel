package officeace

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	v4Algorithm  = "SDK-HMAC-SHA256"
	v11Algorithm = "V11-HMAC-SHA256"
	dateFormat   = "20060102T150405Z"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacHex(key []byte, data string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// jsURLEncode encodes RFC3986 unreserved characters only.
func jsURLEncode(s string) string {
	const upperhex = "0123456789ABCDEF"
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			sb.WriteByte(c)
		default:
			sb.WriteByte('%')
			sb.WriteByte(upperhex[c>>4])
			sb.WriteByte(upperhex[c&0x0f])
		}
	}
	return sb.String()
}

// Canonical URI segment-by-segment encoding with trailing slash.
func canonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if dec, err := url.PathUnescape(s); err == nil {
			segs[i] = jsURLEncode(dec)
		} else {
			segs[i] = jsURLEncode(s)
		}
	}
	out := strings.Join(segs, "/")
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

// Canonical query parameters sorted by key.
func canonicalQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, jsURLEncode(k)+"="+jsURLEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// signV4 signs standard Huawei Cloud SDK-HMAC-SHA256 requests (e.g. client-permission-validate).
func signV4(ak, sk, securityToken, method, rawURL string, body []byte, headers map[string]string, dateOverride string) (map[string]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range headers {
		out[strings.ToLower(k)] = v
	}
	out["host"] = u.Host
	if dateOverride != "" {
		out["x-sdk-date"] = dateOverride
	} else if _, ok := out["x-sdk-date"]; !ok {
		out["x-sdk-date"] = time.Now().UTC().Format(dateFormat)
	}
	if securityToken != "" {
		out["x-security-token"] = securityToken
	}

	payload := body
	switch method {
	case "PUT", "PATCH", "POST":
	default:
		payload = nil
	}

	names := make([]string, 0, len(out))
	for k := range out {
		names = append(names, k)
	}
	sort.Strings(names)

	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k)
		ch.WriteByte(':')
		ch.WriteString(strings.TrimSpace(out[k]))
		ch.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(u),
		canonicalQuery(u),
		ch.String(),
		signedHeaders,
		sha256Hex(payload),
	}, "\n")

	stringToSign := strings.Join([]string{
		v4Algorithm,
		out["x-sdk-date"],
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signature := hmacHex([]byte(sk), stringToSign)
	out["Authorization"] = v4Algorithm + " Access=" + ak +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	return out, nil
}

// signV11 signs APIG regional scope requests (V11-HMAC-SHA256) for OfficeAce subscriptions.
// SigningKey = hex(HMAC(HMAC(AK, SK), scope + "\x01")[:32])
func signV11(ak, sk, securityToken, projectID, method, rawURL string, body []byte, headers map[string]string, dateOverride string) (map[string]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range headers {
		out[strings.ToLower(k)] = v
	}
	out["host"] = u.Host
	out["x-subscription-type"] = "v2"
	if dateOverride != "" {
		out["x-sdk-date"] = dateOverride
	} else if _, ok := out["x-sdk-date"]; !ok {
		out["x-sdk-date"] = time.Now().UTC().Format(dateFormat)
	}
	if securityToken != "" {
		out["x-security-token"] = securityToken
	}
	if projectID != "" {
		out["x-project-id"] = projectID
	}
	if method == "POST" {
		if _, ok := out["content-type"]; !ok {
			out["content-type"] = "application/json;charset=utf8"
		}
	}

	payload := body
	switch method {
	case "PUT", "PATCH", "POST":
	default:
		payload = nil
	}

	names := make([]string, 0, len(out))
	for k := range out {
		names = append(names, k)
	}
	sort.Strings(names)

	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k)
		ch.WriteByte(':')
		ch.WriteString(strings.TrimSpace(out[k]))
		ch.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(u),
		canonicalQuery(u),
		ch.String(),
		signedHeaders,
		sha256Hex(payload),
	}, "\n")

	xdate := out["x-sdk-date"]
	day := xdate
	if len(day) >= 8 {
		day = day[:8]
	}
	scope := day + "/" + Region + "/" + Service

	stringToSign := strings.Join([]string{
		v11Algorithm,
		xdate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Step 1: u1 = HMAC-SHA256(ak, sk)
	mac1 := hmac.New(sha256.New, []byte(ak))
	mac1.Write([]byte(sk))
	u1 := mac1.Sum(nil)

	// Step 2: derivedKey = hex(HMAC-SHA256(u1, scope + "\x01")[:32])
	mac2 := hmac.New(sha256.New, u1)
	mac2.Write([]byte(scope))
	mac2.Write([]byte{1})
	derivedRaw := mac2.Sum(nil)
	if len(derivedRaw) > 32 {
		derivedRaw = derivedRaw[:32]
	}
	derivedHex := hex.EncodeToString(derivedRaw)

	// Step 3: signature = hex(HMAC-SHA256(derivedHex, stringToSign))
	signature := hmacHex([]byte(derivedHex), stringToSign)

	out["Authorization"] = v11Algorithm + " Credential=" + ak + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	return out, nil
}
