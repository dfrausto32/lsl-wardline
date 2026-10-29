package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type spy struct {
	calls []string
	err   error
}

func (s *spy) actions() menuActions {
	return menuActions{
		installServer: func(_ context.Context, _ io.Writer) (string, error) {
			s.calls = append(s.calls, "server")
			return "http://192.168.1.50:8080", s.err
		},
		installGateway: func(_ context.Context, _ io.Writer, url, token string) error {
			s.calls = append(s.calls, "gateway "+url+" "+token)
			return s.err
		},
		installAgent: func(_ context.Context, _ io.Writer, url, token string) error {
			s.calls = append(s.calls, "agent "+url+" "+token)
			return s.err
		},
		remove: func(_ context.Context, _ io.Writer) error {
			s.calls = append(s.calls, "remove")
			return s.err
		},
	}
}

func run(t *testing.T, s *spy, input string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runMenu(context.Background(), bufio.NewReader(strings.NewReader(input)), &out, s.actions()); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestQuitDoesNothing(t *testing.T) {
	s := &spy{}
	out := run(t, s, "4\n")
	if len(s.calls) != 0 {
		t.Fatalf("calls: %v", s.calls)
	}
	for _, want := range []string{"Make this computer my server", "Add this computer to my network", "Remove Wardline from this computer"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu is missing %q:\n%s", want, out)
		}
	}
}

func TestAnUnknownChoiceAsksAgain(t *testing.T) {
	s := &spy{}
	out := run(t, s, "9\nhello\n4\n")
	if len(s.calls) != 0 || strings.Count(out, "Please type 1, 2, 3 or 4") != 2 {
		t.Fatalf("calls %v\n%s", s.calls, out)
	}
}

func TestServerThenThePastedCommand(t *testing.T) {
	s := &spy{}
	run(t, s, "1\n.\\wardline.exe gateway install --control-url http://10.0.0.9:8080 --token abc123\n")
	want := []string{"server", "gateway http://10.0.0.9:8080 abc123"}
	if strings.Join(s.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", s.calls, want)
	}
}

func TestServerThenJustTheCodeUsesTheAddressFromTheFirstStep(t *testing.T) {
	s := &spy{}
	run(t, s, "1\nabc123\n")
	want := "gateway http://192.168.1.50:8080 abc123"
	if len(s.calls) != 2 || s.calls[1] != want {
		t.Fatalf("got %v want second call %q", s.calls, want)
	}
}

func TestServerCanBeFinishedLater(t *testing.T) {
	s := &spy{}
	out := run(t, s, "1\n\n")
	if len(s.calls) != 1 {
		t.Fatalf("only the web app should be installed: %v", s.calls)
	}
	if !strings.Contains(out, "gateway install") {
		t.Fatalf("it should say how to finish later:\n%s", out)
	}
}

func TestAddThisComputerNeedsTheWholeCommandAndAsksAgainOnJunk(t *testing.T) {
	s := &spy{}
	out := run(t, s, "2\njust some words\n.\\wardline.exe agent install --control-url \"http://10.0.0.9:8080\" --token 'tok-9'\n")
	if len(s.calls) != 1 || s.calls[0] != "agent http://10.0.0.9:8080 tok-9" {
		t.Fatalf("calls %v\n%s", s.calls, out)
	}
	if !strings.Contains(out, "could not find") {
		t.Fatalf("junk should be explained:\n%s", out)
	}
}

func TestRemoveAsksFirst(t *testing.T) {
	s := &spy{}
	run(t, s, "3\nn\n")
	if len(s.calls) != 0 {
		t.Fatalf("removed without a yes: %v", s.calls)
	}
	s = &spy{}
	run(t, s, "3\ny\n")
	if len(s.calls) != 1 || s.calls[0] != "remove" {
		t.Fatalf("calls %v", s.calls)
	}
}

func TestAProblemIsExplainedAndTheMenuDoesNotCrash(t *testing.T) {
	s := &spy{err: errors.New("the code has expired")}
	out := run(t, s, "2\n--control-url http://x:1 --token t\n")
	if !strings.Contains(out, "did not work") || !strings.Contains(out, "the code has expired") {
		t.Fatalf("the problem should be shown:\n%s", out)
	}
}

func TestInputEndingEarlyStopsCleanly(t *testing.T) {
	s := &spy{}
	run(t, s, "") // nobody is there to answer
	if len(s.calls) != 0 {
		t.Fatalf("calls %v", s.calls)
	}
}

func TestParseInstallCommand(t *testing.T) {
	cases := map[string]struct {
		url, token string
		ok         bool
	}{
		`.\wardline.exe agent install --control-url http://a:8080 --token T1`:     {"http://a:8080", "T1", true},
		`wardline.exe gateway install --token T2 --control-url https://b.example`: {"https://b.example", "T2", true},
		`--control-url="http://c:1" --token="T3"`:                                 {"http://c:1", "T3", true},
		`--control-url http://d:1`:                                                {"http://d:1", "", false},
		`nothing useful`:                                                          {"", "", false},
	}
	for in, want := range cases {
		url, token, ok := parseInstallCommand(in)
		if ok != want.ok || (ok && (url != want.url || token != want.token)) {
			t.Errorf("%q: got %q %q %v, want %q %q %v", in, url, token, ok, want.url, want.token, want.ok)
		}
	}
}
