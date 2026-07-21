package gbs

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
)

var ErrPTZPositionQueryTimeout = errors.New("PTZ position query response timeout")

type ptzPositionQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type PTZPosition struct {
	CmdType              string   `xml:"CmdType" json:"-"`
	SN                   int      `xml:"SN" json:"-"`
	DeviceID             string   `xml:"DeviceID" json:"device_id"`
	Pan                  *float64 `xml:"Pan" json:"pan"`
	Tilt                 *float64 `xml:"Tilt" json:"tilt"`
	Zoom                 *float64 `xml:"Zoom" json:"zoom"`
	HorizontalFieldAngle *float64 `xml:"HorizontalFieldAngle" json:"horizontal_field_angle,omitempty"`
	VerticalFieldAngle   *float64 `xml:"VerticalFieldAngle" json:"vertical_field_angle,omitempty"`
	MaxViewDistance      *float64 `xml:"MaxViewDistance" json:"max_view_distance,omitempty"`
}

type ptzPreciseControl struct {
	XMLName        xml.Name         `xml:"Control"`
	CmdType        string           `xml:"CmdType"`
	SN             int              `xml:"SN"`
	DeviceID       string           `xml:"DeviceID"`
	PTZPreciseCtrl ptzPreciseTarget `xml:"PTZPreciseCtrl"`
}

type ptzPreciseTarget struct {
	Pan  *float64 `xml:"Pan,omitempty"`
	Tilt *float64 `xml:"Tilt,omitempty"`
	Zoom *float64 `xml:"Zoom,omitempty"`
}

func buildPTZPositionQueryXML(channelID string, sn int) ([]byte, error) {
	return sip.XMLEncode(ptzPositionQuery{CmdType: "PTZPosition", SN: sn, DeviceID: channelID})
}

func buildPTZPreciseControlXML(channelID string, sn int, pan, tilt, zoom *float64) ([]byte, error) {
	if pan == nil && tilt == nil && zoom == nil {
		return nil, errors.New("at least one of pan, tilt or zoom is required")
	}
	if pan != nil && (math.IsNaN(*pan) || math.IsInf(*pan, 0) || *pan < 0 || *pan > 360) {
		return nil, errors.New("pan must be between 0 and 360 degrees")
	}
	if tilt != nil && (math.IsNaN(*tilt) || math.IsInf(*tilt, 0) || *tilt < -180 || *tilt > 180) {
		return nil, errors.New("tilt must be between -180 and 180 degrees")
	}
	if zoom != nil && (math.IsNaN(*zoom) || math.IsInf(*zoom, 0) || *zoom < 1 || *zoom > 256) {
		return nil, errors.New("zoom must be between 1 and 256")
	}
	return sip.XMLEncode(ptzPreciseControl{
		CmdType:        "DeviceControl",
		SN:             sn,
		DeviceID:       channelID,
		PTZPreciseCtrl: ptzPreciseTarget{Pan: pan, Tilt: tilt, Zoom: zoom},
	})
}

func (s *Server) QueryPTZPosition(channel *ipc.Channel) (*PTZPosition, error) {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return nil, ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return nil, ErrChannelNotExist
	}

	sn := sip.RandInt(100000, 999999)
	key := fmt.Sprintf("%s:%d", channel.ChannelID, sn)
	done := make(chan *PTZPosition, 1)
	s.gb.ptzPositionQueries.Store(key, done)
	defer s.gb.ptzPositionQueries.Delete(key)

	body, err := buildPTZPositionQueryXML(channel.ChannelID, sn)
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
	case position := <-done:
		if position.Pan == nil || position.Tilt == nil {
			return nil, errors.New("device returned PTZ position without pan or tilt")
		}
		return position, nil
	case <-time.After(8 * time.Second):
		return nil, ErrPTZPositionQueryTimeout
	}
}

func (s *Server) MovePTZPrecise(channel *ipc.Channel, pan, tilt, zoom *float64) error {
	dev, ok := s.memoryStorer.Load(channel.DeviceID)
	if !ok || !dev.IsOnline || dev.conn == nil {
		return ErrDeviceOffline
	}
	if _, ok := dev.GetChannel(channel.ChannelID); !ok {
		return ErrChannelNotExist
	}
	body, err := buildPTZPreciseControlXML(channel.ChannelID, sip.RandInt(100000, 999999), pan, tilt, zoom)
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

func (g *GB28181API) sipMessagePTZPosition(ctx *sip.Context) {
	var msg PTZPosition
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("decode PTZPosition", "err", err)
		ctx.String(http.StatusBadRequest, ErrXMLDecode.Error())
		return
	}
	key := fmt.Sprintf("%s:%d", msg.DeviceID, msg.SN)
	if value, ok := g.ptzPositionQueries.Load(key); ok {
		select {
		case value.(chan *PTZPosition) <- &msg:
		default:
		}
	}
	ctx.String(http.StatusOK, "OK")
}
