package llama

import (
	"syscall"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestProcAttrKeepsPgidAndPdeathsig(t *testing.T) {
	attr := procAttr()
	if attr == nil || !attr.Setpgid {
		t.Fatalf("Setpgid=%v", attr)
	}
	if attr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Pdeathsig=%v", attr.Pdeathsig)
	}
}

func TestStopNoSpawn(t *testing.T) {
	Stop()
}

func TestServerArgsOffload(t *testing.T) {
	args := serverArgs(8080, nglOf(config.Config{}), "--model", "x.gguf")
	ok := false
	for i, a := range args {
		if a != "-ngl" && a != "--n-gpu-layers" {
			continue
		}
		if i+1 < len(args) && (args[i+1] == "999" || args[i+1] == "-1") {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("missing -ngl 999 in %v", args)
	}
}

func TestMetricValue(t *testing.T) {
	body := `# HELP llamacpp:prompt_tokens_seconds Average prompt throughput in tokens/s.
# TYPE llamacpp:prompt_tokens_seconds gauge
llamacpp:prompt_tokens_seconds 123.4
llamacpp:predicted_tokens_seconds{model="qwen"} 45.67
`
	if got := metricValue(body, "prompt_tokens_seconds"); got != 123.4 {
		t.Fatalf("prompt=%v", got)
	}
	if got := metricValue(body, "predicted_tokens_seconds"); got != 45.67 {
		t.Fatalf("predicted=%v", got)
	}
}

func TestModelListed(t *testing.T) {
	ids := parseModelIDs([]byte(`{"data":[{"id":"all-MiniLM-L6-v2-Q4_K_M"}]}`))
	if !modelListed(ids, "all-MiniLM-L6-v2-Q4_K_M.gguf") {
		t.Fatalf("stem id not matched: %v", ids)
	}
	ids = parseModelIDs([]byte(`{"models":[{"name":"SmolLM2-135M-Instruct-Q8_0.gguf"}]}`))
	if !modelListed(ids, "SmolLM2-135M-Instruct-Q8_0.gguf") {
		t.Fatalf("filename id not matched: %v", ids)
	}
	if modelListed(ids, "all-MiniLM-L6-v2-Q4_K_M.gguf") {
		t.Fatal("unrelated file listed")
	}
}

func TestModelName(t *testing.T) {
	if got := modelName("llamacpp/models/all-MiniLM-L6-v2-Q4_K_M.gguf"); got != "all-MiniLM-L6-v2-Q4_K_M" {
		t.Fatalf("got %q", got)
	}
}

func TestDeviceFromJSON(t *testing.T) {
	if d := deviceFromJSON([]byte(`{"default_generation_settings":{"n_gpu_layers":99}}`)); d != "gpu" {
		t.Fatalf("gpu got %q", d)
	}
	if d := deviceFromJSON([]byte(`{"n_gpu_layers":0}`)); d != "cpu" {
		t.Fatalf("cpu got %q", d)
	}
	if d := deviceFrom([]byte(`{"n_gpu_layers":0}`), nil, 999); d != "cpu" {
		t.Fatalf("props cpu override got %q", d)
	}
	if d := deviceFrom(nil, nil, 999); d != "gpu" {
		t.Fatalf("fallback gpu got %q", d)
	}
	if d := deviceFrom(nil, nil, 0); d != "cpu" {
		t.Fatalf("fallback cpu got %q", d)
	}
}
