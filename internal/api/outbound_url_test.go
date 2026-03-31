package api

import (
	"context"
	"net/http"
	"testing"
)

func TestValidateSSOFetchURLRejectsEmbeddedCredentials(t *testing.T) {
	_, err := validateSSOFetchURL(context.Background(), "https://user:pass@example.com/.well-known/openid-configuration")
	if err == nil {
		t.Fatal("expected embedded credentials to be rejected")
	}
}

func TestValidateSSOFetchURLRejectsLoopbackByDefault(t *testing.T) {
	_, err := validateSSOFetchURL(context.Background(), "http://127.0.0.1:8080/metadata")
	if err == nil {
		t.Fatal("expected loopback URL to be rejected")
	}
}

func TestSameOriginRedirectPolicyRejectsCrossOrigin(t *testing.T) {
	redirectedReq, err := http.NewRequest(http.MethodGet, "https://idp-two.example.com/metadata", nil)
	if err != nil {
		t.Fatalf("new redirected request: %v", err)
	}
	originReq, err := http.NewRequest(http.MethodGet, "https://idp-one.example.com/metadata", nil)
	if err != nil {
		t.Fatalf("new origin request: %v", err)
	}

	err = sameOriginRedirectPolicy([]string{"https", "http"}, outboundURLOptions{allowPrivateIPs: true})(redirectedReq, []*http.Request{originReq})
	if err == nil {
		t.Fatal("expected cross-origin redirect to be rejected")
	}
}
