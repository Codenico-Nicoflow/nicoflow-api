package middleware

import (
	"net"
	"net/http"
	"testing"
)

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func req(remoteAddr, xff string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP_NoXFF_StripsPort(t *testing.T) {
	r := req("203.0.113.5:54321", "")
	got := clientIP(r, nil)
	if got != "203.0.113.5" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.5")
	}
}

func TestClientIP_NoTrustedProxies_UsesXFFLeftmost(t *testing.T) {
	// No trustedProxies configured → trust XFF unconditionally (single-proxy mode).
	r := req("10.0.0.1:1234", "203.0.113.5, 10.0.0.1")
	got := clientIP(r, nil)
	if got != "203.0.113.5" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.5")
	}
}

func TestClientIP_TrustedProxy_UsesXFFLeftmost(t *testing.T) {
	// RemoteAddr is within the trusted CIDR → trust XFF.
	nets := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	r := req("10.0.0.1:1234", "203.0.113.5, 10.0.0.1")
	got := clientIP(r, nets)
	if got != "203.0.113.5" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.5")
	}
}

func TestClientIP_UntrustedRemoteAddr_IgnoresXFF(t *testing.T) {
	// RemoteAddr is NOT within the trusted CIDR → ignore spoofed XFF, use RemoteAddr.
	nets := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	r := req("1.2.3.4:9999", "192.168.1.1")
	got := clientIP(r, nets)
	if got != "1.2.3.4" {
		t.Errorf("clientIP = %q, want %q", got, "1.2.3.4")
	}
}

func TestClientIP_MultipleXFFEntries_LeftmostOnly(t *testing.T) {
	// Multiple proxies in XFF — only first (real client) used.
	r := req("10.0.0.1:1234", "203.0.113.5, 172.16.0.1, 10.0.0.1")
	got := clientIP(r, nil)
	if got != "203.0.113.5" {
		t.Errorf("clientIP = %q, want %q", got, "203.0.113.5")
	}
}

func TestParseCIDRs_SkipsInvalid(t *testing.T) {
	nets := parseCIDRs([]string{"10.0.0.0/8", "notacidr", "", "192.168.0.0/16"})
	if len(nets) != 2 {
		t.Errorf("parseCIDRs len = %d, want 2", len(nets))
	}
}
