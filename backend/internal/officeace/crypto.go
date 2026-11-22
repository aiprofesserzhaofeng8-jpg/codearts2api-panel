package officeace

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func pad32(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

type ecJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}

func newPKCEPair() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = fmt.Sprintf("%x", buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, b64url(sum[:]), nil
}

func newDPoPKeyPair() (privJWK, pubJWK json.RawMessage, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	priv, err := json.Marshal(map[string]string{
		"kty": "EC", "crv": "P-256",
		"x": b64url(pad32(key.X)),
		"y": b64url(pad32(key.Y)),
		"d": b64url(pad32(key.D)),
	})
	if err != nil {
		return nil, nil, err
	}
	pub, err := json.Marshal(map[string]string{
		"kty": "EC", "crv": "P-256",
		"x": b64url(pad32(key.X)),
		"y": b64url(pad32(key.Y)),
	})
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

func parseECPrivateJWK(raw json.RawMessage) (*ecdsa.PrivateKey, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("DPoP 私钥缺失")
	}
	var j ecJWK
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("DPoP 私钥解析失败: %w", err)
	}
	if j.Kty != "EC" || j.Crv != "P-256" {
		return nil, fmt.Errorf("DPoP 私钥类型不支持: kty=%s crv=%s", j.Kty, j.Crv)
	}
	x, err := b64urlDecode(j.X)
	if err != nil {
		return nil, fmt.Errorf("DPoP 私钥 x 解析失败: %w", err)
	}
	y, err := b64urlDecode(j.Y)
	if err != nil {
		return nil, fmt.Errorf("DPoP 私钥 y 解析失败: %w", err)
	}
	d, err := b64urlDecode(j.D)
	if err != nil {
		return nil, fmt.Errorf("DPoP 私钥 d 解析失败: %w", err)
	}
	curve := elliptic.P256()
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		},
		D: new(big.Int).SetBytes(d),
	}
	if !curve.IsOnCurve(key.X, key.Y) {
		return nil, fmt.Errorf("DPoP 公钥不在 P-256 曲线上")
	}
	return key, nil
}

func publicJWKMap(privateJWK, publicJWK json.RawMessage) (map[string]any, error) {
	if len(publicJWK) > 0 && string(publicJWK) != "null" {
		var pub map[string]any
		if err := json.Unmarshal(publicJWK, &pub); err == nil {
			delete(pub, "d")
			if pub["x"] != nil && pub["y"] != nil {
				return pub, nil
			}
		}
	}
	var j ecJWK
	if err := json.Unmarshal(privateJWK, &j); err != nil {
		return nil, fmt.Errorf("DPoP 私钥解析失败: %w", err)
	}
	if j.X == "" || j.Y == "" {
		return nil, fmt.Errorf("DPoP 公钥缺失，且无法从私钥派生")
	}
	return map[string]any{"kty": j.Kty, "crv": j.Crv, "x": j.X, "y": j.Y}, nil
}

func dpopProof(privateJWK, publicJWK json.RawMessage, htm, htu string) (string, error) {
	key, err := parseECPrivateJWK(privateJWK)
	if err != nil {
		return "", err
	}
	pub, err := publicJWKMap(privateJWK, publicJWK)
	if err != nil {
		return "", err
	}
	header := map[string]any{"alg": "ES256", "typ": "dpop+jwt", "jwk": pub}
	payload := map[string]any{
		"htm": htm,
		"htu": htu,
		"iat": time.Now().Unix(),
		"jti": randomHex(32),
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := b64url(hb) + "." + b64url(pb)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + b64url(sig), nil
}
