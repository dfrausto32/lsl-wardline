package names

import (
	"strings"
	"testing"
)

func TestSlugMakesASafeShortName(t *testing.T) {
	for in, want := range map[string]string{
		"Office PC":                    "office-pc",
		"Sam's iPhone":                 "sam-s-iphone",
		"  Back   PC  ":                "back-pc",
		"Windows computer 2":           "windows-computer-2",
		"---weird---":                  "weird",
		"Büro Rechner":                 "b-ro-rechner",
		"!!!":                          "machine",
		"":                             "machine",
		strings.Repeat("a", 80):        strings.Repeat("a", 63),
		strings.Repeat("a", 62) + " b": strings.Repeat("a", 62),
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssignNumbersTakenAndReservedNames(t *testing.T) {
	taken := map[string]bool{"office-pc": true, "office-pc-2": true, "server": true}
	free := func(s string) bool { return !taken[s] }
	if got := Assign("office-pc", free); got != "office-pc-3" {
		t.Fatalf("got %q", got)
	}
	if got := Assign("new-one", free); got != "new-one" {
		t.Fatalf("got %q", got)
	}
	if got := Assign("localhost", free); got != "localhost-2" {
		t.Fatalf("a reserved name must be numbered, got %q", got)
	}
	long := strings.Repeat("a", 63)
	taken[long] = true
	got := Assign(long, free)
	if len(got) > 63 || got == long || !strings.HasSuffix(got, "-2") {
		t.Fatalf("a numbered long name must still fit in 63 characters: %q", got)
	}
}

func TestCheckRefusesBadHandTypedNames(t *testing.T) {
	for in, ok := range map[string]bool{
		"files": true, "back-pc": true, "pc2": true,
		"": false, "-x": false, "x-": false, "Office": false, "a b": false, "a.b": false,
		"server": false, "localhost": false, "wardline": false, strings.Repeat("a", 64): false,
	} {
		if err := Check(in); (err == nil) != ok {
			t.Errorf("Check(%q) = %v, want ok=%v", in, err, ok)
		}
	}
}

func TestFullAndShortForms(t *testing.T) {
	if Full("office-pc") != "office-pc.wardline.internal" {
		t.Fatal(Full("office-pc"))
	}
	if s, ok := Short("Office-PC.Wardline.Internal."); !ok || s != "office-pc" {
		t.Fatalf("%q %v", s, ok)
	}
	for _, bad := range []string{"example.com", "wardline.internal", "a.b.wardline.internal", ".wardline.internal"} {
		if _, ok := Short(bad); ok {
			t.Errorf("%q is not a machine name under the ending", bad)
		}
	}
}
