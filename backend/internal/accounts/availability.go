package accounts

import "errors"

// ErrNoAccount retains the existing public error identity through apierr.NoAccount.
var ErrNoAccount = errors.New("没有可用账号，请先在控制台「导入本机桌面端」或在 CodeArts Agent 里登录")
