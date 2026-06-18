package logging

import (
	"strings"
	"testing"
)

// logging.go RedactPII shipps 9 scrubber patterns. Two of
// them are multi-alternative regexes that cover a specific
// enumerated set of sensitive names, and the existing
// redaction tests only hit a subset:
//
//   - envSecretPattern matches 4 provider API-key env vars:
//       OPENAI_API_KEY / ANTHROPIC_API_KEY /
//       GROK_API_KEY / GEMINI_API_KEY
//     TestRedactPII_EnvSecrets covers only OPENAI + ANTHROPIC.
//     GROK and GEMINI values would leak through unredacted if
//     a regex-alternation typo ever dropped them from the set
//     — Grok/Gemini logs would silently emit the raw key
//     until the pattern was re-audited.
//
//   - jsonSecretPattern matches 7 JSON-body field names:
//       api_key / apikey / access_token / refresh_token /
//       password / authorization / cookie
//     Existing TestRedactPII_RedactsCommonSecrets +
//     TestRedactPII_JSONAccessToken cover api_key,
//     access_token, password. The remaining 4 (apikey,
//     refresh_token, authorization, cookie) are untested —
//     silently unredacting any of them is a real credential-
//     leak vector (e.g., a refresh_token posted to an OAuth
//     endpoint would pass through logs unredacted).
//
// Pin each alternate explicitly so a regex-alternation
// drop is caught immediately.

// TestRedactPII_AllFourEnvSecrets pins that all 4 provider
// env-var names are redacted. A regression dropping
// GROK_API_KEY or GEMINI_API_KEY from the pattern would
// silently leak their values in operator logs until a
// downstream security review caught it.
func TestRedactPII_AllFourEnvSecrets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		secret string
	}{
		{"OPENAI_API_KEY", "sk-secret-openai-12345"},
		{"ANTHROPIC_API_KEY", "ant-secret-anthropic-67890"},
		{"GROK_API_KEY", "xai-secret-grok-abcdef"},
		{"GEMINI_API_KEY", "AIzaSy-gemini-xyz123"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := tc.name + "=" + tc.secret
			output := RedactPII(input)
			if strings.Contains(output, tc.secret) {
				t.Errorf("RedactPII(%q) leaked %s secret %q in output %q — provider-env-secret contract",
					input, tc.name, tc.secret, output)
			}
			want := tc.name + "=[REDACTED]"
			if !strings.Contains(output, want) {
				t.Errorf("RedactPII(%q) = %q, want substring %q — env-secret redaction marker contract",
					input, output, want)
			}
		})
	}
}

// TestRedactPII_JSONSecretKeys pins that JSON
// field names in jsonSecretPattern redact their values.
// A regex-alternation drop of e.g. "refresh_token" would
// make every OAuth-refresh JSON payload log unredacted —
// a direct credential-leak vector.
func TestRedactPII_JSONSecretKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string // JSON key as it appears in the pattern
	}{
		{"api_key", "api_key"},
		{"api-key", "api-key"},
		{"apikey", "apikey"},
		{"access_token", "access_token"},
		{"access-token", "access-token"},
		{"refresh_token", "refresh_token"},
		{"refresh-token", "refresh-token"},
		{"auth_token", "auth_token"},
		{"auth-token", "auth-token"},
		{"bearer_token", "bearer_token"},
		{"bearer-token", "bearer-token"},
		{"credential", "credential"},
		{"password", "password"},
		{"passwd", "passwd"},
		{"client_secret", "client_secret"},
		{"client-secret", "client-secret"},
		{"client_assertion", "client_assertion"},
		{"client-assertion", "client-assertion"},
		{"id_token", "id_token"},
		{"id-token", "id-token"},
		{"secret", "secret"},
		{"session_token", "session_token"},
		{"session-token", "session-token"},
		{"signature", "signature"},
		{"sig", "sig"},
		{"token", "token"},
		{"authorization", "authorization"},
		{"cookie", "cookie"},
	}
	secret := strings.Join([]string{"super", "credential", "value", "1234567890"}, "-")
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := `{"` + tc.field + `":"` + secret + `","other":"ok"}`
			output := RedactPII(input)
			if strings.Contains(output, secret) {
				t.Errorf("RedactPII(%q) leaked secret value %q via JSON field %q — output=%q; credential-leak vector",
					input, secret, tc.field, output)
			}
			want := `"` + tc.field + `":"[REDACTED]"`
			if !strings.Contains(output, want) {
				t.Errorf("RedactPII(%q) missing expected marker %q — output=%q; JSON-secret redaction contract",
					input, want, output)
			}
			// Unaffected field must pass through.
			if !strings.Contains(output, `"other":"ok"`) {
				t.Errorf("RedactPII clobbered unrelated JSON field — output=%q; scrubber-scope contract",
					output)
			}
		})
	}
}

// TestRedactPII_URLQuerySecretKeys pins the URL-query
// credential keys that can appear in auth callbacks, provider
// URLs, and artifact links. These are not JSON fields, so they
// must be covered by the query redaction pattern directly.
func TestRedactPII_URLQuerySecretKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
	}{
		{"api_key", "api_key"},
		{"api_key_encoded_underscore", "api%5Fkey"},
		{"api_key_hyphen", "api-key"},
		{"api_key_encoded_hyphen", "api%2Dkey"},
		{"apikey", "apikey"},
		{"access_token", "access_token"},
		{"access_token_encoded_underscore", "access%5Ftoken"},
		{"access_token_hyphen", "access-token"},
		{"access_token_encoded_hyphen", "access%2Dtoken"},
		{"refresh_token", "refresh_token"},
		{"refresh_token_encoded_underscore", "refresh%5Ftoken"},
		{"refresh_token_hyphen", "refresh-token"},
		{"refresh_token_encoded_hyphen", "refresh%2Dtoken"},
		{"auth_token", "auth_token"},
		{"auth_token_encoded_underscore", "auth%5Ftoken"},
		{"auth_token_hyphen", "auth-token"},
		{"auth_token_encoded_hyphen", "auth%2Dtoken"},
		{"bearer_token", "bearer_token"},
		{"bearer_token_encoded_underscore", "bearer%5Ftoken"},
		{"bearer_token_hyphen", "bearer-token"},
		{"bearer_token_encoded_hyphen", "bearer%2Dtoken"},
		{"credential", "credential"},
		{"authorization", "authorization"},
		{"cookie", "cookie"},
		{"password", "password"},
		{"passwd", "passwd"},
		{"client_secret", "client_secret"},
		{"client_secret_encoded_underscore", "client%5Fsecret"},
		{"client_secret_hyphen", "client-secret"},
		{"client_secret_encoded_hyphen", "client%2Dsecret"},
		{"client_assertion", "client_assertion"},
		{"client_assertion_encoded_underscore", "client%5Fassertion"},
		{"client_assertion_hyphen", "client-assertion"},
		{"client_assertion_encoded_hyphen", "client%2Dassertion"},
		{"id_token", "id_token"},
		{"id_token_encoded_underscore", "id%5Ftoken"},
		{"id_token_hyphen", "id-token"},
		{"id_token_encoded_hyphen", "id%2Dtoken"},
		{"secret", "secret"},
		{"session_token", "session_token"},
		{"session_token_encoded_underscore", "session%5Ftoken"},
		{"session_token_hyphen", "session-token"},
		{"session_token_encoded_hyphen", "session%2Dtoken"},
		{"signature", "signature"},
		{"sig", "sig"},
		{"token", "token"},
	}
	secret := strings.Join([]string{"query", "credential", "value", "1234567890"}, "-")
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := "https://auth.example/callback?safe=keep;" + tc.key + "=" + secret + "&next=visible"
			output := RedactPII(input)
			if strings.Contains(output, secret) {
				t.Errorf("RedactPII(%q) leaked query secret %q for key %q — output=%q",
					input, secret, tc.key, output)
			}
			want := tc.key + "=[REDACTED]"
			if !strings.Contains(output, want) {
				t.Errorf("RedactPII(%q) missing expected marker %q — output=%q",
					input, want, output)
			}
			for _, safe := range []string{"safe=keep", "next=visible"} {
				if !strings.Contains(output, safe) {
					t.Errorf("RedactPII(%q) clobbered non-secret query value %q — output=%q",
						input, safe, output)
				}
			}
		})
	}
}

func TestRedactPII_AuthorizationHeaderPreservesTrailingSecretMarkers(t *testing.T) {
	t.Parallel()

	bearerSecret := strings.Join([]string{"bearer", "credential", "value", "1234567890"}, "-")
	refreshSecret := strings.Join([]string{"refresh", "credential", "value", "1234567890"}, "-")
	cookieSecret := strings.Join([]string{"cookie", "credential", "value", "1234567890"}, "-")
	jsonSecret := strings.Join([]string{"json", "credential", "value", "1234567890"}, "-")
	jsonKey := strings.Join([]string{"api", "key"}, "_")

	input := `Authorization: Bearer ` + bearerSecret +
		` callback=https://auth.example/cb?refresh_token=` + refreshSecret +
		`&cookie=` + cookieSecret +
		` {"` + jsonKey + `":"` + jsonSecret + `","safe":"visible"}`

	output := RedactPII(input)
	for _, leaked := range []string{bearerSecret, refreshSecret, cookieSecret, jsonSecret} {
		if strings.Contains(output, leaked) {
			t.Fatalf("RedactPII leaked credential %q in output %q", leaked, output)
		}
	}
	for _, marker := range []string{
		"Bearer [REDACTED]",
		"refresh_token=[REDACTED]",
		"cookie=[REDACTED]",
		`"` + jsonKey + `":"[REDACTED]"`,
		`"safe":"visible"`,
	} {
		if !strings.Contains(output, marker) {
			t.Fatalf("RedactPII(%q) missing marker %q in output %q",
				input, marker, output)
		}
	}
}

// TestRedactPII_JSONSecrets_CaseInsensitive pins that the
// jsonSecretPattern's (?i) flag matches uppercase /
// mixed-case field names. Clients that emit
// "Authorization" (camelCase) or "API_KEY" (screaming)
// would otherwise slip through the scrubber. Existing
// tests only exercise lowercase.
func TestRedactPII_JSONSecrets_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cases := []string{
		"API_KEY", "Api_Key", "API_Key",
		"AUTHORIZATION", "Authorization",
		"Cookie", "COOKIE",
	}
	secret := strings.Join([]string{"mixed", "case", "credential", "8888"}, "-")
	for _, field := range cases {
		input := `{"` + field + `":"` + secret + `"}`
		output := RedactPII(input)
		if strings.Contains(output, secret) {
			t.Errorf("RedactPII(%q) leaked mixed-case %q secret %q — output=%q; case-insensitive-scrubber contract",
				input, field, secret, output)
		}
	}
}

// TestRedactPII_EnvSecrets_CaseInsensitive pins the
// envSecretPattern (?i) flag: operators who set env vars
// in scripts using mixed case (e.g. OpenAI_API_Key=...)
// must still get redaction. A regression removing the
// case-insensitive flag would leak every non-canonical
// casing.
func TestRedactPII_EnvSecrets_CaseInsensitive(t *testing.T) {
	t.Parallel()

	secret := strings.Join([]string{"sk", "case", "credential", "4321"}, "-")
	for _, name := range []string{
		"openai_api_key", "Openai_Api_Key",
		"anthropic_API_KEY", "Grok_Api_Key",
	} {
		input := name + "=" + secret
		output := RedactPII(input)
		if strings.Contains(output, secret) {
			t.Errorf("RedactPII(%q) leaked case-variant env-secret %q — output=%q; case-insensitive-scrubber contract",
				input, name, output)
		}
	}
}

// TestRedactPII_RedactionMarkers_ExactValues pins the 6
// exact marker strings emitted by the redaction pipeline.
// Downstream log-aggregation rules and compliance scanners
// filter on these exact literals; a rename of
// "[REDACTED_EMAIL]" to "[EMAIL_REDACTED]" would break
// every compliance-export rule that counts redacted-email
// occurrences to prove PII scrubbing was applied.
func TestRedactPII_RedactionMarkers_ExactValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		input      string
		wantMarker string
	}{
		{"bearer", "Authorization: Bearer abc.def.ghi", "Bearer [REDACTED]"},
		{"jwt", "token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", "[REDACTED_JWT]"},
		{"openai_key", "my key is sk-abcdef1234567890", "sk-[REDACTED]"},
		{"email", "contact me at user@example.com", "[REDACTED_EMAIL]"},
		{"ipv4", "from 192.0.2.10:443", "[REDACTED_IP]"},
		{"ipv6", "from [2001:db8::1]:8080", "[REDACTED_IP]"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RedactPII(tc.input)
			if !strings.Contains(got, tc.wantMarker) {
				t.Errorf("RedactPII(%q) = %q, missing exact marker %q — compliance-scanner wire contract",
					tc.input, got, tc.wantMarker)
			}
		})
	}
}
