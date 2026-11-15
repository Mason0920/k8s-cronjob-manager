package config

import (
	"strings"
	"testing"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := FromLookup(lookup(map[string]string{"NAMESPACES": " batch, jobs ,batch,"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Namespaces, ",") != "batch,jobs" {
		t.Fatalf("namespaces = %v", cfg.Namespaces)
	}
	if cfg.ListenAddr != ":8080" || cfg.ReadOnly || cfg.AuthToken != "" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.LogTailLines != DefaultLogTailLines || cfg.LogLimitBytes != DefaultLogLimitBytes {
		t.Fatalf("log defaults = %d %d", cfg.LogTailLines, cfg.LogLimitBytes)
	}
	if !cfg.LabelSelector.Empty() {
		t.Fatalf("selector = %q, want empty", cfg.LabelSelector)
	}
}

func TestFullConfig(t *testing.T) {
	cfg, err := FromLookup(lookup(map[string]string{
		"NAMESPACES":      "batch",
		"LABEL_SELECTOR":  "team=data,tier!=critical",
		"READ_ONLY":       "true",
		"AUTH_TOKEN":      "0123456789abcdef",
		"LISTEN_ADDR":     "127.0.0.1:9090",
		"LOG_TAIL_LINES":  "100",
		"LOG_LIMIT_BYTES": "2048",
		"LOG_LEVEL":       "DEBUG",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReadOnly || cfg.ListenAddr != "127.0.0.1:9090" || cfg.LogTailLines != 100 || cfg.LogLimitBytes != 2048 || cfg.LogLevel != "debug" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.LabelSelector.String() != "team=data,tier!=critical" {
		t.Fatalf("selector = %q", cfg.LabelSelector)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"missing namespaces": {map[string]string{}, "NAMESPACES is required"},
		"empty namespaces":   {map[string]string{"NAMESPACES": " , "}, "NAMESPACES is required"},
		"invalid namespace":  {map[string]string{"NAMESPACES": "Bad_NS"}, "invalid namespace"},
		"bad selector":       {map[string]string{"NAMESPACES": "a", "LABEL_SELECTOR": "a==="}, "LABEL_SELECTOR"},
		"bad read only":      {map[string]string{"NAMESPACES": "a", "READ_ONLY": "maybe"}, "READ_ONLY"},
		"short token":        {map[string]string{"NAMESPACES": "a", "AUTH_TOKEN": "short"}, "AUTH_TOKEN"},
		"bad tail":           {map[string]string{"NAMESPACES": "a", "LOG_TAIL_LINES": "0"}, "LOG_TAIL_LINES"},
		"bad limit":          {map[string]string{"NAMESPACES": "a", "LOG_LIMIT_BYTES": "x"}, "LOG_LIMIT_BYTES"},
		"bad level":          {map[string]string{"NAMESPACES": "a", "LOG_LEVEL": "loud"}, "LOG_LEVEL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := FromLookup(lookup(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
