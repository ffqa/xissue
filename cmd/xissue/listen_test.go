package main

import "testing"

func TestIsLoopbackListen(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:7789", true},
		{"localhost:7789", true},
		{"[::1]:7789", true},
		{"0.0.0.0:7789", false},
		{":7789", false},
		{"192.168.1.10:7789", false},
		{"example.com:7789", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackListen(c.addr); got != c.want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
