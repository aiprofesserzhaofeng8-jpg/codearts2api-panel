package qoder

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" // Required by the upstream COSY wire protocol, not password storage.
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Public protocol constants; this is Qoder's server public key, not a secret.
const serverPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

const bodyAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"

func encodeBody(raw []byte) string {
	s := base64.StdEncoding.EncodeToString(raw)
	const standard = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	for i, ch := range b {
		if ch == '=' {
			b[i] = '$'
		} else {
			b[i] = bodyAlphabet[strings.IndexByte(standard, ch)]
		}
	}
	k := len(b) / 3
	return string(b[len(b)-k:]) + string(b[k:len(b)-k]) + string(b[:k])
}

func (c *Client) signedRequest(ctx context.Context, a Credentials, method, path, model string, raw []byte) (*http.Response, error) {
	r, ok := c.Regions[a.Scope]
	if !ok {
		return nil, fmt.Errorf("Qoder 地区无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Per-call signing has no shared mutable account cache or token lifetime.
	keyID, err := uuid()
	if err != nil {
		return nil, err
	}
	key := []byte(strings.ReplaceAll(keyID, "-", "")[:16])
	info, err := json.Marshal(map[string]any{
		"aid": a.UID, "uid": a.UID, "name": a.Name, "yx_uid": "",
		"organization_id": a.OrganizationID, "organization_name": a.OrganizationName,
		"user_type": a.UserType, "security_oauth_token": a.AccessToken, "refresh_token": a.RefreshToken,
	})
	if err != nil {
		return nil, fmt.Errorf("Qoder 身份编码失败")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("Qoder 身份编码失败")
	}
	pad := aes.BlockSize - len(info)%aes.BlockSize
	info = append(info, bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher.NewCBCEncrypter(block, key).CryptBlocks(info, info)
	pemBlock, _ := pem.Decode([]byte(serverPublicKey))
	if pemBlock == nil {
		return nil, fmt.Errorf("Qoder 协议公钥无效")
	}
	parsed, err := x509.ParsePKIXPublicKey(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("Qoder 协议公钥无效")
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("Qoder 协议公钥无效")
	}
	wrapped, err := rsa.EncryptPKCS1v15(rand.Reader, pub, key)
	if err != nil {
		return nil, fmt.Errorf("Qoder 身份密钥编码失败")
	}
	requestID, err := uuid()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{"version": "v1", "requestId": requestID, "info": base64.StdEncoding.EncodeToString(info), "cosyVersion": Version, "ideVersion": ""})
	if err != nil {
		return nil, fmt.Errorf("Qoder 签名编码失败")
	}
	body := encodeBody(raw)
	payload64, cosyKey := base64.StdEncoding.EncodeToString(payload), base64.StdEncoding.EncodeToString(wrapped)
	date := strconv.FormatInt(time.Now().Unix(), 10)
	signedPath := strings.TrimPrefix(strings.SplitN(path, "?", 2)[0], "/algo")
	digest := md5.Sum([]byte(strings.Join([]string{payload64, cosyKey, date, body, signedPath}, "\n")))
	req, err := http.NewRequestWithContext(ctx, method, r.Inference+path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Qoder 请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if model != "" {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("X-Model-Key", model)
		req.Header.Set("X-Model-Source", "system")
	}
	for k, v := range map[string]string{
		"Authorization": "Bearer COSY." + payload64 + "." + hex.EncodeToString(digest[:]),
		"Cosy-Key":      cosyKey, "Cosy-Date": date, "Cosy-User": a.UID,
		"Cosy-Version": Version, "Cosy-ClientType": "5", "Cosy-MachineId": a.MachineID,
		"Cosy-MachineOS":        machineOS(),
		"Cosy-Business-Product": "cli", "Cosy-Business-Type": "agent", "Cosy-Scene": "assistant",
		"Cosy-Data-Policy": "agree", "Login-Version": "v2", "User-Agent": "qoder/" + Version, "Cache-Control": "no-cache",
	} {
		req.Header.Set(k, v)
	}
	if a.OrganizationID != "" {
		req.Header.Set("Cosy-Organization-Id", a.OrganizationID)
	}
	if len(a.OrganizationTags) > 0 {
		req.Header.Set("Cosy-Organization-Tags", strings.Join(a.OrganizationTags, ","))
	}
	// Modern clients omit MachineToken/Type when the native UMID bridge is absent.
	// Do not fabricate hardware tokens, hostnames, or a client IP.
	return c.do(req)
}

func machineOS() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "386":
		arch = "x86"
	}
	os := runtime.GOOS
	if os == "windows" {
		os = "win32"
	}
	return arch + "_" + os
}
