package access

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxUserConcurrency = 1024
const MaxAvatarBytes = 20 * 1024

var ErrInvalidProfile = errors.New("个人资料无效")

// ProfileUpdate deliberately excludes identity, permissions and accounting.
// Nil means preserve, an empty string explicitly clears the selected field.
type ProfileUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
	Avatar      *string `json:"avatar,omitempty"`
}

func ValidateUserConcurrency(value int) error {
	if value < 0 || value > MaxUserConcurrency {
		return fmt.Errorf("用户并发上限须为 0–%d，0 表示不限", MaxUserConcurrency)
	}
	return nil
}

type UserConcurrencyError struct{}

func (*UserConcurrencyError) Error() string {
	return "已达到用户并发上限，请在现有请求结束后重试"
}
func (*UserConcurrencyError) ConcurrencyExceeded() bool { return true }
func (*UserConcurrencyError) NonRetryableRequest() bool { return true }

func (p *ProfileUpdate) Normalize() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w：%s", ErrInvalidProfile, err)
		}
	}()
	if p.DisplayName == nil && p.Avatar == nil {
		return fmt.Errorf("请提供昵称或头像")
	}
	if p.DisplayName != nil {
		name := strings.TrimSpace(*p.DisplayName)
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 40 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return fmt.Errorf("昵称最多 40 字，不能包含控制字符")
		}
		p.DisplayName = &name
	}
	if p.Avatar != nil && *p.Avatar != "" {
		if err := validateAvatar(*p.Avatar); err != nil {
			return err
		}
	}
	return nil
}

// Accept only small inline raster images. Never fetch remote URLs or accept SVG.
// Validation is done on profile writes, outside the inference/storage lock.
func validateAvatar(value string) error {
	if len(value) > base64.StdEncoding.EncodedLen(MaxAvatarBytes)+32 {
		return fmt.Errorf("头像须不超过 20KB")
	}
	header, encoded, ok := strings.Cut(value, ",")
	formats := map[string]string{"data:image/png;base64": "png", "data:image/jpeg;base64": "jpeg", "data:image/gif;base64": "gif"}
	format, supported := formats[header]
	if !ok || !supported {
		return fmt.Errorf("头像仅支持 PNG、JPEG 或 GIF 图片")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > MaxAvatarBytes {
		return fmt.Errorf("头像编码无效或超过 20KB")
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || actual != format || config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		return fmt.Errorf("头像图片无效，宽高须不超过 1024 像素")
	}
	if _, actual, err = image.Decode(bytes.NewReader(raw)); err != nil || actual != format {
		return fmt.Errorf("头像图片数据不完整")
	}
	return nil
}
