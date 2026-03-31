package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var resolveOutboundFetchIPs = net.DefaultResolver.LookupIPAddr
var allowLoopbackOutboundFetch bool

type outboundURLOptions struct {
	allowPrivateIPs bool
}

func validateOutboundFetchURL(ctx context.Context, rawURL string, allowedSchemes []string, opts outboundURLOptions) (*url.URL, error) {
	if strings.TrimSpace(rawURL) == "" || len(rawURL) > maxURLLength {
		return nil, fmt.Errorf("invalid URL length")
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("embedded credentials are not allowed")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("URL fragments are not allowed")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL missing hostname")
	}

	allowed := false
	for _, scheme := range allowedSchemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("URL scheme must be one of: %s", strings.Join(allowedSchemes, ", "))
	}

	if err := validateOutboundFetchHost(ctx, parsed.Hostname(), opts); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validateOutboundFetchHost(ctx context.Context, host string, opts outboundURLOptions) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("URL missing hostname")
	}

	switch strings.ToLower(host) {
	case "metadata.google.internal", "metadata.goog":
		return fmt.Errorf("metadata service host is not allowed")
	}

	if ip := net.ParseIP(host); ip != nil {
		return validateOutboundFetchIP(ip, opts)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	addrs, err := resolveOutboundFetchIPs(resolveCtx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("hostname %s did not resolve", host)
	}

	for _, addr := range addrs {
		if err := validateOutboundFetchIP(addr.IP, opts); err != nil {
			return err
		}
	}
	return nil
}

func validateOutboundFetchIP(ip net.IP, opts outboundURLOptions) error {
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}
	if ip.IsLoopback() {
		if allowLoopbackOutboundFetch {
			return nil
		}
		return fmt.Errorf("loopback addresses are not allowed")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local addresses are not allowed")
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast addresses are not allowed")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses are not allowed")
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("metadata service address is not allowed")
	}
	if !opts.allowPrivateIPs && isPrivateOutboundIP(ip) {
		return fmt.Errorf("private addresses are not allowed")
	}
	return nil
}

func isPrivateOutboundIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fe80::/10",
		"fc00::/7",
	}

	for _, cidr := range privateRanges {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

func sameOriginRedirectPolicy(allowedSchemes []string, opts outboundURLOptions) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		validated, err := validateOutboundFetchURL(req.Context(), req.URL.String(), allowedSchemes, opts)
		if err != nil {
			return err
		}
		origin := via[0].URL
		if !strings.EqualFold(validated.Scheme, origin.Scheme) || !strings.EqualFold(validated.Host, origin.Host) {
			return fmt.Errorf("redirects must stay on the same origin")
		}
		return nil
	}
}

func newRestrictedOutboundHTTPClient(timeout time.Duration, opts outboundURLOptions) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: sameOriginRedirectPolicy([]string{"https", "http"}, opts),
	}
}
