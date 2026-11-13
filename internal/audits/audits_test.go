package audits

import (
	"encoding/hex"
	"encoding/json"
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

func TestHashRequestStableAndScoped(t *testing.T) {
	first := hex.EncodeToString(hashRequest("project-1", "protocol-1"))
	if first != hex.EncodeToString(hashRequest("project-1", "protocol-1")) {
		t.Fatal("request hash must be stable")
	}
	if first == hex.EncodeToString(hashRequest("project-1", "protocol-2")) {
		t.Fatal("request hash must include the protocol")
	}
}
