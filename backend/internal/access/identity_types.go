package access

// Consumer login settings are separate from all upstream-provider credentials.
type OAuthSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}
type IdentitySettings struct {
	PublicOrigin string        `json:"public_origin"`
	SMTPHost     string        `json:"smtp_host"`
	SMTPPort     int           `json:"smtp_port"`
	SMTPUser     string        `json:"smtp_user"`
	SMTPPassword string        `json:"smtp_password"`
	SMTPFrom     string        `json:"smtp_from"`
	GitHub       OAuthSettings `json:"github"`
	LinuxDO      OAuthSettings `json:"linuxdo"`
}
