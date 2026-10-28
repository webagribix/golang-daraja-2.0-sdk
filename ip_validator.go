package mpesa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// DefaultSafaricomIPRanges contains the official production IP addresses and CIDR subnets
// utilized by the Safaricom Daraja API Gateway for delivering webhook callbacks.
var DefaultSafaricomIPRanges = []string{
	"196.201.214.200",
	"196.201.214.206",
	"196.201.214.207",
	"196.201.214.208",
	"196.201.213.114",
	"196.201.213.44",
	"196.201.213.41",
	"196.201.212.127",
	"196.201.212.129",
	"196.201.212.136",
	"196.201.212.138",
	"196.201.212.74",
	"196.201.212.69",
	"196.207.242.0/24",
	"196.201.214.200/28",
}

// DefaultProxyHeaders defines standard HTTP proxy headers inspected to extract real client IP addresses.
var DefaultProxyHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
	"CF-Connecting-IP",
	"True-Client-IP",
}

// ipRange represents an inclusive IP range [start, end].
type ipRange struct {
	start net.IP
	end   net.IP
}

func (r ipRange) contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	start4 := r.start.To4()
	end4 := r.end.To4()
	if ip4 != nil && start4 != nil && end4 != nil {
		return bytes.Compare(ip4, start4) >= 0 && bytes.Compare(ip4, end4) <= 0
	}

	ip16 := ip.To16()
	start16 := r.start.To16()
	end16 := r.end.To16()
	if ip16 != nil && start16 != nil && end16 != nil {
		return bytes.Compare(ip16, start16) >= 0 && bytes.Compare(ip16, end16) <= 0
	}
	return false
}

// IPValidator provides thread-safe IP address validation against authorized Safaricom callback
// subnets, individual IPs, CIDR blocks, and custom IP ranges.
type IPValidator struct {
	mu                sync.RWMutex
	singleIPs         map[string]bool
	ipNets            []*net.IPNet
	ipRanges          []ipRange
	rawEntries        []string
	trustProxyHeaders bool
	proxyHeaders      []string
}

// IPValidatorOption configures an IPValidator instance.
type IPValidatorOption func(*IPValidator)

// WithDefaultSafaricomIPs seeds the validator with Safaricom's official production IP ranges.
func WithDefaultSafaricomIPs() IPValidatorOption {
	return func(v *IPValidator) {
		_ = v.AddAllowedIPs(DefaultSafaricomIPRanges...)
	}
}

// WithAllowedIPs adds custom IP addresses, CIDR blocks, or range notations (e.g. "196.201.214.200-196.201.214.214").
func WithAllowedIPs(entries ...string) IPValidatorOption {
	return func(v *IPValidator) {
		_ = v.AddAllowedIPs(entries...)
	}
}

// WithTrustProxyHeaders configures whether to inspect reverse proxy headers (e.g. X-Forwarded-For).
func WithTrustProxyHeaders(trust bool) IPValidatorOption {
	return func(v *IPValidator) {
		v.trustProxyHeaders = trust
	}
}

// WithProxyHeaders configures custom proxy header names in order of evaluation priority.
func WithProxyHeaders(headers ...string) IPValidatorOption {
	return func(v *IPValidator) {
		if len(headers) > 0 {
			v.proxyHeaders = make([]string, len(headers))
			copy(v.proxyHeaders, headers)
		}
	}
}

// NewIPValidator creates a new thread-safe IP validator with the specified options.
func NewIPValidator(options ...IPValidatorOption) *IPValidator {
	v := &IPValidator{
		singleIPs:         make(map[string]bool),
		ipNets:            make([]*net.IPNet, 0),
		ipRanges:          make([]ipRange, 0),
		rawEntries:        make([]string, 0),
		trustProxyHeaders: true,
		proxyHeaders:      DefaultProxyHeaders,
	}

	for _, opt := range options {
		opt(v)
	}

	return v
}

// AddAllowedIPs dynamically adds one or more IP addresses, CIDRs, or ranges to the validator.
// Supported formats:
//   - Single IP: "196.201.214.200", "127.0.0.1", "::1"
//   - CIDR block: "196.207.242.0/24", "10.0.0.0/8"
//   - IP range: "196.201.214.200-196.201.214.214"
func (v *IPValidator) AddAllowedIPs(entries ...string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	for _, raw := range entries {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if strings.Contains(raw, "-") {
			parts := strings.SplitN(raw, "-", 2)
			start := parseIPString(parts[0])
			end := parseIPString(parts[1])
			if start == nil || end == nil {
				return fmt.Errorf("mpesa: invalid IP range notation %q", raw)
			}
			if (start.To4() != nil) != (end.To4() != nil) {
				return fmt.Errorf("mpesa: IP range %q cannot mix IPv4 and IPv6", raw)
			}
			if bytes.Compare(start, end) > 0 {
				return fmt.Errorf("mpesa: IP range start %q must be <= end %q", parts[0], parts[1])
			}
			v.ipRanges = append(v.ipRanges, ipRange{start: start, end: end})
			v.rawEntries = append(v.rawEntries, raw)
			continue
		}

		if strings.Contains(raw, "/") {
			_, ipNet, err := net.ParseCIDR(raw)
			if err != nil {
				return fmt.Errorf("mpesa: invalid CIDR block %q: %w", raw, err)
			}
			v.ipNets = append(v.ipNets, ipNet)
			v.rawEntries = append(v.rawEntries, raw)
			continue
		}

		ip := parseIPString(raw)
		if ip == nil {
			return fmt.Errorf("mpesa: invalid IP address %q", raw)
		}
		if ip4 := ip.To4(); ip4 != nil {
			v.singleIPs[ip4.String()] = true
		} else {
			v.singleIPs[ip.String()] = true
		}
		v.rawEntries = append(v.rawEntries, raw)
	}

	return nil
}

// SetAllowedIPs resets the validator and replaces all allowed IPs with the given list.
func (v *IPValidator) SetAllowedIPs(entries ...string) error {
	v.mu.Lock()
	v.singleIPs = make(map[string]bool)
	v.ipNets = make([]*net.IPNet, 0)
	v.ipRanges = make([]ipRange, 0)
	v.rawEntries = make([]string, 0)
	v.mu.Unlock()

	return v.AddAllowedIPs(entries...)
}

// GetAllowedIPs returns a copy of all registered raw IP/CIDR/range entries.
func (v *IPValidator) GetAllowedIPs() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	result := make([]string, len(v.rawEntries))
	copy(result, v.rawEntries)
	return result
}

// IsAllowedIP checks if the given IP address or remoteAddr string (e.g. "196.201.214.200:44321") is authorized.
func (v *IPValidator) IsAllowedIP(ipOrRemoteAddr string) bool {
	ip := parseIPString(ipOrRemoteAddr)
	if ip == nil {
		return false
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	// 1. Direct single IP match
	if ip4 := ip.To4(); ip4 != nil {
		if v.singleIPs[ip4.String()] {
			return true
		}
	}
	if v.singleIPs[ip.String()] {
		return true
	}

	// 2. CIDR subnet match
	for _, ipNet := range v.ipNets {
		if ipNet.Contains(ip) {
			return true
		}
	}

	// 3. IP range match
	for _, r := range v.ipRanges {
		if r.contains(ip) {
			return true
		}
	}

	return false
}

// GetClientIP extracts the resolved client IP from an incoming HTTP request, inspecting
// proxy headers (if enabled) or falling back to RemoteAddr.
func (v *IPValidator) GetClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	v.mu.RLock()
	trustProxy := v.trustProxyHeaders
	headers := v.proxyHeaders
	v.mu.RUnlock()

	if trustProxy {
		for _, header := range headers {
			val := r.Header.Get(header)
			if val == "" {
				continue
			}

			if strings.Contains(val, ",") {
				for _, part := range strings.Split(val, ",") {
					ip := parseIPString(part)
					if ip != nil {
						return ip.String()
					}
				}
			} else {
				ip := parseIPString(val)
				if ip != nil {
					return ip.String()
				}
			}
		}
	}

	ip := parseIPString(r.RemoteAddr)
	if ip != nil {
		return ip.String()
	}
	return ""
}

// VerifyRequest inspects an HTTP request and confirms its origin against the allowed IP list.
// Returns the resolved client IP on success, or an error if unauthorized.
func (v *IPValidator) VerifyRequest(r *http.Request) (string, error) {
	clientIP := v.GetClientIP(r)
	if clientIP == "" {
		return "", errors.New("mpesa: could not determine client IP address from request")
	}
	if !v.IsAllowedIP(clientIP) {
		return clientIP, fmt.Errorf("mpesa: forbidden callback from unauthorized IP %q", clientIP)
	}
	return clientIP, nil
}

// Middleware returns a standard net/http middleware that protects Daraja callback routes.
// Unauthorized requests receive an HTTP 403 Forbidden JSON response.
func (v *IPValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP, err := v.VerifyRequest(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":     "forbidden: unauthorized callback source IP",
				"client_ip": clientIP,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parseIPString extracts and parses an IP address from a string, safely stripping port numbers, IPv6 brackets, and whitespace.
func parseIPString(raw string) net.IP {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// Try splitting host:port
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}

	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	raw = strings.TrimSpace(raw)

	return net.ParseIP(raw)
}

var defaultGlobalValidator = NewIPValidator(WithDefaultSafaricomIPs())

// IsSafaricomIP returns true if the provided IP or host:port string belongs to official Safaricom Daraja IP ranges.
func IsSafaricomIP(ipOrRemoteAddr string) bool {
	return defaultGlobalValidator.IsAllowedIP(ipOrRemoteAddr)
}

// ValidateCallbackIP validates an incoming IP against official Safaricom IPs plus optional custom allowed IPs/CIDRs/ranges.
func ValidateCallbackIP(ipOrRemoteAddr string, additionalAllowed ...string) bool {
	if defaultGlobalValidator.IsAllowedIP(ipOrRemoteAddr) {
		return true
	}
	if len(additionalAllowed) == 0 {
		return false
	}
	v := NewIPValidator(WithAllowedIPs(additionalAllowed...))
	return v.IsAllowedIP(ipOrRemoteAddr)
}
