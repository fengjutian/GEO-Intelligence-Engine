package audits

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"testing"
)

func TestAuditSampleKeyIsDeterministic(t *testing.T) {
	first := auditSampleKey("audit-1", "question-1", "engine-1", "deepseek-flash", "model_api", "CN", "zh-CN", "params-hash", 1)
	second := auditSampleKey("audit-1", "question-1", "engine-1", "deepseek-flash", "model_api", "CN", "zh-CN", "params-hash", 1)
	if first != second {
		t.Fatalf("sample key must be deterministic: %x vs %x", first, second)
	}
	differentSample := auditSampleKey("audit-1", "question-1", "engine-1", "deepseek-flash", "model_api", "CN", "zh-CN", "params-hash", 2)
	if first == differentSample {
		t.Fatal("different sample numbers must produce different keys")
	}
	differentChannel := auditSampleKey("audit-1", "question-1", "engine-1", "deepseek-flash", "search_api", "CN", "zh-CN", "params-hash", 1)
	if first == differentChannel {
		t.Fatal("different channels must produce different keys")
	}
	differentAudit := auditSampleKey("audit-2", "question-1", "engine-1", "deepseek-flash", "model_api", "CN", "zh-CN", "params-hash", 1)
	if first == differentAudit {
		t.Fatal("repeated audits of the same protocol must use distinct sample keys")
	}
}

func TestConfigHelpersParsePersistedJSON(t *testing.T) {
	var config map[string]any
	if err := json.Unmarshal([]byte(`{"questionVersionId":"qv-1","samplesPerQuestion":3,"estimatedCostPerSample":250,"engines":[{"engineConfigId":"engine-1","channel":"model_api"}],"budgetMicrounits":100000}`), &config); err != nil {
		t.Fatal(err)
	}
	if got := configString(config, "questionVersionId"); got != "qv-1" {
		t.Fatalf("questionVersionId = %q", got)
	}
	if got := configInt(config, "samplesPerQuestion"); got != 3 {
		t.Fatalf("samplesPerQuestion = %d", got)
	}
	if got := configInt(config, "estimatedCostPerSample"); got != 250 {
		t.Fatalf("estimatedCostPerSample = %d", got)
	}
	engines := configEngines(config)
	if len(engines) != 1 || configString(engines[0], "engineConfigId") != "engine-1" {
		t.Fatalf("unexpected engines: %+v", engines)
	}
}

func TestWilsonInterval(t *testing.T) {
	low, high := wilson(5, 10)
	if math.Abs(low-0.2366) > 0.001 || math.Abs(high-0.7634) > 0.001 {
		t.Fatalf("wilson(5,10) = [%.4f, %.4f]", low, high)
	}
	low, high = wilson(0, 10)
	if low != 0 || math.Abs(high-0.2775) > 0.001 {
		t.Fatalf("wilson(0,10) = [%.4f, %.4f]", low, high)
	}
	if low, high := wilson(0, 0); low != 0 || high != 0 {
		t.Fatalf("wilson(0,0) = [%.4f, %.4f]", low, high)
	}
}

func TestSanitizeErrorCode(t *testing.T) {
	if got := sanitizeErrorCode("model call failed: timeout"); got != "MODELCALLFAILEDTIMEOUT" {
		t.Fatalf("sanitizeErrorCode = %q", got)
	}
	if got := sanitizeErrorCode("  "); got != "MODEL_EXECUTION_FAILED" {
		t.Fatalf("empty error code = %q", got)
	}
	if got := sanitizeErrorCode("PROVIDER_UNCONFIGURED"); got != "PROVIDER_UNCONFIGURED" {
		t.Fatalf("valid error code = %q", got)
	}
}

func TestDecodeHashRejectsInvalidInput(t *testing.T) {
	if _, err := decodeHash("not-a-hash"); err == nil {
		t.Fatal("invalid hash must be rejected")
	}
	if _, err := decodeHash(""); err == nil {
		t.Fatal("empty hash must be rejected")
	}
}

func TestHashRequestStableAndScoped(t *testing.T) {
	first := hex.EncodeToString(hashRequest("project-1", "protocol-1"))
	if first != hex.EncodeToString(hashRequest("project-1", "protocol-1")) {
		t.Fatal("request hash must be stable")
	}
	if first == hex.EncodeToString(hashRequest("project-1", "protocol-2")) {
		t.Fatal("request hash must include the protocol")
	}
}
