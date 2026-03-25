package dl

import (
	"context"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/peers"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/downloader"
	"github.com/iyear/tdl/core/logctx"
	"github.com/iyear/tdl/core/storage"
	"github.com/iyear/tdl/core/tclient"
	"github.com/iyear/tdl/pkg/prog"
	"github.com/iyear/tdl/pkg/utils"
	jdbprog "github.com/jedib0t/go-pretty/v6/progress"
)

type Options struct {
	Dir             string
	RewriteExt      bool
	SkipSame        bool
	Template        string
	URLs            []string
	Files           []string
	Dialogs         MessagesIterator
	ProgressAdapter ProgressAdapter
	PeersManager    *peers.Manager
	Threads         int
	Concurrency     int
	Include         []string
	Exclude         []string
	Desc            bool
	Takeout         bool
	Group           bool // auto detect grouped message

	// resume opts
	Continue, Restart bool

	// serve
	Serve bool
	Port  int
}

const (
	FlagPoolSize         = 8
	FlagReconnectTimeout = 5 * time.Minute
	FlagDelay            = 0
	FlagThreads          = 4
	FlagLimit            = 2
)

func Run(ctx context.Context, c *telegram.Client, kvd storage.Storage, opts Options) (rerr error) {
	pool := dcpool.NewPool(c,
		int64(FlagPoolSize),
		tclient.NewDefaultMiddlewares(ctx, FlagReconnectTimeout)...)
	defer multierr.AppendInvoke(&rerr, multierr.Close(pool))

	var manager *peers.Manager
	if opts.PeersManager != nil {
		manager = opts.PeersManager
	} else {
		manager = peers.Options{Storage: storage.NewPeers(kvd)}.Build(pool.Default(ctx))
	}

	it, err := newIter(pool, manager, opts.Dialogs, opts, FlagDelay)
	if err != nil {
		return err
	}

	var dlProgress jdbprog.Writer
	var progress downloader.Progress

	if opts.ProgressAdapter != nil {
		opts.ProgressAdapter.SetIterator(it)
		progress = opts.ProgressAdapter
	} else {
		dlProgress = prog.New(utils.Byte.FormatBinaryBytes)
		dlProgress.SetNumTrackersExpected(it.Total())
		prog.EnablePS(ctx, dlProgress)
		progress = newProgress(dlProgress, it, opts)
	}

	threads := FlagThreads
	if opts.Threads > 0 {
		threads = opts.Threads
	}

	options := downloader.Options{
		Pool:     pool,
		Threads:  threads,
		Iter:     it,
		Progress: progress,
	}
	limit := FlagLimit
	if opts.Concurrency > 0 {
		limit = opts.Concurrency
	}

	logctx.From(ctx).Info("Start download",
		zap.String("dir", opts.Dir),
		zap.Bool("rewrite_ext", opts.RewriteExt),
		zap.Bool("skip_same", opts.SkipSame),
		zap.Int("threads", options.Threads),
		zap.Int("limit", limit))

	if dlProgress != nil {
		go dlProgress.Render()
		defer prog.Wait(ctx, dlProgress)
	}

	return downloader.New(options).Download(ctx, limit)
}
