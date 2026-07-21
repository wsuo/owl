package gbs

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
)

const (
	SDRecordingStart = "Record"
	SDRecordingStop  = "StopRecord"
)

type sdRecordingControlRequest struct {
	XMLName   xml.Name `xml:"Control"`
	CmdType   string   `xml:"CmdType"`
	SN        int      `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	RecordCmd string   `xml:"RecordCmd"`
}

type SDRecordingSegment struct {
	DeviceID  string `xml:"DeviceID" json:"device_id"`
	Name      string `xml:"Name" json:"name"`
	FilePath  string `xml:"FilePath" json:"file_path"`
	Address   string `xml:"Address" json:"address"`
	StartTime string `xml:"StartTime" json:"start_time"`
	EndTime   string `xml:"EndTime" json:"end_time"`
	Secrecy   int    `xml:"Secrecy" json:"secrecy"`
	Type      string `xml:"Type" json:"type"`
}

type sdRecordingQueryResponse struct {
	CmdType  string               `xml:"CmdType"`
	SN       int                  `xml:"SN"`
	DeviceID string               `xml:"DeviceID"`
	SumNum   int                  `xml:"SumNum"`
	Items    []SDRecordingSegment `xml:"RecordList>Item"`
}

type pendingSDRecordingQuery struct {
	mu       sync.Mutex
	expected int
	items    []SDRecordingSegment
	done     chan struct{}
	once     sync.Once
}

var ErrSDRecordingQueryTimeout = errors.New("record info response timeout")

func (p *pendingSDRecordingQuery) append(expected int, items []SDRecordingSegment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if expected >= 0 {
		p.expected = expected
	}
	p.items = append(p.items, items...)
	if p.expected == 0 || (p.expected > 0 && len(p.items) >= p.expected) {
		p.once.Do(func() { close(p.done) })
	}
}

func (p *pendingSDRecordingQuery) snapshot() []SDRecordingSegment {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append(make([]SDRecordingSegment, 0, len(p.items)), p.items...)
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime < out[j].StartTime })
	return out
}

func waitForSDRecordingQuery(pending *pendingSDRecordingQuery, timeout time.Duration) ([]SDRecordingSegment, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-pending.done:
		return pending.snapshot(), nil
	case <-timer.C:
		return nil, ErrSDRecordingQueryTimeout
	}
}

func buildSDRecordingControlXML(channelID, action string, sn int) ([]byte, error) {
	if action != SDRecordingStart && action != SDRecordingStop {
		return nil, fmt.Errorf("unsupported SD recording action: %s", action)
	}
	req := sdRecordingControlRequest{CmdType: "DeviceControl", SN: sn, DeviceID: channelID, RecordCmd: action}
	return sip.XMLEncode(req)
}

func (s *Server) ControlSDRecording(channel *ipc.Channel, action string) error {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return ErrChannelNotExist
	}
	body, err := buildSDRecordingControlXML(channel.ChannelID, action, sip.RandInt(100000, 999999))
	if err != nil {
		return err
	}
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return err
	}
	_, err = sipResponse(tx)
	return err
}

func (s *Server) QuerySDRecordings(channel *ipc.Channel, start, end time.Time) ([]SDRecordingSegment, error) {
	if !end.After(start) {
		return nil, errors.New("end must be after start")
	}
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return nil, ErrChannelNotExist
	}

	sn := sip.RandInt(100000, 999999)
	key := fmt.Sprintf("%s:%d", channel.ChannelID, sn)
	pending := &pendingSDRecordingQuery{expected: -1, done: make(chan struct{})}
	s.gb.sdRecordingQueries.Store(key, pending)
	defer s.gb.sdRecordingQueries.Delete(key)

	body := sip.GetRecordInfoXML(channel.ChannelID, sn, start.Unix(), end.Unix())
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return nil, err
	}
	if _, err := sipResponse(tx); err != nil {
		return nil, err
	}

	return waitForSDRecordingQuery(pending, 10*time.Second)
}

func (g *GB28181API) sipMessageRecordInfo(ctx *sip.Context) {
	var msg sdRecordingQueryResponse
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("decode RecordInfo", "err", err)
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	key := fmt.Sprintf("%s:%d", msg.DeviceID, msg.SN)
	if value, ok := g.sdRecordingQueries.Load(key); ok {
		value.(*pendingSDRecordingQuery).append(msg.SumNum, msg.Items)
	}
	ctx.String(http.StatusOK, "OK")
}

func NormalizeRecordingAction(action string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start", "record":
		return SDRecordingStart, nil
	case "stop", "stoprecord":
		return SDRecordingStop, nil
	default:
		return "", fmt.Errorf("action must be start or stop")
	}
}
