package gbs

import (
	"encoding/hex"
	"fmt"
)

const (
	PresetSet    = "set"
	PresetCall   = "call"
	PresetDelete = "delete"
)

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
