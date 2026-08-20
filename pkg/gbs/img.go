package gbs

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gowvp/owl/pkg/gbs/sip"
)

func (g *GB28181API) QuerySnapshot(deviceID, channelID string) error {
	_, err := g.QueryNativeSnapshot(deviceID, channelID, "http://192.168.10.31:15123/gb28181/snapshot")
	return err
}

type nativeSnapshotResult struct {
	deviceID string
	image    chan []byte
}

func (g *GB28181API) QueryNativeSnapshot(deviceID, channelID, callbackURL string) ([]byte, error) {
	slog.Debug("QuerySnapshot", "deviceID", deviceID)
	ipc, ok := g.svr.memoryStorer.Load(deviceID)
	if !ok || !ipc.IsOnline {
		return nil, ErrDeviceOffline
	}
	if strings.TrimSpace(callbackURL) == "" {
		return nil, fmt.Errorf("snapshot callback URL is required")
	}
	parsedURL, err := url.Parse(callbackURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid snapshot callback URL")
	}
	sessionID := fmt.Sprintf("gowvp-%d", time.Now().UnixNano())
	query := parsedURL.Query()
	query.Set("session_id", sessionID)
	query.Set("device_id", deviceID)
	parsedURL.RawQuery = query.Encode()
	result := &nativeSnapshotResult{deviceID: deviceID, image: make(chan []byte, 1)}
	g.nativeSnapshots.Store(sessionID, result)
	defer g.nativeSnapshots.Delete(sessionID)

	body := NewDeviceConfig(channelID).SetSnapShotConfig(&SnapShot{
		SnapNum:   1,
		Interval:  1,
		UploadURL: parsedURL.String(),
		SessionID: sessionID,
	}).Marshal()

	tx, err := g.svr.wrapRequest(ipc, sip.MethodMessage, &sip.ContentTypeXML, body)
	if err != nil {
		return nil, err
	}
	if _, err = sipResponse(tx); err != nil {
		return nil, err
	}
	select {
	case image := <-result.image:
		return image, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("snapshot callback timeout: session_id=%s", sessionID)
	}
}

func (g *GB28181API) ReceiveNativeSnapshot(sessionID, deviceID string, image []byte) error {
	if len(image) < 4 || string(image[:2]) != "\xff\xd8" || string(image[len(image)-2:]) != "\xff\xd9" {
		return fmt.Errorf("snapshot callback is not a JPEG")
	}
	value, ok := g.nativeSnapshots.Load(sessionID)
	if !ok {
		return fmt.Errorf("unknown snapshot session")
	}
	result := value.(*nativeSnapshotResult)
	if result.deviceID != deviceID {
		return fmt.Errorf("snapshot device mismatch")
	}
	select {
	case result.image <- append([]byte(nil), image...):
		return nil
	default:
		return fmt.Errorf("snapshot already received")
	}
}
