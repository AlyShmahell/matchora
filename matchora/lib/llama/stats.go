package llama

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

type ModelStat struct {
	Role   string  `json:"role"`
	Name   string  `json:"name"`
	TokS   float64 `json:"tok_s"`
	Device string  `json:"device"`
}

func Stats(cfg config.Config) []ModelStat {
	ngl := nglOf(cfg)
	var out []ModelStat
	if strings.TrimSpace(cfg.Llama.BaseURL) != "" {
		out = append(out, probe("embed", cfg.Llama.EmbedFile, cfg.Llama.BaseURL, ngl, true))
	}
	if cfg.LocalInstruct() {
		out = append(out, probe("instruct", cfg.Llama.InstructFile, cfg.ChatBaseURL(), ngl, false))
	}
	return out
}

func probe(role, file, base string, ngl int, embed bool) ModelStat {
	st := ModelStat{
		Role:   role,
		Name:   modelName(file),
		Device: deviceFallback(ngl),
	}
	origin := originOf(base)
	var metrics, props []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		metrics, _ = get(origin + "/metrics")
	}()
	go func() {
		defer wg.Done()
		props, _ = get(origin + "/props")
	}()
	wg.Wait()
	key := "predicted_tokens_seconds"
	if embed {
		key = "prompt_tokens_seconds"
	}
	st.TokS = metricValue(string(metrics), key)
	if d := deviceFrom(props, metrics, ngl); d != "" {
		st.Device = d
	}
	return st
}

func originOf(base string) string {
	u := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(u, "/v1") {
		u = strings.TrimSuffix(u, "/v1")
	}
	return u
}

func modelName(file string) string {
	b := filepath.Base(strings.TrimSpace(file))
	return strings.TrimSuffix(b, ".gguf")
}

func deviceFallback(ngl int) string {
	if ngl != 0 {
		return "gpu"
	}
	return "cpu"
}

func get(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func metricValue(body, key string) float64 {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if i := strings.IndexByte(name, '{'); i >= 0 {
			name = name[:i]
		}
		if name != key && !strings.HasSuffix(name, ":"+key) && !strings.HasSuffix(name, "_"+key) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err == nil {
			return v
		}
	}
	return 0
}

func deviceFrom(props, metrics []byte, ngl int) string {
	if n, ok := metricValueOK(string(metrics), "n_gpu_layers"); ok {
		if n > 0 {
			return "gpu"
		}
		return "cpu"
	}
	if n, ok := metricValueOK(string(metrics), "n-gpu-layers"); ok {
		if n > 0 {
			return "gpu"
		}
		return "cpu"
	}
	if d := deviceFromJSON(props); d != "" {
		return d
	}
	return deviceFallback(ngl)
}

func metricValueOK(body, key string) (float64, bool) {
	if !strings.Contains(body, key) {
		return 0, false
	}
	v := metricValue(body, key)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if i := strings.IndexByte(name, '{'); i >= 0 {
			name = name[:i]
		}
		if name == key || strings.HasSuffix(name, ":"+key) || strings.HasSuffix(name, "_"+key) {
			return v, true
		}
	}
	return 0, false
}

func deviceFromJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	if n, ok := findFloat(v, "n_gpu_layers", "n-gpu-layers", "gpu_layers"); ok {
		if n > 0 {
			return "gpu"
		}
		return "cpu"
	}
	if s := findString(v, "device"); s != "" {
		ls := strings.ToLower(s)
		if strings.Contains(ls, "cpu") {
			return "cpu"
		}
		if strings.Contains(ls, "gpu") || strings.Contains(ls, "vulkan") ||
			strings.Contains(ls, "cuda") || strings.Contains(ls, "metal") ||
			strings.Contains(ls, "rocm") {
			return "gpu"
		}
	}
	return ""
}

func findFloat(v any, keys ...string) (float64, bool) {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var found *float64
	walkJSON(v, func(k string, val any) {
		if found != nil || !want[k] {
			return
		}
		switch t := val.(type) {
		case float64:
			found = &t
		case json.Number:
			if n, err := t.Float64(); err == nil {
				found = &n
			}
		}
	})
	if found == nil {
		return 0, false
	}
	return *found, true
}

func findString(v any, key string) string {
	var s string
	walkJSON(v, func(k string, val any) {
		if s != "" || k != key {
			return
		}
		if t, ok := val.(string); ok {
			s = t
		}
	})
	return s
}

func walkJSON(v any, fn func(k string, val any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			fn(k, val)
			walkJSON(val, fn)
		}
	case []any:
		for _, val := range t {
			walkJSON(val, fn)
		}
	}
}
