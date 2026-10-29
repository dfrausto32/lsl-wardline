package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "k-123"

type installerRig struct {
	calls  []string
	err    error
	quit   bool
	opened []string
	srv    *httptest.Server
}

func newInstallerRig(t *testing.T) *installerRig {
	r := &installerRig{}
	a := installActions{
		installServer: func(_ context.Context, out io.Writer) (string, error) {
			r.calls = append(r.calls, "server")
			io.WriteString(out, "The web app is running.\n")
			return "http://192.168.1.50:8080", r.err
		},
		installGateway: func(context.Context, io.Writer, string, string) error { return nil },
		installAgent: func(_ context.Context, _ io.Writer, url, token string) error {
			r.calls = append(r.calls, "agent "+url+" "+token)
			return r.err
		},
		remove: func(context.Context, io.Writer) error { r.calls = append(r.calls, "remove"); return r.err },
	}
	h := installerHandler(testKey, "http://localhost:8080", a, func() { r.quit = true }, func(url string) { r.opened = append(r.opened, url) })
	r.srv = httptest.NewServer(h)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *installerRig) post(t *testing.T, path, key, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", r.srv.URL+path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-Wardline-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestThePageIsOnlyServedWithTheKey(t *testing.T) {
	r := newInstallerRig(t)
	for url, want := range map[string]int{r.srv.URL + "/": 403, r.srv.URL + "/?k=wrong": 403, r.srv.URL + "/?k=" + testKey: 200} {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: got %d want %d", url, resp.StatusCode, want)
		}
		if want == 200 {
			for _, text := range []string{"Make this computer my server", "Add this computer to my network", "Remove Wardline from this computer"} {
				if !strings.Contains(string(body), text) {
					t.Errorf("the page is missing %q", text)
				}
			}
		}
	}
}

func TestActionsNeedTheKeyInAHeader(t *testing.T) {
	r := newInstallerRig(t)
	if code, _ := r.post(t, "/api/server", "", "{}"); code != 403 {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := r.post(t, "/api/server", "wrong", "{}"); code != 403 {
		t.Fatalf("wrong key: %d", code)
	}
	if len(r.calls) != 0 {
		t.Fatalf("nothing should have run: %v", r.calls)
	}
}

func TestOtherWebsitesCannotReachItByAnotherName(t *testing.T) {
	r := newInstallerRig(t)
	req, _ := http.NewRequest("POST", r.srv.URL+"/api/server", strings.NewReader("{}"))
	req.Host = "evil.example.com"
	req.Header.Set("X-Wardline-Key", testKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || len(r.calls) != 0 {
		t.Fatalf("status %d, calls %v", resp.StatusCode, r.calls)
	}
}

func TestMakingTheServerReturnsWhereToGoNext(t *testing.T) {
	r := newInstallerRig(t)
	code, out := r.post(t, "/api/server", testKey, "{}")
	if code != 200 || out["ok"] != true || out["open"] != "http://localhost:8080" {
		t.Fatalf("%d %v", code, out)
	}
	if !strings.Contains(out["message"].(string), "web app is running") {
		t.Fatalf("message: %v", out["message"])
	}
}

func TestAddingThisComputerParsesThePastedCommand(t *testing.T) {
	r := newInstallerRig(t)
	body, _ := json.Marshal(map[string]string{"command": `.\wardline.exe agent install --control-url http://10.0.0.9:8080 --token T9`})
	code, out := r.post(t, "/api/computer", testKey, string(body))
	if code != 200 || out["ok"] != true || len(r.calls) != 1 || r.calls[0] != "agent http://10.0.0.9:8080 T9" {
		t.Fatalf("%d %v %v", code, out, r.calls)
	}
}

func TestAJunkCommandIsExplainedAndNothingRuns(t *testing.T) {
	r := newInstallerRig(t)
	code, out := r.post(t, "/api/computer", testKey, `{"command":"hello"}`)
	if code != 200 || out["ok"] != false || !strings.Contains(out["message"].(string), "could not find") || len(r.calls) != 0 {
		t.Fatalf("%d %v %v", code, out, r.calls)
	}
}

func TestAFailureIsShownInPlainWords(t *testing.T) {
	r := newInstallerRig(t)
	r.err = errors.New("the code has expired")
	_, out := r.post(t, "/api/server", testKey, "{}")
	if out["ok"] != false || !strings.Contains(out["message"].(string), "the code has expired") {
		t.Fatalf("%v", out)
	}
}

func TestRemoveAndQuit(t *testing.T) {
	r := newInstallerRig(t)
	if _, out := r.post(t, "/api/remove", testKey, "{}"); out["ok"] != true || len(r.calls) != 1 || r.calls[0] != "remove" {
		t.Fatalf("%v %v", out, r.calls)
	}
	r.post(t, "/api/quit", testKey, "{}")
	if !r.quit {
		t.Fatal("quit was not called")
	}
}

// ---- opening the web app in the person's own browser, and the desktop shortcut ----

func TestAfterTheServerIsInstalledTheWebAppOpensInTheBrowser(t *testing.T) {
	r := newInstallerRig(t)
	if _, out := r.post(t, "/api/server", testKey, "{}"); out["ok"] != true {
		t.Fatalf("%v", out)
	}
	if len(r.opened) != 1 || r.opened[0] != "http://localhost:8080" {
		t.Fatalf("opened %v", r.opened)
	}
}

func TestTheBrowserIsNotOpenedWhenTheInstallFailed(t *testing.T) {
	r := newInstallerRig(t)
	r.err = errors.New("no")
	r.post(t, "/api/server", testKey, "{}")
	if len(r.opened) != 0 {
		t.Fatalf("opened %v", r.opened)
	}
}

func TestTheWebAppCanBeOpenedAgain(t *testing.T) {
	r := newInstallerRig(t)
	if code, out := r.post(t, "/api/open", testKey, "{}"); code != 200 || out["ok"] != true || len(r.opened) != 1 {
		t.Fatalf("%d %v %v", code, out, r.opened)
	}
	if code, _ := r.post(t, "/api/open", "", "{}"); code != 403 {
		t.Fatalf("the key is needed: %d", code)
	}
}
