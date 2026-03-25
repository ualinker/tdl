package dl

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/logctx"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/downloader"
	"github.com/iyear/tdl/core/tmedia"
	"github.com/iyear/tdl/core/util/fsutil"
	"github.com/iyear/tdl/core/util/tutil"
	"github.com/iyear/tdl/pkg/tplfunc"
	"github.com/iyear/tdl/pkg/utils"
)

const tempExt = ".tmp"

type fileTemplate struct {
	DialogID     int64
	MessageID    int
	MessageDate  int64
	FileName     string
	FileCaption  string
	FileSize     string
	DownloadDate int64
}

type iter struct {
	pool     dcpool.Pool
	manager  *peers.Manager
	messages MessagesIterator
	tpl      *template.Template
	include  map[string]struct{}
	exclude  map[string]struct{}
	opts     Options
	delay    time.Duration

	mu          *sync.Mutex
	finished    map[int]struct{}
	fingerprint string
	i, j        int
	counter     *atomic.Int64
	elem        chan downloader.Elem
	err         error
}

type MessagesIterator interface {
	Total() int
	Next() (bool, error)
	Peer() tg.InputPeerClass
	MessageID() int
	Reset()
}

func newIter(pool dcpool.Pool, manager *peers.Manager, messages MessagesIterator,
	opts Options, delay time.Duration,
) (*iter, error) {
	tpl, err := template.New("dl").
		Funcs(tplfunc.FuncMap(tplfunc.All...)).
		Parse(opts.Template)
	if err != nil {
		return nil, errors.Wrap(err, "parse template")
	}

	if messages == nil {
		return nil, errors.Errorf("empty MessagesIterator")
	}

	// if msgs is empty, return error to avoid range out of index
	if messages.Total() == 0 {
		return nil, errors.Errorf("you must specify at least one message")
	}

	// include and exclude
	includeMap := filterMap(opts.Include, fsutil.AddPrefixDot)
	excludeMap := filterMap(opts.Exclude, fsutil.AddPrefixDot)

	return &iter{
		pool:     pool,
		manager:  manager,
		messages: messages,
		opts:     opts,
		include:  includeMap,
		exclude:  excludeMap,
		tpl:      tpl,
		delay:    delay,

		mu:          &sync.Mutex{},
		finished:    make(map[int]struct{}),
		fingerprint: "",
		i:           0,
		j:           0,
		counter:     atomic.NewInt64(-1),
		elem:        make(chan downloader.Elem, 10), // grouped message buffer
		err:         nil,
	}, nil
}

func (i *iter) Next(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		i.err = ctx.Err()
		return false
	default:
	}

	// if delay is set, sleep for a while for each iteration
	if i.delay > 0 && (i.i+i.j) > 0 { // skip first delay
		time.Sleep(i.delay)
	}
	if len(i.elem) > 0 { // there are messages(grouped) in channel that not processed
		return true
	}

	for {
		ok, skip := i.process(ctx)
		if skip {
			continue
		}

		return ok
	}
}

func (i *iter) process(ctx context.Context) (ret bool, skip bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// end of iteration or error occurred
	next, err := i.messages.Next()
	if err != nil {
		i.err = err
	}
	if !next || i.err != nil {
		return false, false
	}

	peer, msg := i.messages.Peer(), i.messages.MessageID()

	from, err := i.manager.FromInputPeer(ctx, peer)
	if err != nil {
		i.err = errors.Wrap(err, "resolve from input peer")
		return false, false
	}
	message, err := tutil.GetSingleMessage(ctx, i.pool.Default(ctx), peer, msg)
	if err != nil {
		logctx.From(ctx).Error("unable to resolve message", zap.String("peer", peer.String()), zap.Int("message_id", msg))
		return false, true
	}

	if _, ok := message.GetGroupedID(); ok && i.opts.Group {
		return i.processGrouped(ctx, message, from)
	}
	return i.processSingle(ctx, message, from)
}

func (i *iter) processSingle(ctx context.Context, message *tg.Message, from peers.Peer) (bool, bool) {
	item, ok := tmedia.GetMedia(message)
	if !ok {
		logctx.From(ctx).Error("can not get media", zap.Int64("from_id", from.ID()), zap.Int("message_id", message.ID))
		return false, true
	}

	// process include and exclude
	ext := filepath.Ext(item.Name)
	if _, ok = i.include[ext]; len(i.include) > 0 && !ok {
		return false, true
	}
	if _, ok = i.exclude[ext]; len(i.exclude) > 0 && ok {
		return false, true
	}

	toName := bytes.Buffer{}
	err := i.tpl.Execute(&toName, &fileTemplate{
		DialogID:     from.ID(),
		MessageID:    message.ID,
		MessageDate:  int64(message.Date),
		FileName:     item.Name,
		FileCaption:  message.Message,
		FileSize:     utils.Byte.FormatBinaryBytes(item.Size),
		DownloadDate: time.Now().Unix(),
	})
	if err != nil {
		i.err = errors.Wrap(err, "execute template")
		return false, false
	}

	if i.opts.SkipSame {
		if stat, err := os.Stat(filepath.Join(i.opts.Dir, toName.String())); err == nil {
			if fsutil.GetNameWithoutExt(toName.String()) == fsutil.GetNameWithoutExt(stat.Name()) &&
				stat.Size() == item.Size {
				slog.Info("dl/iter: already exists", "name", toName.String())
				return false, true
			}
		}
	}

	filename := fmt.Sprintf("%s%s", toName.String(), tempExt)
	path := filepath.Join(i.opts.Dir, filename)

	// #113. If path contains dirs, create it. So now we support nested dirs.
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		i.err = errors.Wrap(err, "create dir")
		return false, false
	}

	to, err := os.Create(path)
	if err != nil {
		i.err = errors.Wrap(err, "create file")
		return false, false
	}

	i.elem <- &iterElem{
		id: int(i.counter.Inc()),

		from:    from,
		fromMsg: message,
		file:    item,

		to: to,

		opts: i.opts,
	}

	return true, false
}

func (i *iter) processGrouped(ctx context.Context, message *tg.Message, from peers.Peer) (bool, bool) {
	grouped, err := tutil.GetGroupedMessages(ctx, i.pool.Default(ctx), from.InputPeer(), message)
	if err != nil {
		i.err = errors.Wrapf(err, "resolve grouped message %d/%d", from.ID(), message.ID)
		return false, false
	}

	var hasRet, hasSkip bool

	for _, msg := range grouped {
		ret, skip := i.processSingle(ctx, msg, from)
		hasRet = hasRet || ret
		hasSkip = hasSkip || skip
	}

	if hasRet {
		return true, false
	}

	return false, hasSkip
}

func (i *iter) Value() downloader.Elem {
	return <-i.elem
}

func (i *iter) Err() error {
	return i.err
}

func (i *iter) Total() int {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.messages.Total()
}

func (i *iter) Finished() map[int]struct{} {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.finished
}

func (i *iter) Finish(id int) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.finished[id] = struct{}{}
}

func filterMap(data []string, keyFn func(key string) string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, v := range data {
		m[keyFn(v)] = struct{}{}
	}
	return m
}
