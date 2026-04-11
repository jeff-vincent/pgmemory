package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/memory-daemon/memoryd/internal/pipeline"
	"github.com/memory-daemon/memoryd/internal/redact"
)

// scenarioRedactAPIKey verifies that API keys embedded in content are scrubbed
// before the memory is stored. Tests both the redact package directly and the
// write pipeline's end-to-end redaction-before-storage guarantee.
func scenarioRedactAPIKey(ctx context.Context) error {
	// Direct redact test.
	raw := `To call the embeddings API use this key: api_key=sk-ant-api03-AAABBBCCC111222333444555666777888999000aaabbbccc in your Authorization header.`
	cleaned := redact.Clean(raw)

	if strings.Contains(cleaned, "sk-ant-api03") {
		return fmt.Errorf("redact.Clean did not remove API key from content")
	}
	if !strings.Contains(cleaned, "[REDACTED]") {
		return fmt.Errorf("redact.Clean removed key but added no [REDACTED] marker: %q", cleaned)
	}

	// End-to-end: store content with a secret, verify the stored memory is clean.
	st := newMemStore()
	emb := newHashEmbedder(128)
	wp := pipeline.NewWritePipeline(emb, st)

	wp.ProcessFiltered(raw, "validate-redact", nil)

	memories, err := st.List(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	for _, m := range memories {
		if strings.Contains(m.Content, "sk-ant-api03") {
			return fmt.Errorf("stored memory still contains raw API key: %q", m.Content[:min(80, len(m.Content))])
		}
	}

	if *verbose {
		fmt.Printf("\n    raw=%d chars cleaned=%d chars stored_memories=%d\n", len(raw), len(cleaned), len(memories))
	}
	return nil
}

// scenarioRedactConnectionString verifies that database URIs with embedded
// credentials are scrubbed. This is the most common credential form in
// real developer sessions.
func scenarioRedactConnectionString(ctx context.Context) error {
	cases := []struct {
		input   string
		pattern string // must NOT appear in output
	}{
		{
			input:   "Connect with: mongodb+srv://dbuser:s3cr3tPassw0rd@cluster0.mongodb.net/memoryd?retryWrites=true",
			pattern: "s3cr3tPassw0rd",
		},
		{
			input:   "Redis URL: redis://default:hunter2@redis.example.com:6379/0",
			pattern: "hunter2",
		},
		{
			input:   "Postgres DSN: postgres://app:correct-horse-battery-staple@db.internal:5432/prod",
			pattern: "correct-horse-battery-staple",
		},
	}

	for _, tc := range cases {
		cleaned := redact.Clean(tc.input)
		if strings.Contains(cleaned, tc.pattern) {
			return fmt.Errorf("credential %q not redacted in: %q", tc.pattern, cleaned)
		}
		if !strings.Contains(cleaned, "[REDACTED") {
			return fmt.Errorf("no REDACTED marker in output for input containing %q: %q", tc.pattern, cleaned)
		}
	}

	if *verbose {
		fmt.Printf("\n    %d connection string patterns verified\n", len(cases))
	}
	return nil
}

// scenarioRedactPreservesCode verifies that normal Go code without secrets
// passes through the redactor unchanged. The redactor should only remove
// actual credentials, not benign field names or struct definitions.
func scenarioRedactPreservesCode(ctx context.Context) error {
	code := `// Config holds daemon configuration.
type Config struct {
	Port            int    ` + "`yaml:\"port\"`" + `
	APIClient       string ` + "`yaml:\"api_client\"`" + `
	MongoDBDatabase string ` + "`yaml:\"mongodb_database\"`" + `
	ModelPath       string ` + "`yaml:\"model_path\"`" + `
}

func (c *Config) Validate() error {
	if c.Port == 0 {
		return fmt.Errorf("port is required")
	}
	return nil
}`

	cleaned := redact.Clean(code)

	// Key struct fields must survive.
	mustSurvive := []string{"type Config struct", "Port", "APIClient", "MongoDBDatabase", "Validate"}
	for _, s := range mustSurvive {
		if !strings.Contains(cleaned, s) {
			return fmt.Errorf("redactor clobbered non-secret code element %q\ncleaned: %s", s, cleaned)
		}
	}

	if *verbose {
		fmt.Printf("\n    code preserved intact (%d chars in, %d chars out)\n", len(code), len(cleaned))
	}
	return nil
}

// scenarioRedactJWT verifies that JWT-format strings (three base64url segments
// separated by dots) are detected and scrubbed. JWTs are a common auth mechanism
// that frequently leak into conversation context.
func scenarioRedactJWT(ctx context.Context) error {
	// A well-formed JWT structure (fake, for testing the pattern).
	// Header.Payload.Signature — all base64url encoded.
	raw := `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyMTIzIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`

	cleaned := redact.Clean(raw)

	// The JWT payload should not survive.
	if strings.Contains(cleaned, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		return fmt.Errorf("JWT header not redacted: %q", cleaned)
	}

	// The "Authorization: Bearer" prefix may survive (it's not a secret).
	// What matters is the token itself is gone.
	if strings.Count(cleaned, "[REDACTED") == 0 {
		return fmt.Errorf("no REDACTED marker in output: %q", cleaned)
	}

	if *verbose {
		fmt.Printf("\n    JWT redacted: %q\n", cleaned)
	}
	return nil
}

// scenarioRedactMultilineSecret verifies that multi-line secrets like PEM private
// keys are fully scrubbed even when they span many lines of content.
func scenarioRedactMultilineSecret(ctx context.Context) error {
	raw := `To configure TLS for the memoryd server, add the following private key to your config:

-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0Z3VS5JJcds3xHn/ygWep4PAtEsHAD6RNHQgTU0Wd0p5KDJ
sxFfLiSG0VmRi3mmrBiCUkgmN1HQvkT5kIHxTVkFsWJBNGKMYFSJD2gUCBgz3fH
ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ012345
-----END RSA PRIVATE KEY-----

Use this for mTLS between the proxy and upstream.`

	cleaned := redact.Clean(raw)

	if strings.Contains(cleaned, "BEGIN RSA PRIVATE KEY") {
		return fmt.Errorf("PEM private key header not redacted")
	}
	if strings.Contains(cleaned, "MIIEowIBAAKCAQEA") {
		return fmt.Errorf("PEM key body not redacted")
	}
	if !strings.Contains(cleaned, "[REDACTED") {
		return fmt.Errorf("no REDACTED marker after PEM scrubbing: %q", cleaned)
	}

	if *verbose {
		fmt.Printf("\n    PEM key scrubbed: %q\n", cleaned[:min(120, len(cleaned))])
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
