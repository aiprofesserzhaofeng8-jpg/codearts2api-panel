package access

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
)

func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", fmt.Errorf("邮箱格式无效")
	}
	return email, nil
}
func ValidateIdentitySettings(c IdentitySettings) error {
	if len(c.SMTPHost) > 253 || len(c.SMTPUser) > 320 || len(c.SMTPPassword) > 4096 || len(c.GitHub.ClientSecret) > 4096 || len(c.LinuxDO.ClientSecret) > 4096 {
		return fmt.Errorf("登录配置过长")
	}
	if c.PublicOrigin != "" {
		u, err := url.Parse(c.PublicOrigin)
		if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()))) {
			return fmt.Errorf("公开地址必须为 HTTPS origin，本机允许 HTTP")
		}
	}
	if c.SMTPHost != "" {
		if strings.ContainsAny(c.SMTPHost, "/\\\r\n :") || c.SMTPPort < 1 || c.SMTPPort > 65535 {
			return fmt.Errorf("SMTP 主机或端口无效")
		}
		if _, err := NormalizeEmail(c.SMTPFrom); err != nil {
			return fmt.Errorf("发件邮箱无效")
		}
	}
	for _, p := range []OAuthSettings{c.GitHub, c.LinuxDO} {
		if p.Enabled && (p.ClientID == "" || p.ClientSecret == "" || c.PublicOrigin == "") {
			return fmt.Errorf("启用第三方登录需要公开地址、Client ID 和 Client Secret")
		}
	}
	return nil
}
