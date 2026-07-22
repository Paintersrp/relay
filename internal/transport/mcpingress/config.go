package mcpingress

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"relay/internal/transport/transporttrace"
)

const (
	upstreamBaseEnv       = "RELAY_MCP_INGRESS_UPSTREAM_BASE_URL"
	upstreamTokenEnv      = "RELAY_MCP_INGRESS_UPSTREAM_BEARER_TOKEN"
	traceDirectoryEnv     = "RELAY_MCP_TRACE_DIR"
	traceMaxAgeEnv        = "RELAY_MCP_TRACE_MAX_AGE"
	traceMaxBytesEnv      = "RELAY_MCP_TRACE_MAX_BYTES"
	defaultTraceDirectory = "data/transport/mcp-traces"
)

type Config struct {
	Mappings       []MappingSpec
	Bearer         BearerInjector
	TraceDirectory string
	Retention      transporttrace.RetentionPolicy
}

type ConfigError struct {
	Code      string
	MappingID MappingID
	Field     string
}

func (err *ConfigError) Error() string {
	if err == nil {
		return "MCP ingress configuration is invalid"
	}
	if err.MappingID != "" {
		return fmt.Sprintf("%s: mapping %s field %s", err.Code, err.MappingID, err.Field)
	}
	return fmt.Sprintf("%s: field %s", err.Code, err.Field)
}

func LoadConfig(getenv func(string) string, defaultUpstreamBase string, descriptors []RouteDescriptor) (Config, error) {
	if getenv == nil {
		return Config{}, &ConfigError{Code: "MCP_INGRESS_MAPPING_SET_INVALID", Field: "environment"}
	}
	if len(descriptors) != len(mappingCatalog) {
		return Config{}, &ConfigError{Code: "MCP_INGRESS_MAPPING_SET_INVALID", Field: "route_descriptors"}
	}
	descriptorsByPath := make(map[string]RouteDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if !validRouteDescriptor(descriptor) {
			return Config{}, &ConfigError{Code: "MCP_INGRESS_ROUTE_MISMATCH", MappingID: descriptor.MappingID, Field: "route_descriptor"}
		}
		if _, exists := descriptorsByPath[descriptor.RoutePath]; exists {
			return Config{}, &ConfigError{Code: "MCP_INGRESS_MAPPING_SET_INVALID", MappingID: descriptor.MappingID, Field: "route_path"}
		}
		descriptorsByPath[descriptor.RoutePath] = descriptor
	}
	baseValue := strings.TrimSpace(getenv(upstreamBaseEnv))
	if baseValue == "" {
		baseValue = strings.TrimSpace(defaultUpstreamBase)
	}
	base, err := parseUpstreamBase(baseValue)
	if err != nil {
		return Config{}, &ConfigError{Code: "MCP_INGRESS_UPSTREAM_INVALID", Field: upstreamBaseEnv}
	}
	mappings := make([]MappingSpec, 0, len(mappingCatalog))
	listeners := map[string]MappingID{}
	for _, entry := range mappingCatalog {
		descriptor, ok := descriptorsByPath[entry.RoutePath]
		if !ok || descriptor.MappingID != entry.ID || descriptor.PublicSurface != string(entry.ID) {
			return Config{}, &ConfigError{Code: "MCP_INGRESS_ROUTE_MISMATCH", MappingID: entry.ID, Field: "route_descriptor"}
		}
		addressValue := strings.TrimSpace(getenv(entry.ListenerEnv))
		if addressValue == "" {
			addressValue = entry.DefaultAddress
		}
		address, err := ParsePrivateAddress(addressValue)
		if err != nil {
			return Config{}, &ConfigError{Code: "MCP_INGRESS_LISTENER_INVALID", MappingID: entry.ID, Field: entry.ListenerEnv}
		}
		if existing, duplicate := listeners[address.String()]; duplicate {
			return Config{}, &ConfigError{Code: "MCP_INGRESS_DUPLICATE_LISTENER", MappingID: existing, Field: address.String()}
		}
		listeners[address.String()] = entry.ID
		upstreamURL := *base
		upstreamURL.Path = entry.RoutePath
		mappings = append(mappings, MappingSpec{
			ID: entry.ID, RoutePath: entry.RoutePath, PublicSurface: descriptor.PublicSurface,
			PublicSurfaceManifestSHA256: descriptor.PublicSurfaceManifestSHA256,
			ToolIdentities:              append([]ToolIdentity(nil), descriptor.ToolIdentities...), Listener: address,
			Upstream: UpstreamTarget{value: upstreamURL},
		})
	}
	retention, err := loadRetention(getenv)
	if err != nil {
		return Config{}, err
	}
	traceDirectory := strings.TrimSpace(getenv(traceDirectoryEnv))
	if traceDirectory == "" {
		traceDirectory = defaultTraceDirectory
	}
	if strings.ContainsRune(traceDirectory, '\x00') {
		return Config{}, &ConfigError{Code: "MCP_INGRESS_RETENTION_INVALID", Field: traceDirectoryEnv}
	}
	traceDirectory = filepath.Clean(traceDirectory)
	return Config{Mappings: mappings, Bearer: NewBearerInjector(getenv(upstreamTokenEnv)), TraceDirectory: traceDirectory, Retention: retention}, nil
}

func validRouteDescriptor(descriptor RouteDescriptor) bool {
	if descriptor.RoutePath == "" || descriptor.PublicSurface == "" || !validLowerHex(descriptor.PublicSurfaceManifestSHA256, 64) || len(descriptor.ToolIdentities) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(descriptor.ToolIdentities))
	for _, identity := range descriptor.ToolIdentities {
		if identity.AdvertisedName == "" || identity.InternalToolName == "" || identity.InternalRoutePath == "" || identity.SurfaceContract == "" ||
			identity.StandingAuthorityRepository == "" || identity.StandingAuthorityCommitOID == "" || identity.StandingAuthorityPath == "" || identity.StandingAuthorityBlobOID == "" ||
			!validLowerHex(identity.RouteManifestSHA256, 64) || !validLowerHex(identity.StandingAuthorityCommitOID, 40) || !validLowerHex(identity.StandingAuthorityBlobOID, 40) {
			return false
		}
		if _, duplicate := seen[identity.AdvertisedName]; duplicate {
			return false
		}
		seen[identity.AdvertisedName] = struct{}{}
	}
	return true
}

func ParsePrivateAddress(value string) (PrivateAddress, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return PrivateAddress{}, fmt.Errorf("private listener is empty")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil || host == "" || portText == "" {
		return PrivateAddress{}, fmt.Errorf("private listener must contain an IP literal and port")
	}
	if strings.Contains(host, "%") {
		return PrivateAddress{}, fmt.Errorf("private listener zones are not supported")
	}
	ip := net.ParseIP(host)
	if !allowedPrivateIP(ip) {
		return PrivateAddress{}, fmt.Errorf("private listener host is not private")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return PrivateAddress{}, fmt.Errorf("private listener port is invalid")
	}
	return PrivateAddress{value: net.JoinHostPort(ip.String(), strconv.Itoa(port))}, nil
}

func parseUpstreamBase(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("upstream base is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("upstream scheme is invalid")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || parsed.RawPath != "" {
		return nil, fmt.Errorf("upstream base must not contain credentials, path, query, or fragment")
	}
	host := parsed.Hostname()
	if strings.Contains(host, "%") || !allowedPrivateIP(net.ParseIP(host)) {
		return nil, fmt.Errorf("upstream host is not private")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("upstream port is invalid")
	}
	parsed.Host = net.JoinHostPort(net.ParseIP(host).String(), strconv.Itoa(port))
	return parsed, nil
}

func loadRetention(getenv func(string) string) (transporttrace.RetentionPolicy, error) {
	policy := transporttrace.RetentionPolicy{MaxAge: transporttrace.DefaultMaxAge, MaxBytes: transporttrace.DefaultMaxBytes}
	if value := strings.TrimSpace(getenv(traceMaxAgeEnv)); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return transporttrace.RetentionPolicy{}, &ConfigError{Code: "MCP_INGRESS_RETENTION_INVALID", Field: traceMaxAgeEnv}
		}
		policy.MaxAge = parsed
	}
	if value := strings.TrimSpace(getenv(traceMaxBytesEnv)); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return transporttrace.RetentionPolicy{}, &ConfigError{Code: "MCP_INGRESS_RETENTION_INVALID", Field: traceMaxBytesEnv}
		}
		policy.MaxBytes = parsed
	}
	if err := policy.Validate(); err != nil {
		return transporttrace.RetentionPolicy{}, &ConfigError{Code: "MCP_INGRESS_RETENTION_INVALID", Field: "retention"}
	}
	return policy, nil
}

func allowedPrivateIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func validLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
