package main

import "testing"

func TestColorRefSwapsRedAndBlue(t *testing.T) {
	if got := colorRef(0xf5f0e1); got != 0xe1f0f5 {
		t.Fatalf("colorRef(0xf5f0e1) = %#x, want 0xe1f0f5", got)
	}
	if got := colorRef(0x141414); got != 0x141414 {
		t.Fatalf("grey should not change, got %#x", got)
	}
}

func TestThemeColorsFollowThePage(t *testing.T) {
	light, dark := themeColors(false), themeColors(true)
	if light.caption != colorRef(0xf5f0e1) || dark.caption != colorRef(0x141414) {
		t.Fatalf("title bar should match the page background: light %#x dark %#x", light.caption, dark.caption)
	}
	if light.text == light.caption || dark.text == dark.caption {
		t.Fatal("title text must differ from the title bar")
	}
}
