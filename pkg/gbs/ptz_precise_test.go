package gbs

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestBuildPTZPositionQueryXML(t *testing.T) {
	body, err := buildPTZPositionQueryXML("34020000001320000001", 123456)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"<Query>", "<CmdType>PTZPosition</CmdType>", "<SN>123456</SN>", "<DeviceID>34020000001320000001</DeviceID>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("query XML missing %q: %s", want, text)
		}
	}
}

func TestBuildPTZPreciseControlXML(t *testing.T) {
	pan, tilt, zoom := 123.45, -12.5, 4.0
	body, err := buildPTZPreciseControlXML("34020000001320000001", 654321, &pan, &tilt, &zoom)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"<Control>", "<CmdType>DeviceControl</CmdType>", "<PTZPreciseCtrl>", "<Pan>123.45</Pan>", "<Tilt>-12.5</Tilt>", "<Zoom>4</Zoom>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("control XML missing %q: %s", want, text)
		}
	}
}

func TestBuildPTZPreciseControlXMLRejectsInvalidTarget(t *testing.T) {
	pan := 361.0
	if _, err := buildPTZPreciseControlXML("34020000001320000001", 1, &pan, nil, nil); err == nil {
		t.Fatal("expected invalid pan to fail")
	}
	if _, err := buildPTZPreciseControlXML("34020000001320000001", 1, nil, nil, nil); err == nil {
		t.Fatal("expected empty target to fail")
	}
}

func TestDecodePTZPosition(t *testing.T) {
	var position PTZPosition
	err := xml.Unmarshal([]byte(`<Response><CmdType>PTZPosition</CmdType><SN>7</SN><DeviceID>34020000001320000001</DeviceID><Pan>180.25</Pan><Tilt>12.5</Tilt><Zoom>3</Zoom><HorizontalFieldAngle>31.2</HorizontalFieldAngle></Response>`), &position)
	if err != nil {
		t.Fatal(err)
	}
	if position.Pan == nil || *position.Pan != 180.25 || position.Tilt == nil || *position.Tilt != 12.5 || position.Zoom == nil || *position.Zoom != 3 {
		t.Fatalf("unexpected position: %+v", position)
	}
}
