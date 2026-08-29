package labjenkins

import (
	"strings"
	"testing"
)

const (
	testTenant  = "11111111-1111-1111-1111-111111111111"
	testAPI     = "22222222-2222-2222-2222-222222222222"
	testGateway = "33333333-3333-3333-3333-333333333333"
)

func TestResolveEmptyIsKeycloak(t *testing.T) {
	got, err := Resolve(Input{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.IDP != IDPKeycloak || got.JWKSURL != KeycloakJWKS || got.Audience != KeycloakAudience {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got.JWKSURL, "microsoftonline") {
		t.Fatal("empty IDs must not point at Entra")
	}
}

func TestResolveThreeUUIDsIsEntra(t *testing.T) {
	got, err := Resolve(Input{
		Enabled:      true,
		TenantID:     testTenant,
		APIAppID:     testAPI,
		GatewayAppID: testGateway,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantJWKS := "https://login.microsoftonline.com/" + testTenant + "/discovery/v2.0/keys"
	if got.IDP != IDPEntra || got.JWKSURL != wantJWKS {
		t.Fatalf("jwks = %+v", got)
	}
	if got.Audience != testAPI {
		t.Fatalf("audience = %q, want API app GUID", got.Audience)
	}
	if strings.HasPrefix(got.Audience, "api://") {
		t.Fatal("audience must not use api://")
	}
	if got.GatewayAppID != testGateway {
		t.Fatalf("gateway = %q", got.GatewayAppID)
	}
}

func TestResolveUppercaseUUID(t *testing.T) {
	got, err := Resolve(Input{
		Enabled:      true,
		TenantID:     strings.ToUpper(testTenant),
		APIAppID:     strings.ToUpper(testAPI),
		GatewayAppID: strings.ToUpper(testGateway),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.JWKSURL, testTenant) {
		t.Fatalf("JWKS must lowercase the tenant: %s", got.JWKSURL)
	}
	if got.Audience != testAPI {
		t.Fatalf("audience = %q, want lowercase GUID", got.Audience)
	}
}

func TestResolvePlaceholderErrors(t *testing.T) {
	cases := []Input{
		{Enabled: true, TenantID: PlaceholderTenant, APIAppID: testAPI, GatewayAppID: testGateway},
		{Enabled: true, TenantID: testTenant, APIAppID: PlaceholderAPI, GatewayAppID: testGateway},
		{Enabled: true, TenantID: testTenant, APIAppID: testAPI, GatewayAppID: PlaceholderGateway},
		{Enabled: false, TenantID: PlaceholderTenant},
	}
	for _, in := range cases {
		if _, err := Resolve(in); err == nil {
			t.Fatalf("expected error for %+v", in)
		}
	}
}

func TestResolvePartialEnabledErrors(t *testing.T) {
	if _, err := Resolve(Input{Enabled: true, TenantID: testTenant}); err == nil {
		t.Fatal("expected error for partial Entra IDs when enabled")
	}
}

func TestResolvePartialDisabledKeepsKeycloak(t *testing.T) {
	got, err := Resolve(Input{Enabled: false, TenantID: testTenant})
	if err != nil {
		t.Fatal(err)
	}
	if got.IDP != IDPKeycloak || got.Audience != KeycloakAudience {
		t.Fatalf("disabled mid-edit must stay Keycloak, got %+v", got)
	}
}

func TestResolveRejectsAPIURIAsID(t *testing.T) {
	_, err := Resolve(Input{
		Enabled:      true,
		TenantID:     testTenant,
		APIAppID:     "api://" + testAPI,
		GatewayAppID: testGateway,
	})
	if err == nil {
		t.Fatal("api:// app id must not be accepted as a UUID")
	}
}
