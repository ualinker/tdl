package dl

import (
	"io"

	"github.com/gotd/td/telegram/peers"
	"github.com/iyear/tdl/core/downloader"
)

type Iterator interface {
	Finish(id int)
	Finished() map[int]struct{}
}

type ProgressAdapter interface {
	downloader.Progress
	SetIterator(Iterator)
}

type ProgressElem struct {
	Peer      peers.Peer
	MessageID int
	To        io.WriterAt
	ID        int
}

type ProgressHandler interface {
	OnAdd(elem ProgressElem)
	OnDownload(elem ProgressElem, state downloader.ProgressState)
	OnDone(elem ProgressElem, err error)

	SetIterator(Iterator)
}

type progressAdapter struct {
	handler ProgressHandler
}

func NewProgressAdapter(handler ProgressHandler) ProgressAdapter {
	return &progressAdapter{handler: handler}
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

func (p *progressAdapter) SetIterator(iter Iterator) {
	p.handler.SetIterator(iter)
}

func (p *progressAdapter) progressElem(elem downloader.Elem) ProgressElem {
	el := elem.(*iterElem)
	return ProgressElem{
		Peer:      el.from,
		MessageID: el.fromMsg.ID,
		To:        el.to,
		ID:        el.id,
	}
}
