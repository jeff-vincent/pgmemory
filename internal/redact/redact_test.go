package redact

import (
	"strings"
	"testing"
)

func TestClean_AWSKeys(t *testing.T) {
	input := "Use key AKIAIOSFODNN7EXAMPLE and secret"
	out := Clean(input)
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS access key not redacted")
	}
}

func TestClean_GenericAPIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		bad   string
	}{
		{"api_key=", "api_key=sk_1234567890abcdefghij", "sk_1234567890abcdefghij"},
		{"api-key:", "api-key: sk_1234567890abcdefghij", "sk_1234567890abcdefghij"},
		{"token=", "auth_token=eyAbcDefGh1234567890xx", "eyAbcDefGh1234567890xx"},
		{"bearer", "Authorization: Bearer eyTokenHere1234567890abc", "eyTokenHere1234567890abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Clean(tt.input)
			if strings.Contains(out, tt.bad) {
				t.Errorf("secret not redacted in %q -> %q", tt.input, out)
			}
		})
	}
}

func TestClean_GitHubTokens(t *testing.T) {
	input := "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl"
	out := Clean(input)
	if strings.Contains(out, "ghp_") {
		t.Errorf("GitHub token not redacted: %s", out)
	}
}

func TestClean_ConnectionStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"mongodb", "mongodb+srv://user:s3cretP4ss@cluster.mongodb.net/db"},
		{"postgres", "postgres://admin:hunter2@db.example.com:5432/mydb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Clean(tt.input)
			if !strings.Contains(out, "[REDACTED:CONNECTION_STRING]") {
				t.Errorf("connection string not redacted: %s", out)
			}
		})
	}
}

func TestClean_PrivateKeys(t *testing.T) {
	input := "some text\n-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJBALRIMi7vJR1P\n-----END RSA PRIVATE KEY-----\nmore text"
	out := Clean(input)
	if strings.Contains(out, "MIIBog") {
		t.Error("private key not redacted")
	}
	if !strings.Contains(out, "[REDACTED:PRIVATE_KEY]") {
		t.Error("missing PRIVATE_KEY marker")
	}
}

func TestClean_JWT(t *testing.T) {
	input := "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U end"
	out := Clean(input)
	if !strings.Contains(out, "[REDACTED:JWT]") {
		t.Errorf("JWT not redacted: %s", out)
	}
}

func TestClean_Emails(t *testing.T) {
	input := "Contact jeff@example.com for help"
	out := Clean(input)
	if strings.Contains(out, "jeff@example.com") {
		t.Error("email not redacted")
	}
}

func TestClean_PasswordKeyValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		bad   string
	}{
		{"password=", "db_password=hunter2", "hunter2"},
		{"password:", "password: supersecret123", "supersecret123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Clean(tt.input)
			if strings.Contains(out, tt.bad) {
				t.Errorf("secret not redacted in %q -> %q", tt.input, out)
			}
		})
	}
}

func TestClean_PreservesNormalText(t *testing.T) {
	input := "This is a normal documentation page about setting up a REST API."
	out := Clean(input)
	if out != input {
		t.Errorf("normal text was modified:\n  in:  %s\n  out: %s", input, out)
	}
}

func TestClean_SlackTokens(t *testing.T) {
	input := "SLACK_TOKEN=xoxb-1234567890-abcdefghij"
	out := Clean(input)
	if strings.Contains(out, "xoxb-") {
		t.Errorf("Slack token not redacted: %s", out)
	}
}

func TestClean_StripeKeys(t *testing.T) {
	input := "stripe key sk_live_ABCDEFGHIJKLMNOPQRSTx"
	out := Clean(input)
	if strings.Contains(out, "sk_live_") {
		t.Errorf("Stripe key not redacted: %s", out)
	}
}
