package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// safeClient is used for requests to user-supplied hosts (container
// registries, Git servers). It refuses loopback, link-local (including the
// cloud metadata address 169.254.169.254) and unspecified addresses, so a
// deployment config can't be used to probe the server itself.
var safeClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
					return errors.New("refusing to connect to a local address")
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	},
}

func safeGet(ctx context.Context, url string, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", "insta-deploy")
	return safeClient.Do(req)
}
