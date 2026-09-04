// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var lookupIP = net.LookupIP

func SetLookupIPForTest(fn func(host string) ([]net.IP, error)) func() {
	prev := lookupIP
	lookupIP = fn
	return func() { lookupIP = prev }
}

func Check(raw string, allowedHosts []string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("url must not contain credentials")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	if len(allowedHosts) > 0 && !hostAllowed(host, allowedHosts) {
		return fmt.Errorf("host %s is not in egress.allowed_hosts", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("host %s is not allowed", host)
		}
		return nil
	}
	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return fmt.Errorf("host %s resolves to a blocked address", host)
		}
	}
	return nil
}

func HTTPClient(timeout time.Duration, allowedHosts []string) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				if blockedIP(ip) {
					return nil, fmt.Errorf("blocked address %s", host)
				}
			} else if err := Check("https://"+host, allowedHosts); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func hostAllowed(host string, allowed []string) bool {
	h := strings.ToLower(host)
	for _, raw := range allowed {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if pattern == "" {
			continue
		}
		if strings.HasPrefix(pattern, ".") {
			if strings.HasSuffix(h, pattern) || h == strings.TrimPrefix(pattern, ".") {
				return true
			}
			continue
		}
		if h == pattern {
			return true
		}
	}
	return false
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.Equal(net.IPv4(169, 254, 169, 254)) || ip4.Equal(net.IPv4(100, 100, 100, 200)) {
			return true
		}
	}
	return false
}
