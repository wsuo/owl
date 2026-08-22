package zlm

import (
	"encoding/json"
	"testing"
)

func TestMediaItemDecodesIngressBytesSpeed(t *testing.T) {
	var item MediaItem
	if err := json.Unmarshal([]byte(`{"app":"isup","stream":"camera-main","bytesSpeed":384000}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.BytesSpeed != 384000 {
		t.Fatalf("bytesSpeed = %d, want 384000", item.BytesSpeed)
	}
}
