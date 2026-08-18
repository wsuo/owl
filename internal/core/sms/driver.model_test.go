package sms

import (
	"encoding/json"
	"testing"
)

func TestZLMStreamLiveAddrUsesRegularHLS(t *testing.T) {
	driver := NewZLMDriver()
	addr := driver.GetStreamLiveAddr(nil, &MediaServer{}, "https://nvr.example", "nvr.example", "isup", "camera01", "play-token")

	want := "https://nvr.example/proxy/sms/isup/camera01/hls.m3u8?token=play-token"
	if addr.HLS != want {
		t.Fatalf("HLS address = %q, want %q", addr.HLS, want)
	}
}

// TestStreamLiveAddrJSONFieldNames 保证播放地址只输出新的破坏式字段名。
func TestStreamLiveAddrJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(StreamLiveAddr{FLV: "http", WSFLV: "websocket"})
	if err != nil {
		t.Fatalf("序列化播放地址失败: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("解析播放地址失败: %v", err)
	}
	for _, key := range []string{"flv", "ws-flv"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("播放地址缺少字段 %q", key)
		}
	}
	for _, key := range []string{"http_flv", "ws_flv"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("播放地址仍包含旧字段 %q", key)
		}
	}
}
