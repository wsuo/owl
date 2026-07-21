package gbs

import (
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
)

const (
	PresetSet    = "set"
	PresetCall   = "call"
	PresetDelete = "delete"
)

var ErrPresetQueryTimeout = errors.New("preset query response timeout")

type presetQueryRequest struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type PresetInfo struct {
	ID   int    `xml:"PresetID" json:"id"`
	Name string `xml:"PresetName" json:"name"`
}

type presetQueryResponse struct {
	CmdType  string       `xml:"CmdType"`
	SN       int          `xml:"SN"`
	DeviceID string       `xml:"DeviceID"`
	Items    []PresetInfo `xml:"PresetList>Item"`
}

func buildPresetQueryXML(channelID string, sn int) ([]byte, error) {
	return sip.XMLEncode(presetQueryRequest{CmdType: "PresetQuery", SN: sn, DeviceID: channelID})
}

func BuildPresetCommand(action string, index int) (string, error) {
	if index < 1 || index > 255 {
		return "", fmt.Errorf("preset index must be between 1 and 255")
	}
	var command byte
	switch action {
	case PresetSet:
		command = 0x81
	case PresetCall:
		command = 0x82
	case PresetDelete:
		command = 0x83
	default:
		return "", fmt.Errorf("unsupported preset action: %s", action)
	}
	buf := []byte{0xA5, 0x0F, 0x01, command, 0x00, byte(index), 0x00, 0x00}
	for _, value := range buf[:7] {
		buf[7] += value
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) Preset(deviceID, channelID, action string, index int) error {
	cmd, err := BuildPresetCommand(action, index)
	if err != nil {
		return err
	}
	return s.SendPTZCommand(deviceID, channelID, cmd)
}

func (s *Server) QueryPresets(channel *ipc.Channel) ([]PresetInfo, error) {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return nil, ErrChannelNotExist
	}

	sn := sip.RandInt(100000, 999999)
	key := fmt.Sprintf("%s:%d", channel.ChannelID, sn)
	done := make(chan []PresetInfo, 1)
	s.gb.presetQueries.Store(key, done)
	defer s.gb.presetQueries.Delete(key)

	body, err := buildPresetQueryXML(channel.ChannelID, sn)
	if err != nil {
		return nil, err
	}
	tx, err := s.wrapRequest(dev, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return nil, err
	}
	if _, err := sipResponse(tx); err != nil {
		return nil, err
	}

	select {
	case items := <-done:
		return items, nil
	case <-time.After(10 * time.Second):
		return nil, ErrPresetQueryTimeout
	}
}

func (g *GB28181API) sipMessagePresetQuery(ctx *sip.Context) {
	var msg presetQueryResponse
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("decode PresetQuery", "err", err)
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	key := fmt.Sprintf("%s:%d", msg.DeviceID, msg.SN)
	if value, ok := g.presetQueries.Load(key); ok {
		select {
		case value.(chan []PresetInfo) <- append([]PresetInfo{}, msg.Items...):
		default:
		}
	}
	ctx.String(http.StatusOK, "OK")
}
