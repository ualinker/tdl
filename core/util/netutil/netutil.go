package netutil

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/dcs"
	"github.com/iyear/connectproxy"
	"golang.org/x/net/proxy"
)

func init() {
	connectproxy.Register(&connectproxy.Config{
		InsecureSkipVerify: true,
	})
}

func NewProxy(proxyUrl string) (proxy.ContextDialer, error) {
	u, err := url.Parse(proxyUrl)
	if err != nil {
		return nil, errors.Wrap(err, "parse proxy url")
	}
	dialer, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil, errors.Wrap(err, "proxy from url")
	}

	if d, ok := dialer.(proxy.ContextDialer); ok {
		return d, nil
	}

	return nil, errors.New("proxy dialer is not ContextDialer")
}

func NewResolver(proxyUrl string) (dcs.Resolver, error) {
	if proxyUrl == "" {
		return dcs.Plain(dcs.PlainOptions{Dial: proxy.Direct.DialContext}), nil
	}

	if strings.HasPrefix(proxyUrl, "tg://proxy") || strings.HasPrefix(proxyUrl, "t.me/proxy") {
		matches := regexp.MustCompile(`server=(.+?)&port=(\d+)&secret=(.+)`).FindStringSubmatch(proxyUrl)
		addr := fmt.Sprintf("%s:%s", matches[1], matches[2])

		secret, err := hex.DecodeString(matches[3])
		if err != nil {
			return nil, errors.Wrap(err, "decode secret")
		}

		resolver, err := dcs.MTProxy(addr, secret, dcs.MTProxyOptions{})
		if err != nil {
			return nil, errors.Wrap(err, "create resolver")
		}

		return resolver, nil
	}

	ctxDialer, err := NewProxy(proxyUrl)
	if err != nil {
		return nil, errors.Wrap(err, "create resolver")
	}

	return dcs.Plain(dcs.PlainOptions{Dial: ctxDialer.DialContext}), nil
}
