package server

import "testing"

func TestEndpointAllowed(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://mt1.push.services.mozilla.com/ch/abc",
		"https://web.push.apple.com/abc",
		"http://127.0.0.1:9911/push", // local test receiver
	}
	bad := []string{
		"",
		"http://evil.example/push",
		"https://evil.example/push",
		"https://fcm.googleapis.com.evil.example/push",
		"ftp://fcm.googleapis.com/x",
		"file:///etc/passwd",
	}
	for _, ep := range ok {
		if !endpointAllowed(ep) {
			t.Errorf("endpointAllowed(%q) = false, want true", ep)
		}
	}
	for _, ep := range bad {
		if endpointAllowed(ep) {
			t.Errorf("endpointAllowed(%q) = true, want false", ep)
		}
	}
}
