package gbs

import (
	"strings"
	"testing"
	"time"

	"github.com/gowvp/owl/pkg/gbs/sip"
)

func TestResolveConfigQueryCorrelatesEmptyResponseWithPendingType(t *testing.T) {
	api := &GB28181API{}
	query := &pendingConfigQuery{ConfigType: ConfigVideoRecordPlan, Done: make(chan pendingConfigResponse, 1)}
	api.configQueries.Store(configKey("device-1", "channel-1", 123), query)

	api.resolveConfigQuery("device-1", ConfigDownloadResponse{
		SN: 123, DeviceID: "channel-1", Result: "OK",
	})

	select {
	case response := <-query.Done:
		if response.ConfigType != ConfigVideoRecordPlan || response.Plan != nil {
			t.Fatalf("unexpected response: %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("empty response was not correlated")
	}
}

func TestResolveConfigQueryRejectsMismatchedPayloadType(t *testing.T) {
	api := &GB28181API{}
	query := &pendingConfigQuery{ConfigType: ConfigVideoRecordPlan, Done: make(chan pendingConfigResponse, 1)}
	api.configQueries.Store(configKey("device-1", "channel-1", 456), query)

	api.resolveConfigQuery("device-1", ConfigDownloadResponse{
		SN: 456, DeviceID: "channel-1", Result: "OK", VideoAlarmRecord: &VideoAlarmRecord{},
	})

	select {
	case response := <-query.Done:
		t.Fatalf("mismatched response was correlated: %#v", response)
	default:
	}
}

func TestEmptyRecordingConfigHasDedicatedError(t *testing.T) {
	if !(pendingConfigResponse{ConfigType: ConfigVideoRecordPlan, Result: "OK"}).empty() {
		t.Fatal("expected response without a config payload to be empty")
	}
}

func fullDaySegment() TimeSegment {
	return TimeSegment{StartHour: 0, StopHour: 23, StopMin: 59, StopSec: 59}
}

func TestValidateVideoRecordPlanNormalizesSevenDayPlan(t *testing.T) {
	plan := &VideoRecordPlan{RecordEnable: 1, StreamNumber: 0}
	for day := 1; day <= 7; day++ {
		plan.RecordSchedules = append(plan.RecordSchedules, RecordSchedule{WeekDayNum: day, TimeSegments: []TimeSegment{fullDaySegment()}})
	}
	if err := ValidateVideoRecordPlan(plan); err != nil {
		t.Fatal(err)
	}
	if plan.RecordScheduleSumNum != 7 || plan.RecordSchedules[0].TimeSegmentSumNum != 1 {
		t.Fatalf("counts not normalized: %#v", plan)
	}
}

func TestValidateVideoRecordPlanRejectsOverlapAndCrossDay(t *testing.T) {
	cases := []*VideoRecordPlan{
		{RecordEnable: 1, RecordSchedules: []RecordSchedule{{WeekDayNum: 1, TimeSegments: []TimeSegment{{StartHour: 8, StopHour: 10}, {StartHour: 9, StopHour: 11}}}}},
		{RecordEnable: 1, RecordSchedules: []RecordSchedule{{WeekDayNum: 1, TimeSegments: []TimeSegment{{StartHour: 23, StopHour: 1}}}}},
	}
	for _, plan := range cases {
		if ValidateVideoRecordPlan(plan) == nil {
			t.Fatalf("expected invalid plan: %#v", plan)
		}
	}
}

func TestRecordingConfigXMLIncludesGB28181Fields(t *testing.T) {
	plan := &VideoRecordPlan{RecordEnable: 1, StreamNumber: 0, RecordSchedules: []RecordSchedule{{WeekDayNum: 1, TimeSegments: []TimeSegment{fullDaySegment()}}}}
	req, err := normalizeConfig(ConfigVideoRecordPlan, plan)
	if err != nil {
		t.Fatal(err)
	}
	req.SN, req.DeviceID = 123, "34020000001320000001"
	body, err := sip.XMLEncode(req)
	if err != nil {
		t.Fatal(err)
	}
	xml := string(body)
	for _, field := range []string{"<CmdType>DeviceConfig</CmdType>", "<VideoRecordPlan>", "<WeekDayNum>1</WeekDayNum>", "<StopSec>59</StopSec>"} {
		if !strings.Contains(xml, field) {
			t.Fatalf("missing %s in %s", field, xml)
		}
	}
}

func TestAlarmEventParsesMotionDetection(t *testing.T) {
	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>34020000001320000001</DeviceID><AlarmPriority>2</AlarmPriority><AlarmMethod>5</AlarmMethod><AlarmTime>2026-07-22T12:00:00</AlarmTime><Longitude>112.1</Longitude><Latitude>31.9</Latitude><Info><AlarmType>2</AlarmType><AlarmTypeParam><EventType>1</EventType></AlarmTypeParam></Info></Notify>`)
	var event AlarmEvent
	if err := sip.XMLDecode(body, &event); err != nil {
		t.Fatal(err)
	}
	if event.AlarmMethod != "5" || event.AlarmType != 2 || event.AlarmTypeParam.EventType != 1 {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestAlarmResponseXML(t *testing.T) {
	body, err := sip.XMLEncode(alarmResponse{
		CmdType: "Alarm", SN: 7, DeviceID: "34020000001320000001", Result: "OK",
	})
	if err != nil {
		t.Fatal(err)
	}
	xml := string(body)
	for _, field := range []string{"<Response>", "<CmdType>Alarm</CmdType>", "<SN>7</SN>", "<Result>OK</Result>"} {
		if !strings.Contains(xml, field) {
			t.Fatalf("missing %s in %s", field, xml)
		}
	}
}

func TestSDCardStatusParsesStandardAndCompatLists(t *testing.T) {
	for _, body := range []string{
		`<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>1</DeviceID><Result>OK</Result><SumNum>1</SumNum><SDCardStatusInfo><Item><ID>1</ID><Status>Normal</Status><Capacity>1024</Capacity><FreeSpace>512</FreeSpace></Item></SDCardStatusInfo></Response>`,
		`<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>1</DeviceID><Result>OK</Result><SumNum>1</SumNum><SDCardStatusList Num="1"><Item><ID>1</ID><Status>Normal</Status><Capacity>1024</Capacity><FreeSpace>512</FreeSpace></Item></SDCardStatusList></Response>`,
	} {
		var response sdCardStatusResponse
		if err := sip.XMLDecode([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		items := response.Items
		if len(items) == 0 {
			items = response.CompatItems
		}
		if len(items) != 1 || items[0].CapacityMB != 1024 || response.Result != "OK" {
			t.Fatalf("unexpected response: %#v", response)
		}
	}
}
