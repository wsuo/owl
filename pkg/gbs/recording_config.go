package gbs

import (
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
)

const (
	ConfigVideoRecordPlan  = "VideoRecordPlan"
	ConfigVideoAlarmRecord = "VideoAlarmRecord"
	ConfigAlarmReport      = "AlarmReport"
)

var (
	ErrRecordingConfigTimeout  = errors.New("recording config response timeout")
	ErrRecordingConfigRejected = errors.New("recording config rejected")
	ErrRecordingConfigEmpty    = errors.New("recording config response is empty")
	ErrSDCardStatusTimeout     = errors.New("sd card status response timeout")
	ErrSDCardStatusRejected    = errors.New("sd card status rejected")
)

type TimeSegment struct {
	StartHour int `xml:"StartHour" json:"start_hour"`
	StartMin  int `xml:"StartMin" json:"start_min"`
	StartSec  int `xml:"StartSec" json:"start_sec"`
	StopHour  int `xml:"StopHour" json:"stop_hour"`
	StopMin   int `xml:"StopMin" json:"stop_min"`
	StopSec   int `xml:"StopSec" json:"stop_sec"`
}

type RecordSchedule struct {
	WeekDayNum        int           `xml:"WeekDayNum" json:"weekday"`
	TimeSegmentSumNum int           `xml:"TimeSegmentSumNum" json:"-"`
	TimeSegments      []TimeSegment `xml:"TimeSegment" json:"segments"`
}

type VideoRecordPlan struct {
	RecordEnable         int              `xml:"RecordEnable" json:"record_enable"`
	RecordScheduleSumNum int              `xml:"RecordScheduleSumNum" json:"-"`
	RecordSchedules      []RecordSchedule `xml:"RecordSchedule" json:"schedules"`
	StreamNumber         int              `xml:"StreamNumber" json:"stream_number"`
}

type VideoAlarmRecord struct {
	RecordEnable  int `xml:"RecordEnable" json:"record_enable"`
	RecordTime    int `xml:"RecordTime,omitempty" json:"record_time"`
	PreRecordTime int `xml:"PreRecordTime,omitempty" json:"pre_record_time"`
	StreamNumber  int `xml:"StreamNumber" json:"stream_number"`
}

type AlarmReport struct {
	MotionDetection int `xml:"MotionDetection" json:"motion_detection"`
	FieldDetection  int `xml:"FieldDetection" json:"field_detection"`
}

type configDownloadRequest struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ConfigType string   `xml:"ConfigType"`
}

type deviceRecordingConfigRequest struct {
	XMLName          xml.Name          `xml:"Control"`
	CmdType          string            `xml:"CmdType"`
	SN               int               `xml:"SN"`
	DeviceID         string            `xml:"DeviceID"`
	VideoRecordPlan  *VideoRecordPlan  `xml:"VideoRecordPlan,omitempty"`
	VideoAlarmRecord *VideoAlarmRecord `xml:"VideoAlarmRecord,omitempty"`
	AlarmReport      *AlarmReport      `xml:"AlarmReport,omitempty"`
}

type deviceConfigResponse struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

type pendingConfigResponse struct {
	ConfigType string
	Result     string
	Plan       *VideoRecordPlan
	Alarm      *VideoAlarmRecord
	Report     *AlarmReport
}

func (r pendingConfigResponse) empty() bool {
	return r.Plan == nil && r.Alarm == nil && r.Report == nil
}

type pendingConfigQuery struct {
	ConfigType string
	Done       chan pendingConfigResponse
}

type CapabilityState struct {
	Status     string    `json:"status"`
	VerifiedAt time.Time `json:"verified_at"`
}

type RecordingCapabilities struct {
	SDCardStatus   CapabilityState `json:"sd_card_status"`
	NativeSchedule CapabilityState `json:"native_schedule"`
	AlarmRecording CapabilityState `json:"alarm_recording"`
	AlarmReport    CapabilityState `json:"alarm_report"`
	MotionAlarm    CapabilityState `json:"motion_alarm"`
}

type SDCardInfo struct {
	ID             int    `xml:"ID" json:"id"`
	Name           string `xml:"HddName" json:"name"`
	Status         string `xml:"Status" json:"status"`
	FormatProgress *int   `xml:"FormatProgress" json:"format_progress,omitempty"`
	CapacityMB     int64  `xml:"Capacity" json:"capacity_mb"`
	FreeSpaceMB    int64  `xml:"FreeSpace" json:"free_space_mb"`
}

type sdCardStatusRequest struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type sdCardStatusResponse struct {
	XMLName     xml.Name     `xml:"Response"`
	CmdType     string       `xml:"CmdType"`
	SN          int          `xml:"SN"`
	DeviceID    string       `xml:"DeviceID"`
	Result      string       `xml:"Result"`
	SumNum      int          `xml:"SumNum"`
	Items       []SDCardInfo `xml:"SDCardStatusInfo>Item"`
	CompatItems []SDCardInfo `xml:"SDCardStatusList>Item"`
}

type sdCardQueryResult struct {
	Result string
	Items  []SDCardInfo
}

type AlarmTypeParam struct {
	EventType int `xml:"EventType,omitempty" json:"event_type,omitempty"`
}

type AlarmEvent struct {
	SourceDeviceID   string         `xml:"-" json:"source_device_id"`
	SN               int            `xml:"SN" json:"sn"`
	DeviceID         string         `xml:"DeviceID" json:"device_id"`
	AlarmPriority    string         `xml:"AlarmPriority" json:"alarm_priority"`
	AlarmMethod      string         `xml:"AlarmMethod" json:"alarm_method"`
	AlarmTime        string         `xml:"AlarmTime" json:"alarm_time"`
	AlarmDescription string         `xml:"AlarmDescription" json:"alarm_description,omitempty"`
	Longitude        *float64       `xml:"Longitude" json:"longitude,omitempty"`
	Latitude         *float64       `xml:"Latitude" json:"latitude,omitempty"`
	AlarmType        int            `xml:"Info>AlarmType" json:"alarm_type,omitempty"`
	AlarmTypeParam   AlarmTypeParam `xml:"Info>AlarmTypeParam" json:"alarm_type_param,omitempty"`
	ReceivedAt       time.Time      `xml:"-" json:"received_at"`
	RawXML           string         `xml:"-" json:"raw_xml"`
}

type alarmResponse struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

func boolInt(value int) bool { return value == 0 || value == 1 }

func ValidateVideoRecordPlan(plan *VideoRecordPlan) error {
	if plan == nil || !boolInt(plan.RecordEnable) || plan.StreamNumber < 0 {
		return errors.New("invalid recording plan")
	}
	if len(plan.RecordSchedules) > 7 {
		return errors.New("recording plan supports at most 7 days")
	}
	seen := map[int]bool{}
	for i := range plan.RecordSchedules {
		day := &plan.RecordSchedules[i]
		if day.WeekDayNum < 1 || day.WeekDayNum > 7 || seen[day.WeekDayNum] {
			return errors.New("weekday must be unique and between 1 and 7")
		}
		seen[day.WeekDayNum] = true
		if len(day.TimeSegments) > 8 {
			return errors.New("each day supports at most 8 time segments")
		}
		sort.Slice(day.TimeSegments, func(a, b int) bool {
			return segmentSeconds(day.TimeSegments[a], false) < segmentSeconds(day.TimeSegments[b], false)
		})
		previousEnd := -1
		for _, segment := range day.TimeSegments {
			start, end := segmentSeconds(segment, false), segmentSeconds(segment, true)
			if start < 0 || end < 0 || end <= start || start < previousEnd {
				return errors.New("time segments must be valid, non-overlapping, and within one day")
			}
			previousEnd = end
		}
		day.TimeSegmentSumNum = len(day.TimeSegments)
	}
	plan.RecordScheduleSumNum = len(plan.RecordSchedules)
	return nil
}

func RecordingConfigEqual(configType string, expected, actual any) bool {
	switch configType {
	case ConfigVideoRecordPlan:
		want, ok1 := expected.(*VideoRecordPlan)
		got, ok2 := actual.(*VideoRecordPlan)
		if !ok1 || !ok2 || ValidateVideoRecordPlan(want) != nil || ValidateVideoRecordPlan(got) != nil {
			return false
		}
		return reflect.DeepEqual(want, got)
	case ConfigVideoAlarmRecord:
		want, ok1 := expected.(*VideoAlarmRecord)
		got, ok2 := actual.(*VideoAlarmRecord)
		return ok1 && ok2 && reflect.DeepEqual(want, got)
	case ConfigAlarmReport:
		want, ok1 := expected.(*AlarmReport)
		got, ok2 := actual.(*AlarmReport)
		return ok1 && ok2 && reflect.DeepEqual(want, got)
	default:
		return false
	}
}

func segmentSeconds(segment TimeSegment, stop bool) int {
	h, m, s := segment.StartHour, segment.StartMin, segment.StartSec
	if stop {
		h, m, s = segment.StopHour, segment.StopMin, segment.StopSec
	}
	if h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59 {
		return -1
	}
	return h*3600 + m*60 + s
}

func normalizeConfig(configType string, value any) (*deviceRecordingConfigRequest, error) {
	req := &deviceRecordingConfigRequest{CmdType: "DeviceConfig"}
	switch configType {
	case ConfigVideoRecordPlan:
		plan, ok := value.(*VideoRecordPlan)
		if !ok {
			return nil, errors.New("invalid recording plan")
		}
		if err := ValidateVideoRecordPlan(plan); err != nil {
			return nil, err
		}
		req.VideoRecordPlan = plan
	case ConfigVideoAlarmRecord:
		alarm, ok := value.(*VideoAlarmRecord)
		if !ok || !boolInt(alarm.RecordEnable) || alarm.RecordTime < 0 || alarm.PreRecordTime < 0 || alarm.StreamNumber < 0 {
			return nil, errors.New("invalid alarm recording config")
		}
		req.VideoAlarmRecord = alarm
	case ConfigAlarmReport:
		report, ok := value.(*AlarmReport)
		if !ok || !boolInt(report.MotionDetection) || !boolInt(report.FieldDetection) {
			return nil, errors.New("invalid alarm report config")
		}
		req.AlarmReport = report
	default:
		return nil, fmt.Errorf("unsupported config type: %s", configType)
	}
	return req, nil
}

func configKey(sourceDeviceID, targetDeviceID string, sn int) string {
	return fmt.Sprintf("%s:%s:%d", sourceDeviceID, targetDeviceID, sn)
}

func (s *Server) QueryRecordingConfig(channel *ipc.Channel, configType string) (*pendingConfigResponse, error) {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	var sn int
	var key string
	query := &pendingConfigQuery{ConfigType: configType, Done: make(chan pendingConfigResponse, 1)}
	for {
		sn = sip.RandInt(100000, 999999)
		key = configKey(channel.DeviceID, channel.ChannelID, sn)
		if _, loaded := s.gb.configQueries.LoadOrStore(key, query); !loaded {
			break
		}
	}
	defer s.gb.configQueries.Delete(key)
	body, err := sip.XMLEncode(configDownloadRequest{CmdType: "ConfigDownload", SN: sn, DeviceID: channel.ChannelID, ConfigType: configType})
	if err != nil {
		return nil, err
	}
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return nil, err
	}
	if _, err = sipResponse(tx); err != nil {
		return nil, err
	}
	select {
	case response := <-query.Done:
		if !strings.EqualFold(response.Result, "OK") {
			return nil, fmt.Errorf("%w: %s query: %s", ErrRecordingConfigRejected, configType, response.Result)
		}
		if response.empty() {
			return nil, fmt.Errorf("%w: %s", ErrRecordingConfigEmpty, configType)
		}
		return &response, nil
	case <-time.After(10 * time.Second):
		return nil, ErrRecordingConfigTimeout
	}
}

func (s *Server) SetRecordingConfig(channel *ipc.Channel, configType string, value any) error {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return ErrDeviceOffline
	}
	req, err := normalizeConfig(configType, value)
	if err != nil {
		return err
	}
	req.SN, req.DeviceID = sip.RandInt(100000, 999999), channel.ChannelID
	key := configKey(channel.DeviceID, channel.ChannelID, req.SN)
	done := make(chan deviceConfigResponse, 1)
	s.gb.configControls.Store(key, done)
	defer s.gb.configControls.Delete(key)
	body, err := sip.XMLEncode(req)
	if err != nil {
		return err
	}
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return err
	}
	if _, err = sipResponse(tx); err != nil {
		return err
	}
	select {
	case response := <-done:
		if !strings.EqualFold(response.Result, "OK") {
			return fmt.Errorf("%w: %s: %s", ErrRecordingConfigRejected, configType, response.Result)
		}
		return nil
	case <-time.After(10 * time.Second):
		return ErrRecordingConfigTimeout
	}
}

func (g *GB28181API) resolveConfigQuery(sourceDeviceID string, msg ConfigDownloadResponse) {
	configType := ""
	switch {
	case msg.VideoRecordPlan != nil:
		configType = ConfigVideoRecordPlan
	case msg.VideoAlarmRecord != nil:
		configType = ConfigVideoAlarmRecord
	case msg.AlarmReport != nil:
		configType = ConfigAlarmReport
	}
	value, ok := g.configQueries.Load(configKey(sourceDeviceID, msg.DeviceID, msg.SN))
	if !ok {
		return
	}
	query := value.(*pendingConfigQuery)
	if configType != "" && configType != query.ConfigType {
		return
	}
	response := pendingConfigResponse{ConfigType: query.ConfigType, Result: msg.Result, Plan: msg.VideoRecordPlan, Alarm: msg.VideoAlarmRecord, Report: msg.AlarmReport}
	select {
	case query.Done <- response:
	default:
	}
}

func (g *GB28181API) resolveConfigControl(sourceDeviceID string, msg deviceConfigResponse) {
	if value, ok := g.configControls.Load(configKey(sourceDeviceID, msg.DeviceID, msg.SN)); ok {
		select {
		case value.(chan deviceConfigResponse) <- msg:
		default:
		}
	}
}

func (s *Server) QuerySDCardStatus(channel *ipc.Channel) ([]SDCardInfo, error) {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	sn := sip.RandInt(100000, 999999)
	key := configKey(channel.DeviceID, channel.ChannelID, sn)
	done := make(chan sdCardQueryResult, 1)
	s.gb.sdCardQueries.Store(key, done)
	defer s.gb.sdCardQueries.Delete(key)
	body, err := sip.XMLEncode(sdCardStatusRequest{CmdType: "SDCardStatus", SN: sn, DeviceID: channel.ChannelID})
	if err != nil {
		return nil, err
	}
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return nil, err
	}
	if _, err = sipResponse(tx); err != nil {
		return nil, err
	}
	select {
	case response := <-done:
		if response.Result != "" && !strings.EqualFold(response.Result, "OK") {
			return nil, fmt.Errorf("%w: %s", ErrSDCardStatusRejected, response.Result)
		}
		return response.Items, nil
	case <-time.After(10 * time.Second):
		return nil, ErrSDCardStatusTimeout
	}
}

func (g *GB28181API) sipMessageSDCardStatus(ctx *sip.Context) {
	var msg sdCardStatusResponse
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	if len(msg.Items) == 0 {
		msg.Items = msg.CompatItems
	}
	if value, ok := g.sdCardQueries.Load(configKey(ctx.DeviceID, msg.DeviceID, msg.SN)); ok {
		select {
		case value.(chan sdCardQueryResult) <- sdCardQueryResult{Result: msg.Result, Items: msg.Items}:
		default:
		}
	}
	ctx.String(http.StatusOK, "OK")
}

func (g *GB28181API) sipMessageAlarm(ctx *sip.Context) {
	var event AlarmEvent
	if err := sip.XMLDecode(ctx.Request.Body(), &event); err != nil {
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	event.ReceivedAt, event.RawXML = time.Now().UTC(), string(ctx.Request.Body())
	event.SourceDeviceID = ctx.DeviceID
	g.alarmEventsMu.Lock()
	g.alarmEvents = append(g.alarmEvents, event)
	if len(g.alarmEvents) > 1000 {
		g.alarmEvents = append([]AlarmEvent(nil), g.alarmEvents[len(g.alarmEvents)-1000:]...)
	}
	g.alarmEventsMu.Unlock()
	ctx.String(http.StatusOK, "OK")
	go g.respondAlarm(event)
}

func (g *GB28181API) respondAlarm(event AlarmEvent) {
	dev, ok := g.svr.memoryStorer.Load(event.SourceDeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		slog.Warn("alarm response skipped: device offline", "device_id", event.SourceDeviceID, "sn", event.SN)
		return
	}
	body, err := sip.XMLEncode(alarmResponse{
		CmdType: "Alarm", DeviceID: event.DeviceID, SN: event.SN, Result: "OK",
	})
	if err != nil {
		slog.Error("encode alarm response failed", "device_id", event.SourceDeviceID, "sn", event.SN, "err", err)
		return
	}
	tx, err := g.svr.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err == nil {
		_, err = sipResponse(tx)
	}
	if err != nil {
		slog.Warn("send alarm response failed", "device_id", event.SourceDeviceID, "sn", event.SN, "err", err)
	}
}

func (s *Server) AlarmEvents(sourceDeviceID, channelID string, since time.Time) []AlarmEvent {
	s.gb.alarmEventsMu.Lock()
	defer s.gb.alarmEventsMu.Unlock()
	items := make([]AlarmEvent, 0)
	for _, event := range s.gb.alarmEvents {
		if event.SourceDeviceID == sourceDeviceID && event.DeviceID == channelID && !event.ReceivedAt.Before(since) {
			items = append(items, event)
		}
	}
	return items
}

func (s *Server) ProbeRecordingCapabilities(channel *ipc.Channel) RecordingCapabilities {
	if cached, ok := s.gb.recordingCaps.Load(channel.ID); ok {
		caps := cached.(RecordingCapabilities)
		if time.Since(caps.NativeSchedule.VerifiedAt) < 5*time.Minute {
			return caps
		}
	}
	now := time.Now().UTC()
	unknown := CapabilityState{Status: "unknown", VerifiedAt: now}
	caps := RecordingCapabilities{unknown, unknown, unknown, unknown, unknown}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := s.QuerySDCardStatus(channel); err == nil {
			mu.Lock()
			caps.SDCardStatus.Status = "supported"
			mu.Unlock()
		} else if errors.Is(err, ErrSDCardStatusRejected) {
			mu.Lock()
			caps.SDCardStatus.Status = "unsupported"
			mu.Unlock()
		}
	}()
	queries := []struct {
		kind  string
		state *CapabilityState
	}{{ConfigVideoRecordPlan, &caps.NativeSchedule}, {ConfigVideoAlarmRecord, &caps.AlarmRecording}, {ConfigAlarmReport, &caps.AlarmReport}}
	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.QueryRecordingConfig(channel, query.kind); err == nil {
				mu.Lock()
				query.state.Status = "supported"
				mu.Unlock()
			} else if errors.Is(err, ErrRecordingConfigRejected) {
				mu.Lock()
				query.state.Status = "unsupported"
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	caps.MotionAlarm = caps.AlarmReport
	s.gb.recordingCaps.Store(channel.ID, caps)
	return caps
}
