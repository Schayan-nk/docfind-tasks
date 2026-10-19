package main

import "testing"

func TestPasskeysNeedAHostNameOverHTTPS(t *testing.T) {
	for _, c := range []struct {
		rpid, origins string
		on, ok        bool
	}{
		{"", "", false, true}, // off, and that is fine
		{"tasks.example.com", "", true, true},
		{"tasks.example.com", "https://tasks.example.com:8443", true, true},
		{"192.168.77.13", "", false, false}, // browsers refuse an IP
		{"docfind", "", false, false},
		{"tasks.example.com", "http://tasks.example.com", false, false},
		{"tasks.example.com", "https://evil.example", false, false},
		{"tasks.example.com", "https://tasks.example.com.evil.example", false, false},
	} {
		t.Setenv("DOCFIND_PASSKEY_RPID", c.rpid)
		t.Setenv("DOCFIND_PASSKEY_ORIGINS", c.origins)
		rp, err := passkeyConfig()
		if (err == nil) != c.ok || (rp != nil) != c.on {
			t.Errorf("%q %q: rp=%v err=%v, want on=%v ok=%v", c.rpid, c.origins, rp, err, c.on, c.ok)
		}
	}
}
