package downloader

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

const maxRedirects = 5

// ErrBlockedAddress is returned when a download would connect to a loopback,
// private, link-local or otherwise non-public address.
var ErrBlockedAddress = errors.New("refusing to connect to a non-public address")

// ValidateURL is the default check for image URLs and redirect targets:
// they must be absolute https URLs.
func ValidateURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-https URL %q", u.Redacted())
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL %q has no host", u.Redacted())
	}
	return nil
}

// NewHTTPClient returns a client for downloading images from untrusted URLs.
// Every redirect hop is checked with ValidateURL and the number of hops is
// capped. When no proxy is configured, connections to non-public IP
// addresses are refused, so post URLs can't reach services on the user's
// machine or network.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !proxyConfigured() {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   blockNonPublic,
		}
		transport.DialContext = dialer.DialContext
	}

	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return ValidateURL(req.URL)
}

// proxyConfigured reports whether HTTPS requests go through a proxy. With a
// proxy the dialer only ever sees the proxy's address, so the IP check would
// block a local proxy without protecting anything.
func proxyConfigured() bool {
	req, err := http.NewRequest(http.MethodGet, "https://i.redd.it/", nil)
	if err != nil {
		return false
	}
	proxyURL, err := http.ProxyFromEnvironment(req)
	return err == nil && proxyURL != nil
}

func blockNonPublic(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !isPublic(addr) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, addr)
	}
	return nil
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func isPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!cgnat.Contains(addr)
}
