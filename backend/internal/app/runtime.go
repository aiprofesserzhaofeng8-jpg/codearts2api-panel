package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"codearts2api/internal/server"
	"codearts2api/internal/storage"
)

type Options struct {
	Host    string
	Port    int
	Version string
}

type httpLifecycle interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// Runtime is owned by the process entrypoint. Start and Close are each called
// once; signal registration and fatal exit policy remain in cmd/panel.
type Runtime struct {
	http           httpLifecycle
	save           func() error
	stopBackground context.CancelFunc
	errors         chan error
	logf           func(string, ...any)
}

// Start applies configured identity before starting background work and HTTP.
// Build itself remains free of upstream I/O and background tasks.
func Start(st *storage.Store, srv *server.Server, options Options) *Runtime {
	srv.ApplyIdentity()
	stop := startBackground(st.Save, srv.RefreshModels, srv.RunDailyClaim, log.Printf)
	addr := net.JoinHostPort(options.Host, fmt.Sprintf("%d", options.Port))
	runtime := &Runtime{
		http: &http.Server{
			Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 20 * time.Second,
			// Streaming can last minutes; retain the absence of WriteTimeout.
		},
		save: st.Save, stopBackground: stop, errors: make(chan error, 1), logf: log.Printf,
	}
	go runtime.serve(func() {
		log.Printf("CodeArts2API Panel %s 已启动: http://%s/", options.Version, addr)
		if st.HasKeys() {
			log.Printf("已签发 API 密钥，调用需携带 Authorization: Bearer sk-ca-...")
		} else if st.Settings().AllowAnonymous {
			log.Printf("⚠️  尚未签发 API 密钥，且已允许匿名调用：任何能访问本端口的人都可以使用账号额度")
		} else {
			log.Printf("尚未签发 API 密钥：/v1/* 目前仅允许本机直连，请在控制台创建密钥")
		}
		switch {
		case st.Settings().AdminPassword != "":
			log.Printf("控制台需要统一账号登录（8 小时角色会话；管理员用户名 admin）")
		case server.IsLoopbackHost(options.Host):
			log.Printf("未设置管理密码，管理接口仅允许本机直连访问（可用 -admin-password 设置）")
		default:
			log.Printf("⚠️  监听 %s 且未设置管理密码：管理接口将拒绝一切非本机直连请求，请用 -admin-password 设置密码", options.Host)
		}
	})
	return runtime
}

func (r *Runtime) serve(started func()) {
	started()
	if err := r.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		r.errors <- err
	}
}

// Errors reports a fatal listener failure to the process owner. Normal HTTP
// shutdown does not send an error or close the channel.
func (r *Runtime) Errors() <-chan error { return r.errors }

// Close preserves the original order: drain HTTP for at most 10 seconds,
// attempt the final save even on shutdown failure, then cancel background
// polling. It does not force-close active handlers or join background tasks.
func (r *Runtime) Close() {
	defer r.stopBackground()
	r.logf("正在关闭…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.http.Shutdown(ctx); err != nil {
		r.logf("关闭异常: %v", err)
	}
	if err := r.save(); err != nil {
		r.logf("保存状态失败: %v", err)
	}
	r.logf("已退出")
}
