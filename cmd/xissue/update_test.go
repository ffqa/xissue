package main

import "testing"

func TestParseBrewServices(t *testing.T) {
	out := "Name    Status  User  File\nxissue  started admin ~/Library/LaunchAgents/homebrew.mxcl.xissue.plist\n"
	if !parseBrewServices(out, "xissue") {
		t.Fatal("started service not detected")
	}
	if parseBrewServices(out, "other") {
		t.Fatal("unrelated service reported as running")
	}
	if parseBrewServices("xissue  stopped admin -\n", "xissue") {
		t.Fatal("stopped service reported as running")
	}
}
