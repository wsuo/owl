package gbs

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/internal/core/sms"
	"github.com/gowvp/owl/pkg/gbs/sip"
	"github.com/gowvp/owl/pkg/zlm"
	sdp "github.com/panjjo/gosdp"
)

type SDPlaybackInput struct {
	Channel    *ipc.Channel
	SMS        *sms.MediaServer
	StreamMode int8
	SessionID  string
	Start      time.Time
	End        time.Time
}

type SDPlaybackSession struct {
	ID       string    `json:"session_id"`
	App      string    `json:"app"`
	Stream   string    `json:"stream"`
	Start    time.Time `json:"start_time"`
	End      time.Time `json:"end_time"`
	Channel  *ipc.Channel
	Response *sip.Response
}

func playbackStreamID(sessionID string) string {
	clean := strings.ReplaceAll(sessionID, "-", "")
	if len(clean) > 24 {
		clean = clean[:24]
	}
	return "pb" + clean
}

func buildPlaybackSDP(deviceID, channelID, ip string, port int, streamMode int8, ssrc string, start, end time.Time) []byte {
	protocol := "TCP/RTP/AVP"
	if streamMode == 0 {
		protocol = "RTP/AVP"
	}
	video := sdp.Media{Description: sdp.MediaDescription{Type: "video", Port: port, Formats: []string{"96", "97", "98", "99"}, Protocol: protocol}}
	video.AddAttribute("recvonly")
	if streamMode == 1 {
		video.AddAttribute("setup", "passive")
		video.AddAttribute("connection", "new")
	} else if streamMode == 2 {
		video.AddAttribute("setup", "active")
		video.AddAttribute("connection", "new")
	}
	video.AddAttribute("rtpmap", "96", "PS/90000")
	video.AddAttribute("rtpmap", "97", "MPEG4/90000")
	video.AddAttribute("rtpmap", "98", "H264/90000")
	video.AddAttribute("rtpmap", "99", "H265/90000")
	msg := &sdp.Message{
		Version:    0,
		Origin:     sdp.Origin{Username: deviceID, NetworkType: "IN", AddressType: "IP4", Address: ip},
		Name:       "Playback",
		URI:        fmt.Sprintf("%s:0", channelID),
		Connection: sdp.ConnectionData{NetworkType: "IN", AddressType: "IP4", IP: net.ParseIP(ip)},
		Timing:     []sdp.Timing{{Start: start, End: end}},
		Medias:     []sdp.Media{video},
		SSRC:       ssrc,
	}
	body := msg.Append(nil).AppendTo(nil)
	return append(body, "f=\r\n"...)
}

func (g *GB28181API) StartSDPlayback(in *SDPlaybackInput) (*SDPlaybackSession, error) {
	if !in.End.After(in.Start) {
		return nil, fmt.Errorf("end must be after start")
	}
	ch, ok := g.svr.memoryStorer.GetChannel(in.Channel.DeviceID, in.Channel.ChannelID)
	if !ok || ch.device == nil || !ch.device.IsOnline {
		return nil, ErrDeviceOffline
	}
	streamID := playbackStreamID(in.SessionID)
	ssrc := g.getSSRC(1)
	ssrcValue, _ := strconv.ParseUint(ssrc, 10, 64)
	open, err := g.sms.OpenRTPServer(in.SMS, zlm.OpenRTPServerRequest{TCPMode: in.StreamMode, StreamID: streamID, SSRC: ssrcValue})
	if err != nil {
		return nil, err
	}
	cleanup := func() { _, _ = g.sms.CloseRTPServer(in.SMS, zlm.CloseRTPServerRequest{StreamID: streamID}) }

	ip, err := GetIP(in.SMS.GetSDPIP())
	if err != nil {
		cleanup()
		return nil, err
	}
	body := buildPlaybackSDP(in.Channel.DeviceID, ch.ChannelID, ip, open.Port, in.StreamMode, ssrc, in.Start, in.End)
	tx, err := g.svr.wrapRequest(ch, sip.MethodInvite, &sip.ContentTypeSDP, body, func(req *sip.Request) {
		req.AppendHeader(&sip.GenericHeader{HeaderName: "Subject", Contents: fmt.Sprintf("%s:%s,%s:0", ch.ChannelID, ssrc, g.cfg.ID)})
		req.AppendHeader(&sip.GenericHeader{HeaderName: "Range", Contents: fmt.Sprintf("npt=%d-%d", in.Start.Unix(), in.End.Unix())})
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	resp, err := sipResponse(tx)
	if err != nil {
		cleanup()
		return nil, err
	}
	session := &SDPlaybackSession{ID: in.SessionID, App: "rtp", Stream: streamID, Start: in.Start, End: in.End, Channel: in.Channel, Response: resp}
	g.sdPlaybacks.Store(in.SessionID, session)
	return session, nil
}

func (g *GB28181API) StopSDPlayback(sessionID, channelID string, mediaServer *sms.MediaServer) error {
	session, ok := g.sdPlaybacks.Load(sessionID)
	if !ok {
		return nil
	}
	if session.Channel.ID != channelID {
		return fmt.Errorf("playback session does not belong to channel")
	}
	g.sdPlaybacks.Delete(sessionID)
	if session.Response != nil {
		if ch, exists := g.svr.memoryStorer.GetChannel(session.Channel.DeviceID, session.Channel.ChannelID); exists {
			req := sip.NewRequestFromResponse(sip.MethodBYE, session.Response)
			req.SetDestination(ch.Source())
			req.SetConnection(ch.Conn())
			_, _ = g.svr.Request(req)
		}
	}
	_, err := g.sms.CloseRTPServer(mediaServer, zlm.CloseRTPServerRequest{StreamID: session.Stream})
	return err
}

func (g *GB28181API) StopSDPlaybackByStream(streamID string, mediaServer *sms.MediaServer) error {
	var sessionID string
	g.sdPlaybacks.Range(func(key string, session *SDPlaybackSession) bool {
		if session.Stream == streamID {
			sessionID = key
			return false
		}
		return true
	})
	if sessionID == "" {
		return nil
	}
	session, _ := g.sdPlaybacks.Load(sessionID)
	return g.StopSDPlayback(sessionID, session.Channel.ID, mediaServer)
}

func (s *Server) StartSDPlayback(in *SDPlaybackInput) (*SDPlaybackSession, error) {
	return s.gb.StartSDPlayback(in)
}

func (s *Server) StopSDPlayback(sessionID, channelID string, mediaServer *sms.MediaServer) error {
	return s.gb.StopSDPlayback(sessionID, channelID, mediaServer)
}

func (s *Server) StopSDPlaybackByStream(streamID string, mediaServer *sms.MediaServer) error {
	return s.gb.StopSDPlaybackByStream(streamID, mediaServer)
}
