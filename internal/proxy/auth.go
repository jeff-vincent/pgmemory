package proxy

import (
	"fmt"
	"net/http"
	"strings"
)

// authMiddleware protects /api/* and the dashboard (/) with a Bearer token.
// The LLM proxy paths (/v1/*) and /health are always open — the proxy path
// is called by AI tools that cannot add custom headers.
//
// The token is accepted via:
//   - Authorization: Bearer <token>  (CLI, MCP server, API clients)
//   - ?token=<token>                 (browser dashboard access)
//
// Passing an empty token disables auth entirely (not recommended).
func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Only protect the management API and dashboard.
		if path == "/" || strings.HasPrefix(path, "/api/") {
			if !checkToken(r, token) {
				if strings.HasPrefix(path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				} else {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, tokenLoginPage)
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func checkToken(r *http.Request, expected string) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ") == expected
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t == expected
	}
	return false
}

const tokenLoginPage = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>memoryd – sign in</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0f0f0f; color: #e0e0e0;
      display: flex; align-items: center; justify-content: center;
      min-height: 100vh;
    }
    .card {
      background: #1a1a1a; border: 1px solid #2a2a2a;
      border-radius: 12px; padding: 40px 48px; width: 420px; max-width: 95vw;
    }
    h1 { font-size: 1.2rem; font-weight: 600; margin-bottom: 8px; }
    p  { font-size: 0.85rem; color: #888; margin-bottom: 24px; line-height: 1.5; }
    label { display: block; font-size: 0.8rem; color: #aaa; margin-bottom: 6px; }
    input {
      width: 100%; padding: 10px 12px; background: #111; border: 1px solid #333;
      border-radius: 8px; color: #e0e0e0; font-size: 0.85rem; font-family: monospace;
      outline: none;
    }
    input:focus { border-color: #555; }
    button {
      margin-top: 16px; width: 100%; padding: 10px;
      background: #2563eb; border: none; border-radius: 8px;
      color: #fff; font-size: 0.9rem; cursor: pointer;
    }
    button:hover { background: #1d4ed8; }
    .hint { margin-top: 20px; font-size: 0.78rem; color: #555; }
    code { background: #222; padding: 2px 6px; border-radius: 4px; font-family: monospace; }
  </style>
</head>
<body>
<div class="card">
  <h1>memoryd dashboard</h1>
  <p>Paste your local API token to open the dashboard.<br>
     You can find it in the memoryd tray menu under <strong>Open Dashboard</strong>,
     or in <code>~/.memoryd/token</code>.</p>
  <label for="tok">API token</label>
  <input id="tok" type="password" placeholder="64-character hex token" autocomplete="off" autofocus>
  <button onclick="go()">Open dashboard</button>
  <p class="hint">Tip: click <strong>Open Dashboard</strong> in the menu bar tray to skip this step.</p>
</div>
<script>
  function go() {
    var t = document.getElementById('tok').value.trim();
    if (t) window.location.href = '/?token=' + encodeURIComponent(t);
  }
  document.getElementById('tok').addEventListener('keydown', function(e) {
    if (e.key === 'Enter') go();
  });
</script>
</body>
</html>
`
