package dl

import (
	"strings"

	"github.com/gotd/td/telegram/peers"
	"github.com/iyear/tdl/core/downloader"
)

type ProgressElem struct {
	From      peers.Peer
	MessageID int
	FileName  string
	Size      int64
}

type ProgressHandler interface {
	OnAdd(elem ProgressElem)
	OnDownload(elem ProgressElem, state downloader.ProgressState)
	OnDone(elem ProgressElem, err error)
}

type progressAdapter struct {
	handler ProgressHandler
}

func NewProgressAdapter(handler ProgressHandler) downloader.Progress {
	return &progressAdapter{handler}
}

func (p *progressAdapter) OnAdd(elem downloader.Elem) {
	p.handler.OnAdd(p.progressElem(elem))
}

func (p *progressAdapter) OnDownload(elem downloader.Elem, state downloader.ProgressState) {
	p.handler.OnDownload(p.progressElem(elem), state)
}

func (p *progressAdapter) OnDone(elem downloader.Elem, err error) {
	p.handler.OnDone(p.progressElem(elem), err)
}

func (p *progressAdapter) progressElem(elem downloader.Elem) ProgressElem {
	el := elem.(*iterElem)
	return ProgressElem{
		From:      el.from,
		MessageID: el.fromMsg.ID,
		FileName:  strings.TrimSuffix(el.to.Name(), tempExt),
		Size:      el.Size(),
	}
}
