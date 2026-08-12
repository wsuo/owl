package gbs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/internal/core/sms"
	"github.com/gowvp/owl/pkg/zlm"
)

const (
	mediaReadyWait       = 15 * time.Second
	mediaReadyPoll       = 200 * time.Millisecond
	playRetryBackoff     = 2 * time.Second
	playCoordReadySchema = "rtp"
)

// playCoord is deliberately per GB device/channel.  The media server stream
// is shared by RTSP, FLV, WebRTC, snapshots and recordings, so a second
// caller must wait for the first INVITE instead of replacing its RTP server.
type playCoord struct {
	inFlight     bool
	done         chan struct{}
	err          error
	active       bool
	backoffUntil time.Time
}

func playCoordKey(channel *ipc.Channel) string {
	return channel.DeviceID + ":" + channel.ChannelID
}

func (g *GB28181API) coordFor(channel *ipc.Channel) *playCoord {
	key := playCoordKey(channel)
	g.playCoordMu.Lock()
	defer g.playCoordMu.Unlock()
	coord := g.playCoords[key]
	if coord == nil {
		coord = &playCoord{}
		g.playCoords[key] = coord
	}
	return coord
}

func videoTrackReady(item zlm.MediaItem) bool {
	for _, track := range item.Tracks {
		if track.CodecType == 0 && track.Ready && track.Width > 0 && track.Height > 0 && track.CodecIDName != "" {
			return true
		}
	}
	return false
}

func (g *GB28181API) mediaReady(server *sms.MediaServer, channelID string) bool {
	if server == nil {
		return false
	}
	items, err := g.sms.GetMediaInfo(server, playCoordReadySchema, channelID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.App == playCoordReadySchema && item.Stream == channelID && videoTrackReady(item) {
			return true
		}
	}
	return false
}

func (g *GB28181API) waitMediaReady(ctx context.Context, server *sms.MediaServer, channelID string) error {
	wait := mediaReadyWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}
	}
	if wait <= 0 {
		return ctx.Err()
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(mediaReadyPoll)
	defer ticker.Stop()
	for {
		if g.mediaReady(server, channelID) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("媒体流在 %s 内未出现 ready 视频轨道", mediaReadyWait)
		case <-ticker.C:
		}
	}
}

// EnsurePlay starts at most one upstream session and waits until ZLM exposes
// a usable video track. Existing ready media is reused without another INVITE.
func (g *GB28181API) EnsurePlay(ctx context.Context, in *PlayInput) error {
	if in == nil || in.Channel == nil || in.SMS == nil {
		return fmt.Errorf("播放参数不完整")
	}
	coord := g.coordFor(in.Channel)
	key := playCoordKey(in.Channel)

	if g.mediaReady(in.SMS, in.Channel.ID) {
		g.playCoordMu.Lock()
		coord.active = true
		g.playCoordMu.Unlock()
		return nil
	}

	g.playCoordMu.Lock()
	if coord.inFlight {
		done := coord.done
		g.playCoordMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
		g.playCoordMu.Lock()
		err := coord.err
		active := coord.active
		g.playCoordMu.Unlock()
		if err != nil {
			return err
		}
		if active && g.mediaReady(in.SMS, in.Channel.ID) {
			return nil
		}
		return fmt.Errorf("播放会话未就绪（通道 %s）", key)
	}
	if time.Now().Before(coord.backoffUntil) {
		until := time.Until(coord.backoffUntil).Round(time.Millisecond)
		g.playCoordMu.Unlock()
		return fmt.Errorf("播放重试退避中，%s 后再试", until)
	}
	coord.inFlight = true
	coord.done = make(chan struct{})
	coord.err = nil
	coord.active = false
	done := coord.done
	g.playCoordMu.Unlock()

	slog.InfoContext(ctx, "确保 GB28181 上游媒体就绪", "device_id", in.Channel.DeviceID, "channel_id", in.Channel.ChannelID)
	err := g.play(in)
	if err == nil {
		err = g.waitMediaReady(ctx, in.SMS, in.Channel.ID)
	}
	if err != nil {
		// Do not leave an RTP receiver and a half-open SIP dialog behind after
		// readiness timeout; the next attempt must start from a clean session.
		_ = g.StopPlay(context.Background(), &StopPlayInput{Channel: in.Channel})
		_, _ = g.sms.CloseRTPServer(in.SMS, zlm.CloseRTPServerRequest{StreamID: in.Channel.ID})
	}

	g.playCoordMu.Lock()
	coord.inFlight = false
	coord.err = err
	coord.active = err == nil
	if err != nil {
		coord.backoffUntil = time.Now().Add(playRetryBackoff)
		slog.WarnContext(ctx, "GB28181 上游媒体未就绪", "device_id", in.Channel.DeviceID, "channel_id", in.Channel.ChannelID, "err", err)
	} else {
		coord.backoffUntil = time.Time{}
		slog.InfoContext(ctx, "GB28181 上游媒体已就绪", "device_id", in.Channel.DeviceID, "channel_id", in.Channel.ChannelID)
	}
	close(done)
	g.playCoordMu.Unlock()
	return err
}

func (g *GB28181API) playEstablishing(deviceID, channelID string) bool {
	g.playCoordMu.Lock()
	defer g.playCoordMu.Unlock()
	coord := g.playCoords[deviceID+":"+channelID]
	return coord != nil && coord.inFlight
}

func (g *GB28181API) markPlayStopped(channel *ipc.Channel) {
	g.playCoordMu.Lock()
	defer g.playCoordMu.Unlock()
	if coord := g.playCoords[playCoordKey(channel)]; coord != nil && !coord.inFlight {
		coord.active = false
	}
}
