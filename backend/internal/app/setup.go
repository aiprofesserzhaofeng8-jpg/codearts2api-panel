package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/server"
	"codearts2api/internal/storage"
)

// RunInitialSetup owns a temporary HTTP server before Build/Start. No provider
// clients, renewal jobs, reward loops or periodic state saves exist yet.
func RunInitialSetup(st *storage.Store, options Options, stop <-chan os.Signal) (bool, error) {
	if err := st.PrepareInstallation(); err != nil {
		return false, err
	}
	installation := access.NewInstallation(st)
	done, failed := make(chan struct{}), make(chan error, 1)
	addr := net.JoinHostPort(options.Host, fmt.Sprintf("%d", options.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false, err
	}
	httpServer := &http.Server{Handler: server.NewSetupHandler(installation, func() { close(done) }), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("首次安装：打开 http://%s/ ，设置管理员邮箱和密码。请在对外开放服务前完成初始化。", addr)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	completed := false
	select {
	case <-done:
		completed = true
	case <-stop:
	case err = <-failed:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if shutdownErr := httpServer.Shutdown(ctx); shutdownErr != nil {
		_ = httpServer.Close()
		return false, shutdownErr
	}
	return completed, err
}
