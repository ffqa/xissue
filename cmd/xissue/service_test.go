package main

import "testing"

func TestHasNewListener(t *testing.T) {
	if hasNewListener([]int{100}, []int{100}) {
		t.Fatal("same listener PID must not count as restarted")
	}
	if !hasNewListener([]int{100}, []int{100, 200}) {
		t.Fatal("a new listener PID must count as restarted")
	}
	if !hasNewListener(nil, []int{200}) {
		t.Fatal("first listener must count as restarted")
	}
	if hasNewListener([]int{100}, nil) {
		t.Fatal("no listener must not count as restarted")
	}
}

func TestLaunchdOutputHasPID(t *testing.T) {
	if launchdOutputHasPID("state = running\n\tpid = 123\n") != true {
		t.Fatal("running launchd job should expose a PID")
	}
	if launchdOutputHasPID("state = not running\n") {
		t.Fatal("stopped launchd job must not count as running")
	}
}
