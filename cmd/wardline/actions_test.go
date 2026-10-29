package main

import "testing"

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
