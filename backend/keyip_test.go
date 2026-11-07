package main

import (
	"net/http/httptest"
	"testing"
)

func TestKeyByClientIP(t *testing.T) {
	cases := []struct {
		name, remote, realIP, want string
	}{
		{"direct peer", "203.0.113.5:1234", "", "203.0.113.5"},
		{"direct peer ignores header", "203.0.113.5:1234", "198.51.100.1", "203.0.113.5"},
		{"loopback proxy", "127.0.0.1:1234", "198.51.100.1", "198.51.100.1"},
		{"loopback ipv6 proxy", "[::1]:1234", "2001:db8::1", "2001:db8::1"},
		{"loopback without header", "127.0.0.1:1234", "", "127.0.0.1"},
		{"loopback with garbage header", "127.0.0.1:1234", "not-an-ip", "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.realIP != "" {
			r.Header.Set("X-Real-IP", c.realIP)
		}
		if got, _ := keyByClientIP(r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
