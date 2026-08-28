package webhook

import "testing"

func TestSign(t *testing.T) {
	got := Sign([]byte("secret"), 1700000000, "delivery-id", []byte(`{"id":1}`))
	want := "v1=ed0c732f9901e93553032aed58373b7990604734f9bc8dd89717d5e83e86e7bc"
	if got != want {
		t.Fatalf("signature = %q", got)
	}
}
