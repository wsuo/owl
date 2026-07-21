package gbs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gowvp/owl/pkg/gbs/sip"
)

func TestBuildSDRecordingControlXML(t *testing.T) {
	body, err := buildSDRecordingControlXML("34020000001320000001", SDRecordingStart, 123456)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{"<CmdType>DeviceControl</CmdType>", "<SN>123456</SN>", "<DeviceID>34020000001320000001</DeviceID>", "<RecordCmd>Record</RecordCmd>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %s", want, got)
		}
	}
	if _, err := buildSDRecordingControlXML("id", "bad", 1); err == nil {
		t.Fatal("expected invalid action error")
	}
}

func TestPendingSDRecordingQueryFragmentsEmptyAndTimeout(t *testing.T) {
	pending := &pendingSDRecordingQuery{expected: -1, done: make(chan struct{})}
	pending.append(2, []SDRecordingSegment{{StartTime: "2026-07-21T09:00:00"}})
	select {
	case <-pending.done:
		t.Fatal("query completed before all fragments arrived")
	default:
	}
	pending.append(2, []SDRecordingSegment{{StartTime: "2026-07-21T08:00:00"}})
	items, err := waitForSDRecordingQuery(pending, time.Second)
	if err != nil || len(items) != 2 || items[0].StartTime != "2026-07-21T08:00:00" {
		t.Fatalf("unexpected fragmented result: items=%v err=%v", items, err)
	}

	empty := &pendingSDRecordingQuery{expected: -1, done: make(chan struct{})}
	empty.append(0, nil)
	items, err = waitForSDRecordingQuery(empty, time.Second)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty result must be a non-nil empty slice: items=%v err=%v", items, err)
	}

	timedOut := &pendingSDRecordingQuery{expected: -1, done: make(chan struct{})}
	if _, err = waitForSDRecordingQuery(timedOut, time.Millisecond); !errors.Is(err, ErrSDRecordingQueryTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestBuildPlaybackSDP(t *testing.T) {
	start := time.Unix(1_721_536_200, 0)
	end := start.Add(10 * time.Minute)
	body := string(buildPlaybackSDP("34020000002000000001", "34020000001320000001", "192.0.2.10", 20001, 1, "0100000001", start, end))
	for _, want := range []string{
		"s=Playback\r\n",
		"u=34020000001320000001:0\r\n",
		"c=IN IP4 192.0.2.10\r\n",
		"m=video 20001 TCP/RTP/AVP 96 97 98 99\r\n",
		"a=recvonly\r\n",
		"a=setup:passive\r\n",
		"y=0100000001\r\n",
		"f=\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in playback SDP:\n%s", want, body)
		}
	}
}

func TestBuildPresetCommand(t *testing.T) {
	tests := []struct{ action, want string }{
		{PresetSet, "a50f01810007003d"},
		{PresetCall, "a50f01820007003e"},
		{PresetDelete, "a50f01830007003f"},
	}
	for _, tt := range tests {
		got, err := BuildPresetCommand(tt.action, 7)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %s want %s", tt.action, got, tt.want)
		}
	}
	if _, err := BuildPresetCommand(PresetSet, 0); err == nil {
		t.Fatal("expected range error")
	}
}

func TestBuildPresetQueryXML(t *testing.T) {
	body, err := buildPresetQueryXML("34020000001320000001", 654321)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{
		"<Query>",
		"<CmdType>PresetQuery</CmdType>",
		"<SN>654321</SN>",
		"<DeviceID>34020000001320000001</DeviceID>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %s", want, got)
		}
	}
}

func TestBuildDeviceStatusQueryXML(t *testing.T) {
	body, err := buildDeviceStatusQueryXML("34020000001320000001", 123456)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<Query>",
		"<CmdType>DeviceStatus</CmdType>",
		"<SN>123456</SN>",
		"<DeviceID>34020000001320000001</DeviceID>",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("expected %q in XML:\n%s", want, body)
		}
	}
}

func TestDeviceStatusResponseFields(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Response><CmdType>DeviceStatus</CmdType><SN>123456</SN>
<DeviceID>34020000001320000001</DeviceID><Result>OK</Result>
<Online>ONLINE</Online><Status>OK</Status><Encode>ON</Encode><Record>OFF</Record></Response>`)
	var status DeviceStatus
	if err := sip.XMLDecode(body, &status); err != nil {
		t.Fatal(err)
	}
	if status.Record != "OFF" || status.Encode != "ON" || status.Online != "ONLINE" {
		t.Fatalf("unexpected status: %+v", status)
	}
}
