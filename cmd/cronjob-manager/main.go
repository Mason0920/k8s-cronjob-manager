// Command cronjob-manager serves a small web UI and JSON API for operating
// Kubernetes CronJobs: list, run now, suspend/resume, job history and logs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // CronJob spec.timeZone needs zone data even in distroless images

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Mason0920/k8s-cronjob-manager/internal/api"
	"github.com/Mason0920/k8s-cronjob-manager/internal/config"
	"github.com/Mason0920/k8s-cronjob-manager/internal/manager"
	"github.com/Mason0920/k8s-cronjob-manager/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	restCfg, source, err := kubeConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes config: %w", err)
	}
	restCfg.UserAgent = "k8s-cronjob-manager/" + version
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	mgr := manager.New(client, manager.Options{
		Namespaces:    cfg.Namespaces,
		Selector:      cfg.LabelSelector,
		ReadOnly:      cfg.ReadOnly,
		LogTailLines:  cfg.LogTailLines,
		LogLimitBytes: cfg.LogLimitBytes,
	})

	ui, built := web.UI()
	handler := api.NewHandler(mgr, api.Options{AuthToken: cfg.AuthToken, UI: ui, Logger: logger})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	logger.Info("starting",
		"version", version,
		"addr", cfg.ListenAddr,
		"kubeconfig", source,
		"namespaces", cfg.Namespaces,
		"labelSelector", cfg.LabelSelector.String(),
		"readOnly", cfg.ReadOnly,
		"authToken", cfg.AuthToken != "",
		"uiBuilt", built,
	)
	if cfg.AuthToken == "" {
		logger.Warn("AUTH_TOKEN is not set; put this service behind an authenticating proxy or use kubectl port-forward")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-errCh
}

// kubeConfig uses the in-cluster service account when running in a pod and
// no kubeconfig was given, and the usual kubeconfig loading rules otherwise.
func kubeConfig(cfg config.Config) (*rest.Config, string, error) {
	if cfg.Kubeconfig == "" && os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		c, err := rest.InClusterConfig()
		return c, "in-cluster", err
	}
	// The default rules read $KUBECONFIG (including colon separated lists)
	// and fall back to ~/.kube/config.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: cfg.KubeContext}
	c, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	return c, "kubeconfig", err
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
