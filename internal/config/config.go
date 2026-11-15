// Package config loads runtime settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Defaults used when the matching environment variable is not set.
const (
	DefaultListenAddr    = ":8080"
	DefaultLogTailLines  = 500
	DefaultLogLimitBytes = 1 << 20 // 1 MiB
	minTokenLength       = 16
)

// Config is the full runtime configuration.
type Config struct {
	ListenAddr    string
	Namespaces    []string
	LabelSelector labels.Selector
	ReadOnly      bool
	AuthToken     string
	Kubeconfig    string
	KubeContext   string
	LogTailLines  int64
	LogLimitBytes int64
	LogLevel      string
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	return FromLookup(os.LookupEnv)
}

// FromLookup builds a Config from any lookup function. Tests pass a map-backed
// lookup so they do not depend on the process environment.
func FromLookup(lookup func(string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		ListenAddr:    DefaultListenAddr,
		LogTailLines:  DefaultLogTailLines,
		LogLimitBytes: DefaultLogLimitBytes,
		Kubeconfig:    get("KUBECONFIG"),
		KubeContext:   get("KUBE_CONTEXT"),
		AuthToken:     get("AUTH_TOKEN"),
		LogLevel:      strings.ToLower(get("LOG_LEVEL")),
	}
	if v := get("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	var errs []error

	ns, err := parseNamespaces(get("NAMESPACES"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.Namespaces = ns

	sel, err := labels.Parse(get("LABEL_SELECTOR"))
	if err != nil {
		errs = append(errs, fmt.Errorf("LABEL_SELECTOR: %w", err))
	}
	cfg.LabelSelector = sel

	if v := get("READ_ONLY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("READ_ONLY: %w", err))
		}
		cfg.ReadOnly = b
	}

	if cfg.AuthToken != "" && len(cfg.AuthToken) < minTokenLength {
		errs = append(errs, fmt.Errorf("AUTH_TOKEN must be at least %d characters", minTokenLength))
	}

	if v := get("LOG_TAIL_LINES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("LOG_TAIL_LINES must be a positive integer"))
		}
		cfg.LogTailLines = n
	}
	if v := get("LOG_LIMIT_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("LOG_LIMIT_BYTES must be a positive integer"))
		}
		cfg.LogLimitBytes = n
	}

	switch cfg.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error"))
	}

	return cfg, errors.Join(errs...)
}

func parseNamespaces(raw string) ([]string, error) {
	if raw == "" {
		return nil, errors.New("NAMESPACES is required (comma separated allowlist)")
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		ns := strings.TrimSpace(part)
		if ns == "" || seen[ns] {
			continue
		}
		if msgs := validation.IsDNS1123Label(ns); len(msgs) > 0 {
			return nil, fmt.Errorf("NAMESPACES: invalid namespace %q: %s", ns, strings.Join(msgs, "; "))
		}
		seen[ns] = true
		out = append(out, ns)
	}
	if len(out) == 0 {
		return nil, errors.New("NAMESPACES is required (comma separated allowlist)")
	}
	return out, nil
}
