package httpapi

import "testing"

func TestIsPublicHTTPURL(t *testing.T) {
	allowed := []string{"https://www.feishu.cn/llms.txt", "http://example.com/llms.txt"}
	for _, raw := range allowed {
		if !isPublicHTTPURL(raw) {
			t.Fatalf("%q should be fetchable", raw)
		}
	}
	rejected := []string{
		"file:///etc/passwd",
		"ftp://example.com/llms.txt",
		"http://localhost/llms.txt",
		"http://127.0.0.1/llms.txt",
		"http://10.0.0.5/llms.txt",
		"http://192.168.1.10/llms.txt",
		"http://169.254.169.254/latest/meta-data",
		"http://internal.local/llms.txt",
		"",
	}
	for _, raw := range rejected {
		if isPublicHTTPURL(raw) {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}
