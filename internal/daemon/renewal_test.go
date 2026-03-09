package daemon

import (
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"testing"
	"time"
)

// makeJWT builds a fake JWT with the given exp claim.
// The signature part is a dummy — jwtExpiry does not verify signatures.
func makeJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{"sub": "test", "exp": exp})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadB64 + ".fakesig"
}

// ---------------------------------------------------------------------------
// jwtExpiry tests
// ---------------------------------------------------------------------------

func TestJwtExpiry_ValidToken(t *testing.T) {
	exp := time.Now().Add(24 * time.Hour).Unix()
	got := jwtExpiry(makeJWT(exp))
	if got.Unix() != exp {
		t.Errorf("expected exp=%d, got %d", exp, got.Unix())
	}
}

func TestJwtExpiry_ExpiredToken(t *testing.T) {
	exp := time.Now().Add(-1 * time.Hour).Unix()
	got := jwtExpiry(makeJWT(exp))
	if got.Unix() != exp {
		t.Errorf("expected exp=%d, got %d", exp, got.Unix())
	}
}

func TestJwtExpiry_NoExpClaim(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`))
	token := header + "." + payload + ".sig"
	got := jwtExpiry(token)
	if !got.IsZero() {
		t.Errorf("expected zero time for token without exp, got %v", got)
	}
}

func TestJwtExpiry_MalformedTokens(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"no dots", "nodots"},
		{"one dot", "one.dot"},
		{"invalid base64 payload", "header.!!!invalid!!!.sig"},
		{"payload not json", "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jwtExpiry(tt.token)
			if !got.IsZero() {
				t.Errorf("expected zero time for malformed token %q, got %v", tt.token, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Backoff cap and jitter tests
// ---------------------------------------------------------------------------

func TestMaxHeartbeatBackoff_IsBelow30s(t *testing.T) {
	// Hub marks a node offline after 30s of missed heartbeats.
	// maxHeartbeatBackoff must be well below that threshold.
	if maxHeartbeatBackoff >= 30*time.Second {
		t.Errorf("maxHeartbeatBackoff (%v) must be < 30s (hub offline threshold)", maxHeartbeatBackoff)
	}
}

func TestBackoffJitter_NeverExceedsCap(t *testing.T) {
	// Simulate the backoff logic from heartbeatLoop and verify that the
	// resulting interval never exceeds maxHeartbeatBackoff.
	baseInterval := 10 * time.Second
	currentInterval := baseInterval

	for i := 0; i < 100; i++ {
		// Simulate failure: exponential backoff with jitter (same logic as heartbeatLoop)
		currentInterval = currentInterval * 2
		if currentInterval > maxHeartbeatBackoff {
			currentInterval = maxHeartbeatBackoff
		}
		// Subtract up to 25% jitter (keeps interval ≤ cap)
		currentInterval -= time.Duration(rand.Int63n(int64(currentInterval / 4)))

		if currentInterval > maxHeartbeatBackoff {
			t.Fatalf("iteration %d: interval %v exceeds maxHeartbeatBackoff %v", i, currentInterval, maxHeartbeatBackoff)
		}
		if currentInterval <= 0 {
			t.Fatalf("iteration %d: interval must be positive, got %v", i, currentInterval)
		}
	}
}

func TestBackoffJitter_EventuallyReachesCapRange(t *testing.T) {
	// After enough failures the interval should be in [75% of cap, cap].
	baseInterval := 10 * time.Second
	currentInterval := baseInterval

	for i := 0; i < 20; i++ {
		currentInterval = currentInterval * 2
		if currentInterval > maxHeartbeatBackoff {
			currentInterval = maxHeartbeatBackoff
		}
		currentInterval -= time.Duration(rand.Int63n(int64(currentInterval / 4)))
	}

	floor := maxHeartbeatBackoff * 3 / 4
	if currentInterval < floor {
		t.Errorf("after many failures, interval %v should be >= %v (75%% of cap)", currentInterval, floor)
	}
}

// ---------------------------------------------------------------------------
// Token renewal threshold tests
// ---------------------------------------------------------------------------

func TestTokenRenewalThreshold_Is4Hours(t *testing.T) {
	if tokenRenewalThreshold != 4*time.Hour {
		t.Errorf("expected tokenRenewalThreshold=4h, got %v", tokenRenewalThreshold)
	}
}

func TestMaybeRenewToken_SkipsWhenNoExpiry(t *testing.T) {
	// When tokenExpiresAt is zero, maybeRenewToken should be a no-op
	// (no panic, no autoLogin call). We verify it doesn't panic.
	d := &Daemon{}
	d.maybeRenewToken(nil) // nil ctx is fine — should return before using it
}

func TestMaybeRenewToken_SkipsWhenTokenFresh(t *testing.T) {
	// Token expiring in 20 hours — well above 4h threshold.
	// maybeRenewToken should not attempt autoLogin (which would fail with nil fields).
	d := &Daemon{}
	d.tokenExpiresAt = time.Now().Add(20 * time.Hour)
	d.maybeRenewToken(nil) // should return early, no panic
}

// ---------------------------------------------------------------------------
// Integration: backoff never causes hub to declare node offline
// ---------------------------------------------------------------------------

func TestBackoff_IndividualIntervalBelowHubTimeout(t *testing.T) {
	// The hub marks a node offline after 30s of missed heartbeats.
	// If the hub is completely unreachable, eventual offline status is expected.
	// What matters is that each INDIVIDUAL retry interval is well below the
	// hub timeout — so once the hub recovers, the node sends a heartbeat
	// quickly and recovers, rather than staying silent for up to 5 minutes
	// (the old maxHeartbeatBackoff).
	hubTimeout := 30 * time.Second
	baseInterval := 10 * time.Second
	currentInterval := baseInterval

	for i := 0; i < 50; i++ {
		// Simulate failure: apply backoff (same logic as heartbeatLoop)
		currentInterval = currentInterval * 2
		if currentInterval > maxHeartbeatBackoff {
			currentInterval = maxHeartbeatBackoff
		}
		currentInterval -= time.Duration(rand.Int63n(int64(currentInterval / 4)))

		if currentInterval >= hubTimeout {
			t.Fatalf("iteration %d: individual interval %v >= hub timeout %v — "+
				"node would stay silent too long after hub recovery",
				i, currentInterval, hubTimeout)
		}
	}
}

func TestBackoff_OldCapWouldExceedHubTimeout(t *testing.T) {
	// Regression guard: the old cap was 5 minutes. Verify that would exceed
	// the hub's 30s offline threshold, confirming the fix matters.
	oldCap := 5 * time.Minute
	hubTimeout := 30 * time.Second
	if oldCap <= hubTimeout {
		t.Fatal("test premise broken: old cap should exceed hub timeout")
	}
	if maxHeartbeatBackoff >= hubTimeout {
		t.Errorf("new maxHeartbeatBackoff (%v) must be < hub timeout (%v)",
			maxHeartbeatBackoff, hubTimeout)
	}
}
