package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestServerLoadsSharedCursorCredentialsWithoutExposingThem(t *testing.T) {
	for _, override := range []string{"", "environment-test-secret"} {
		t.Run(override, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv(cursorKeyEnv, override)
			const saved = "saved-test-secret"
			if err := core.SetProviderAPIKey("cursor", saved); err != nil {
				t.Fatal(err)
			}
			want := saved
			if override != "" {
				want = override
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != want || password != "" {
					t.Error("cloud request did not use the resolved provider key")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE","repos":[]}]}`))
			}))
			defer upstream.Close()
			srv, err := newServer(":0", testProvider{}, testToken, testSalt, nil, upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			srv.ln.Close()
			defer srv.cloud.Close()
			local := httptest.NewServer(srv.srv.Handler)
			defer local.Close()
			out := getAgents(t, local.URL)
			if len(out.Errors) != 0 || len(out.Agents) != 1 || out.Agents[0].Source != "cloud" {
				t.Fatalf("saved credentials did not enable cloud lookup: %+v", out)
			}
			resp := authedGet(t, local.URL+"/api/config")
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("config status=%d, error=%v", resp.StatusCode, err)
			}
			if bytes.Contains(data, []byte(saved)) || override != "" && bytes.Contains(data, []byte(override)) || bytes.Contains(data, []byte("api_key")) {
				t.Fatal("provider credentials crossed the configuration API boundary")
			}
		})
	}
}

func TestMalformedProviderCredentialsKeepLocalAgents(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(cursorKeyEnv, "")
	if err := core.SetProviderAPIKey("cursor", "test-secret"); err != nil {
		t.Fatal(err)
	}
	const invalid = `{"providers":{"cursor":{"api_key":["do-not-disclose-this"]}}}`
	if err := os.WriteFile(core.CredentialsPath(), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := fixedSessionProvider{sessions: []*model.Session{
		{SessionID: "ide-1", Agent: "cursor", CWD: t.TempDir(), LastActivity: time.Now(), Status: model.StatusThinking},
	}}
	srv, err := New(":0", provider, testToken, testSalt, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.ln.Close()
	defer srv.cloud.Close()
	if err := srv.manager.Reload(); err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(srv.srv.Handler)
	defer local.Close()
	out := getAgents(t, local.URL)
	if len(out.Agents) != 1 || out.Agents[0].ID != "ide-1" {
		t.Fatalf("local agents were discarded: %+v", out.Agents)
	}
	if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], "credentials.json") || strings.Contains(out.Errors[0], "do-not-disclose-this") {
		t.Fatalf("expected a credential configuration error without secret data: %v", out.Errors)
	}
}
