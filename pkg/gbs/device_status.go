package gbs

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
)

var ErrDeviceStatusQueryTimeout = errors.New("device status response timeout")

type deviceStatusQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type DeviceStatus struct {
	CmdType  string `xml:"CmdType" json:"cmd_type"`
	SN       int    `xml:"SN" json:"sn"`
	DeviceID string `xml:"DeviceID" json:"device_id"`
	Result   string `xml:"Result" json:"result"`
	Online   string `xml:"Online" json:"online"`
	Status   string `xml:"Status" json:"status"`
	Reason   string `xml:"Reason" json:"reason,omitempty"`
	Encode   string `xml:"Encode" json:"encode,omitempty"`
	Record   string `xml:"Record" json:"record,omitempty"`
}

func buildDeviceStatusQueryXML(channelID string, sn int) ([]byte, error) {
	return sip.XMLEncode(deviceStatusQuery{CmdType: "DeviceStatus", SN: sn, DeviceID: channelID})
}

func (s *Server) QueryDeviceStatus(channel *ipc.Channel) (*DeviceStatus, error) {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return nil, ErrChannelNotExist
	}

	sn := sip.RandInt(100000, 999999)
	key := fmt.Sprintf("%s:%d", channel.ChannelID, sn)
	done := make(chan *DeviceStatus, 1)
	s.gb.deviceStatusQueries.Store(key, done)
	defer s.gb.deviceStatusQueries.Delete(key)

	body, err := buildDeviceStatusQueryXML(channel.ChannelID, sn)
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
	case status := <-done:
		return status, nil
	case <-time.After(10 * time.Second):
		return nil, ErrDeviceStatusQueryTimeout
	}
}

func (g *GB28181API) sipMessageDeviceStatus(ctx *sip.Context) {
	var msg DeviceStatus
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("decode DeviceStatus", "err", err)
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	key := fmt.Sprintf("%s:%d", msg.DeviceID, msg.SN)
	if value, ok := g.deviceStatusQueries.Load(key); ok {
		select {
		case value.(chan *DeviceStatus) <- &msg:
		default:
		}
	}
	ctx.String(http.StatusOK, "OK")
}
