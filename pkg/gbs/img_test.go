package gbs

import "testing"

func TestReceiveNativeSnapshot(t *testing.T) {
	g := &GB28181API{}
	result := &nativeSnapshotResult{deviceID: "device-1", image: make(chan []byte, 1)}
	g.nativeSnapshots.Store("session-1", result)

	image := []byte{0xff, 0xd8, 0x01, 0xff, 0xd9}
	if err := g.ReceiveNativeSnapshot("session-1", "device-1", image); err != nil {
		t.Fatalf("ReceiveNativeSnapshot() error = %v", err)
	}
	if got := <-result.image; string(got) != string(image) {
		t.Fatalf("received image = %x, want %x", got, image)
	}
	if err := g.ReceiveNativeSnapshot("session-1", "device-2", image); err == nil {
		t.Fatal("expected device mismatch error")
	}
	if err := g.ReceiveNativeSnapshot("unknown", "device-1", image); err == nil {
		t.Fatal("expected unknown session error")
	}
}

func TestReceiveNativeSnapshotRejectsNonJPEG(t *testing.T) {
	g := &GB28181API{}
	g.nativeSnapshots.Store("session-1", &nativeSnapshotResult{
		deviceID: "device-1",
		image:    make(chan []byte, 1),
	})

	if err := g.ReceiveNativeSnapshot("session-1", "device-1", []byte("not-an-image")); err == nil {
		t.Fatal("expected non-JPEG error")
	}
}
