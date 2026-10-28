package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"my-vpn/internal/config"
)

// PortAvailable 通过实际 IPv4 bind 判断端口，避免只解析进程列表造成漏判。
func PortAvailable(listen string, port int) bool {
	listener, err := net.Listen("tcp4", net.JoinHostPort(listen, fmt.Sprint(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func (s *Service) RecommendPort(ctx context.Context) (int, string, error) {
	if PortAvailable("0.0.0.0", 443) {
		return 443, "", nil
	}
	occupied, _ := s.run(ctx, "ss", "-H", "-ltnp", "sport = :443")
	if occupied == "" {
		occupied = "TCP 443"
	}
	for _, port := range []int{8443, 9443, 10443} {
		if PortAvailable("0.0.0.0", port) {
			return port, strings.TrimSpace(occupied), nil
		}
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return 0, occupied, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, occupied, nil
}

func (s *Service) DiscoverAddress(ctx context.Context) (string, error) {
	for _, endpoint := range []string{"https://api4.ipify.org", "https://ipv4.icanhazip.com"} {
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		request, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
		response, err := s.HTTP.Do(request)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 128))
			_ = response.Body.Close()
			ip, parseErr := netip.ParseAddr(strings.TrimSpace(string(data)))
			if response.StatusCode == http.StatusOK && readErr == nil && parseErr == nil && config.PublicIPv4(ip) {
				cancel()
				return ip.String(), nil
			}
		}
		cancel()
	}
	return "", fmt.Errorf("public IPv4 discovery failed")
}

// ProbeTarget 验证 TLS 1.3、证书/SNI 和 HTTP/2 ALPN；这只是候选筛选，不能代替客户端连通测试。
func ProbeTarget(ctx context.Context, target, serverName string) error {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 6 * time.Second}, Config: &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: serverName, NextProtos: []string{"h2"},
	}}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	defer conn.Close()
	if conn.(*tls.Conn).ConnectionState().NegotiatedProtocol != "h2" {
		return fmt.Errorf("target does not negotiate HTTP/2")
	}
	return nil
}
