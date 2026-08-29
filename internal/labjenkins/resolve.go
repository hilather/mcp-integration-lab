// Package labjenkins resolves jwt-rs-lab IdP settings from profile values.
// Pure logic: no docker. Keycloak is the default; Entra JWKS is used only
// when a team profile fills three UUID-shaped IDs.
package labjenkins

import (
	"fmt"
	"strings"
)

const (
	IDPKeycloak = "keycloak"
	IDPEntra    = "entra"

	PlaceholderTenant  = "{tenant-id}"
	PlaceholderAPI     = "{api-app-id}"
	PlaceholderGateway = "{gateway-app-id}"

	KeycloakJWKS     = "http://keycloak:8080/realms/jwt-rs-lab/protocol/openid-connect/certs"
	KeycloakAudience = "jenkins-api"
)

// Input is the profile-owned Entra fill-in (empty means Keycloak).
type Input struct {
	Enabled      bool
	TenantID     string
	APIAppID     string
	GatewayAppID string
}

// Result is what compose and labinfo interpolate.
type Result struct {
	IDP          string
	JWKSURL      string
	Audience     string
	GatewayAppID string
}

// Resolve picks Keycloak or Entra JWKS/audience. Placeholder tokens never
// become running values. Audience is always a GUID (or the Keycloak
// lab audience), never api://….
func Resolve(in Input) (Result, error) {
	tenant := strings.TrimSpace(in.TenantID)
	api := strings.TrimSpace(in.APIAppID)
	gw := strings.TrimSpace(in.GatewayAppID)

	for _, v := range []string{tenant, api, gw} {
		if isPlaceholder(v) {
			return Result{}, fmt.Errorf("entra id is a placeholder %q; fill a real GUID or leave the field empty for Keycloak", v)
		}
	}

	if tenant == "" && api == "" && gw == "" {
		return keycloakResult(), nil
	}
	if !in.Enabled {
		// Mid-edit in a disabled profile: do not fail on partial GUIDs.
		return keycloakResult(), nil
	}
	if tenant == "" || api == "" || gw == "" {
		return Result{}, fmt.Errorf("ENTRA_TENANT_ID, ENTRA_API_APP_ID, and ENTRA_GATEWAY_APP_ID must all be set or all empty")
	}
	if !isUUID(tenant) || !isUUID(api) || !isUUID(gw) {
		return Result{}, fmt.Errorf("entra ids must be UUID-shaped (8-4-4-4-12 hex)")
	}
	tenant = strings.ToLower(tenant)
	api = strings.ToLower(api)
	gw = strings.ToLower(gw)
	return Result{
		IDP:          IDPEntra,
		JWKSURL:      "https://login.microsoftonline.com/" + tenant + "/discovery/v2.0/keys",
		Audience:     api,
		GatewayAppID: gw,
	}, nil
}

func keycloakResult() Result {
	return Result{
		IDP:      IDPKeycloak,
		JWKSURL:  KeycloakJWKS,
		Audience: KeycloakAudience,
	}
}

func isPlaceholder(v string) bool {
	switch v {
	case PlaceholderTenant, PlaceholderAPI, PlaceholderGateway:
		return true
	}
	return false
}

func isUUID(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) != 36 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(c) {
				return false
			}
		}
	}
	return true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
