package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ddnsRecord struct {
	ip  string
	err string
}

func newDDNS(t *testing.T, reply string, status int) (*ddnsUpdater, *[]ddnsRecord, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RawQuery)
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	var recs []ddnsRecord
	u := &ddnsUpdater{
		base:     srv.URL,
		settings: func(context.Context) (string, string, bool, error) { return "laney-network", "tok-123", true, nil },
		record: func(_ context.Context, ip string, err error) {
			r := ddnsRecord{ip: ip}
			if err != nil {
				r.err = err.Error()
			}
			recs = append(recs, r)
		},
	}
	return u, &recs, &seen
}

func TestTheNameIsUpdatedAndTheAddressRecorded(t *testing.T) {
	u, recs, seen := newDDNS(t, "OK\n203.0.113.7\n\nUPDATED", 200)
	u.once(context.Background())
	if len(*seen) != 1 || !strings.Contains((*seen)[0], "domains=laney-network") || !strings.Contains((*seen)[0], "token=tok-123") {
		t.Fatalf("request: %v", *seen)
	}
	if len(*recs) != 1 || (*recs)[0].ip != "203.0.113.7" || (*recs)[0].err != "" {
		t.Fatalf("recorded: %+v", *recs)
	}
}

func TestARefusedNameOrTokenIsExplainedWithoutTheToken(t *testing.T) {
	u, recs, _ := newDDNS(t, "KO", 200)
	u.once(context.Background())
	if len(*recs) != 1 || !strings.Contains((*recs)[0].err, "did not accept") || strings.Contains((*recs)[0].err, "tok-123") {
		t.Fatalf("%+v", *recs)
	}
}

func TestAnUnreachableServiceIsRecordedAsAProblemNotACrash(t *testing.T) {
	u, recs, _ := newDDNS(t, "", 200)
	u.base = "http://127.0.0.1:1" // nothing listens here
	u.once(context.Background())
	if len(*recs) != 1 || (*recs)[0].err == "" || strings.Contains((*recs)[0].err, "tok-123") {
		t.Fatalf("%+v", *recs)
	}
}

func TestNothingIsSentWhenNoNameWasChosen(t *testing.T) {
	u, recs, seen := newDDNS(t, "OK", 200)
	u.settings = func(context.Context) (string, string, bool, error) { return "", "", false, nil }
	u.once(context.Background())
	if len(*seen) != 0 || len(*recs) != 0 {
		t.Fatalf("seen %v recorded %v", *seen, *recs)
	}
}
