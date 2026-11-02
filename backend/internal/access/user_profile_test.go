package access

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestProfileAcceptsOnlyBoundedRasterImages(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw.Bytes())
	name := "  测试昵称  "
	p := ProfileUpdate{DisplayName: &name, Avatar: &avatar}
	if err := p.Normalize(); err != nil || *p.DisplayName != "测试昵称" {
		t.Fatal(err, p)
	}
	for _, value := range []string{"https://example.com/avatar.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,AAAA", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw.Bytes()), "data:image/png;base64," + strings.Repeat("A", 30000)} {
		p = ProfileUpdate{Avatar: &value}
		if err := p.Normalize(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatal("accepted invalid avatar", value[:min(30, len(value))], err)
		}
	}
	for _, value := range []string{strings.Repeat("字", 41), "bad\x00name"} {
		if err := (&ProfileUpdate{DisplayName: &value}).Normalize(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatal(err)
		}
	}
	if err := (&ProfileUpdate{}).Normalize(); !errors.Is(err, ErrInvalidProfile) {
		t.Fatal("empty patch", err)
	}
	for _, limit := range []int{-1, MaxUserConcurrency + 1} {
		if ValidateUserConcurrency(limit) == nil {
			t.Fatal(limit)
		}
	}
}
