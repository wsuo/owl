package gbs

import (
	"github.com/gowvp/owl/internal/core/ipc"
	"github.com/gowvp/owl/pkg/gbs/sip"
	"github.com/ixugo/goddd/pkg/orm"
)

// MessageNotify 心跳包 XML 结构
type MessageNotify struct {
	CmdType  string        `xml:"CmdType"`
	SN       int           `xml:"SN"`
	DeviceID string        `xml:"DeviceID"`
	Status   string        `xml:"Status"` // 国标标准格式：Status 在根级别
	Info     KeepaliveInfo `xml:"Info"`   // 部分厂商（如海康）将 Status 嵌套在 Info 中
}

// KeepaliveInfo 部分厂商心跳包中 Info 子元素
type KeepaliveInfo struct {
	Status string `xml:"Status"`
}

func (g *GB28181API) sipMessageKeepalive(ctx *sip.Context) {
	var msg MessageNotify
	if err := sip.XMLDecode(ctx.Request.Body(), &msg); err != nil {
		ctx.Log.Error("Message Unmarshal xml err", "err", err)
		return
	}

	// 兼容两种 Status 位置：国标根级别 / 部分厂商嵌套在 Info 中
	status := msg.Status
	if status == "" {
		status = msg.Info.Status
	}

	// 程序重启后内存丢失，收到 keepalive 时补上
	g.svr.memoryStorer.LoadOrStore(ctx.DeviceID, &Device{
		conn:   ctx.Request.GetConnection(),
		source: ctx.Source,
		to:     ctx.To,
		region: ctx.To.URI.Host(),
	})

	if err := g.svr.memoryStorer.Change(ctx.DeviceID, func(d *ipc.Device) error {
		d.KeepaliveAt = orm.Now()
		d.IsOnline = status == "OK" || status == "ON"
		d.Address = ctx.Source.String()
		d.Transport = ctx.Source.Network()
		return nil
	}, func(d *Device) {
		d.conn = ctx.Request.GetConnection()
		d.source = ctx.Source
		d.to = ctx.To
		d.region = ctx.To.URI.Host()
	}); err != nil {
		ctx.Log.Error("keepalive", "err", err)
	}

	ctx.String(200, "OK")
}