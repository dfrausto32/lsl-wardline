package api

import (
	"strings"
	"testing"
)

// There are no people in Wardline, only machines. A machine has a name, and that is all.

type machine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}

func TestAMachineIsAddedWithJustAKindAndAName(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var out struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "ios", "name": "Maria's phone"}, &out, 201)
	if out.Device.Name != "Maria's phone" || out.Device.Platform != "ios" {
		t.Fatalf("%+v", out.Device)
	}
}

func TestWithoutANameTheMachineGetsTheNameOfItsKindAndNumbersFollow(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var a, b struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows"}, &a, 201)
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows"}, &b, 201)
	if a.Device.Name != "Windows computer" || b.Device.Name != "Windows computer 2" {
		t.Fatalf("%q then %q", a.Device.Name, b.Device.Name)
	}
}

func TestAComputerThatSetsItselfUpAlsoNeedsNoPerson(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var out struct {
		Device machine `json:"device"`
		Token  string  `json:"token"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "linux", "agent": true, "name": "Build server"}, &out, 201)
	if out.Device.Name != "Build server" || out.Token == "" {
		t.Fatalf("%+v", out)
	}
}

func TestHomeListsMachinesAndRemovedOnesLeaveNothingBehind(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var a, b struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows", "name": "Office PC"}, &a, 201)
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "macos", "name": "Studio Mac"}, &b, 201)
	var home struct {
		Devices []machine `json:"devices"`
	}
	h.mustCall("GET", "/api/home", nil, &home, 200)
	if len(home.Devices) != 2 || home.Devices[0].Name != "Office PC" || home.Devices[1].Name != "Studio Mac" {
		t.Fatalf("%+v", home.Devices)
	}
	h.mustCall("DELETE", "/api/devices/"+a.Device.ID, nil, nil, 200)
	h.mustCall("GET", "/api/home", nil, &home, 200)
	if len(home.Devices) != 1 || home.Devices[0].Name != "Studio Mac" {
		t.Fatalf("a removed machine must vanish from Home: %+v", home.Devices)
	}
}

func TestRenamingAMachine(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var a struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows", "name": "Home PC's Windows computer"}, &a, 201)
	var out machine
	h.mustCall("PATCH", "/api/devices/"+a.Device.ID, map[string]string{"name": "Office PC"}, &out, 200)
	if out.Name != "Office PC" {
		t.Fatalf("%+v", out)
	}
	var d machine
	h.mustCall("GET", "/api/devices/"+a.Device.ID, nil, &d, 200)
	if d.Name != "Office PC" {
		t.Fatalf("%+v", d)
	}
	var text string
	if err := h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='device.renamed'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Home PC's Windows computer") || !strings.Contains(text, "Office PC") {
		t.Fatalf("activity says %q", text)
	}
}

func TestBadAndDuplicateNamesAreRefusedInPlainWords(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var a, b struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows", "name": "Office PC"}, &a, 201)
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "macos", "name": "Studio Mac"}, &b, 201)
	for _, bad := range []string{"", "   ", strings.Repeat("x", 61), "Studio Mac"} {
		var e map[string]string
		h.mustCall("PATCH", "/api/devices/"+a.Device.ID, map[string]string{"name": bad}, &e, 400)
		if e["error"] == "" {
			t.Errorf("%q: no message", bad)
		}
	}
	h.mustCall("PATCH", "/api/devices/nonexistent", map[string]string{"name": "x"}, nil, 404)
	h.hc.Jar = nil
	h.mustCall("PATCH", "/api/devices/"+a.Device.ID, map[string]string{"name": "Y"}, nil, 401)
}

// Anything made before this change, with a person, keeps working and keeps its names.
func TestOlderDevicesWithAPersonStillWork(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	var out struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"person_id": sam, "platform": "ios"}, &out, 201)
	if out.Device.Name != "Sam's iPhone" {
		t.Fatalf("%q", out.Device.Name)
	}
}
