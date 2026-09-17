package main

import (
	"bytes"
	"container/list"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

var version = "dev"

// --- Configuration ---

type proxyRoute struct {
	prefix string
	target *url.URL
}

type config struct {
	bindAddr           string
	onlyHTTPS          bool
	port               string
	tlsPort            string
	tlsCertDir         string
	tlsCertFile        string
	tlsKeyFile         string
	tlsSelfSignedHosts []string
	healthPath         string
	headersPath        string
	spaMode            bool
	showHeaders        bool
	rootDir            string
	cacheMaxSize       int64
	cacheMaxFileSize   int64
	proxyRoutes        []proxyRoute
	proxyTimeout       time.Duration
	proxyCAFile        string
	proxyInsecure      bool
}

func flagOrEnvStr(flagVal string, envName string, def string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return def
}

func flagOrEnvBool(flagVal bool, envName string) bool {
	if flagVal {
		return true
	}
	return parseBool(os.Getenv(envName))
}

func flagOrEnvInt64(flagVal int64, envName string, def int64) int64 {
	if flagVal >= 0 {
		return flagVal
	}
	if v := os.Getenv(envName); v != "" {
		return parseInt64(v)
	}
	return def
}

func parseProxyRoutes(specs []string) ([]proxyRoute, error) {
	var routes []proxyRoute
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.SplitN(spec, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid proxy route %q: expected /prefix=http://target", spec)
		}
		if parts[0] == "" || parts[0][0] != '/' {
			return nil, fmt.Errorf("invalid proxy prefix %q: must start with /", parts[0])
		}
		// "/" is the catch-all: it trims down to an empty prefix, which matches
		// every path and strips nothing.
		prefix := strings.TrimRight(parts[0], "/")
		target, err := url.Parse(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid proxy target %q: %w", parts[1], err)
		}
		if target.Scheme != "http" && target.Scheme != "https" {
			return nil, fmt.Errorf("invalid proxy target %q: scheme must be http or https", parts[1])
		}
		routes = append(routes, proxyRoute{prefix: prefix, target: target})
	}

	// The longest prefix wins, so a catch-all never shadows a specific route
	// regardless of the order the routes were given in.
	sort.SliceStable(routes, func(i, j int) bool {
		return len(routes[i].prefix) > len(routes[j].prefix)
	})

	return routes, nil
}

// routePrefix is how a prefix is displayed; the catch-all is stored as "".
func routePrefix(prefix string) string {
	if prefix == "" {
		return "/"
	}
	return prefix
}

type proxyFlags []string

func (p *proxyFlags) String() string { return strings.Join(*p, ",") }
func (p *proxyFlags) Set(val string) error {
	*p = append(*p, val)
	return nil
}

func loadConfig() config {
	var (
		fBind          string
		fOnlyHTTPS     bool
		fPort          string
		fTlsPort       string
		fTlsCertDir    string
		fTlsCertFile   string
		fTlsKeyFile    string
		fTlsSSHosts    string
		fHealthPath    string
		fHeadersPath   string
		fSpa           bool
		fShowHeaders   bool
		fRootDir       string
		fCacheMax      int64
		fCacheMaxFile  int64
		fVersion       bool
		fProxy         proxyFlags
		fProxyTimeout  int
		fProxyCA       string
		fProxyInsecure bool
	)

	flag.StringVar(&fBind, "bind", "", "IP address to listen on (env: BIND_ADDRESS, default: 0.0.0.0)")
	flag.BoolVar(&fOnlyHTTPS, "only-https", false, "Never start the plain HTTP listener, even when a port is set (env: ONLY_HTTPS)")
	flag.StringVar(&fPort, "port", "", "HTTP listening port (env: PORT, disabled if not set)")
	flag.StringVar(&fTlsPort, "tls-port", "", "HTTPS listening port (env: TLS_PORT, default: 8443)")
	flag.StringVar(&fTlsCertDir, "tls-cert-dir", "", "TLS certificate directory (env: TLS_CERT_DIR, default: /certs as root, ~/.static-httpserver/certs otherwise)")
	flag.StringVar(&fTlsCertFile, "tls-cert-file", "", "TLS certificate file, overrides --tls-cert-dir (env: TLS_CERT_FILE)")
	flag.StringVar(&fTlsKeyFile, "tls-key-file", "", "TLS private key file, overrides --tls-cert-dir (env: TLS_KEY_FILE)")
	flag.StringVar(&fTlsSSHosts, "tls-selfsigned-hosts", "", "Extra hostnames/IPs for the generated self-signed certificate, comma-separated (env: TLS_SELFSIGNED_HOSTS)")
	flag.StringVar(&fHealthPath, "health-path", "", "Path of the health endpoint (env: HEALTH_PATH, default: /_health)")
	flag.StringVar(&fHeadersPath, "headers-path", "", "Path of the request headers endpoint (env: HEADERS_PATH, default: /_headers)")
	flag.BoolVar(&fSpa, "spa", false, "Enable SPA mode (env: SPA_MODE)")
	flag.BoolVar(&fShowHeaders, "show-headers", false, "Show request headers on parking page (env: SHOW_HEADERS)")
	flag.StringVar(&fRootDir, "root-dir", "", "Root directory for static files (env: ROOT_DIR, required)")
	flag.Int64Var(&fCacheMax, "cache-max-size", -1, "Max cache size in bytes, 0 to disable (env: CACHE_MAX_SIZE, default: 50000000)")
	flag.Int64Var(&fCacheMaxFile, "cache-max-file", -1, "Max file size to cache in bytes (env: CACHE_MAX_FILE_SIZE, default: 5000000)")
	flag.BoolVar(&fVersion, "version", false, "Print version and exit")
	flag.Var(&fProxy, "proxy", "Proxy route as /prefix=http://target (repeatable, env: PROXY_ROUTES comma-separated)")
	flag.IntVar(&fProxyTimeout, "proxy-timeout", 0, "Proxy upstream timeout in seconds (env: PROXY_TIMEOUT, default: 30)")
	flag.StringVar(&fProxyCA, "proxy-ca", "", "CA certificate file for verifying proxy backend TLS (env: PROXY_CA_FILE)")
	flag.BoolVar(&fProxyInsecure, "proxy-insecure", false, "Skip TLS verification for proxy backends — use only on trusted networks (env: PROXY_INSECURE)")
	flag.Parse()

	if fVersion {
		fmt.Printf("static-httpserver %s\n", version)
		os.Exit(0)
	}

	rootDir := flagOrEnvStr(fRootDir, "ROOT_DIR", "")
	if rootDir == "" {
		fmt.Fprintln(os.Stderr, "Error: --root-dir flag or ROOT_DIR environment variable is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse proxy routes from flags or env
	var proxySpecs []string
	if len(fProxy) > 0 {
		proxySpecs = fProxy
	} else if v := os.Getenv("PROXY_ROUTES"); v != "" {
		proxySpecs = strings.Split(v, ",")
	}

	routes, err := parseProxyRoutes(proxySpecs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	timeout := 30
	if fProxyTimeout > 0 {
		timeout = fProxyTimeout
	} else if v := os.Getenv("PROXY_TIMEOUT"); v != "" {
		if t, err := strconv.Atoi(v); err == nil && t > 0 {
			timeout = t
		}
	}

	healthPath := flagOrEnvStr(fHealthPath, "HEALTH_PATH", "/_health")
	headersPath := flagOrEnvStr(fHeadersPath, "HEADERS_PATH", "/_headers")
	for name, path := range map[string]string{"--health-path": healthPath, "--headers-path": headersPath} {
		if path[0] != '/' {
			fmt.Fprintf(os.Stderr, "Error: invalid %s %q: must start with /\n", name, path)
			os.Exit(1)
		}
	}

	onlyHTTPS := flagOrEnvBool(fOnlyHTTPS, "ONLY_HTTPS")
	httpPort := flagOrEnvStr(fPort, "PORT", "")
	if onlyHTTPS && httpPort != "" {
		log.Printf("HTTP port %s ignored: --only-https (env: ONLY_HTTPS) is set", httpPort)
		httpPort = ""
	}

	bindAddr := flagOrEnvStr(fBind, "BIND_ADDRESS", "0.0.0.0")
	if net.ParseIP(bindAddr) == nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --bind address %q: expected an IP address\n", bindAddr)
		os.Exit(1)
	}

	tlsCertFile := flagOrEnvStr(fTlsCertFile, "TLS_CERT_FILE", "")
	tlsKeyFile := flagOrEnvStr(fTlsKeyFile, "TLS_KEY_FILE", "")
	if (tlsCertFile == "") != (tlsKeyFile == "") {
		fmt.Fprintln(os.Stderr, "Error: --tls-cert-file and --tls-key-file must be used together")
		os.Exit(1)
	}

	proxyCAFile := flagOrEnvStr(fProxyCA, "PROXY_CA_FILE", "")
	proxyInsecure := flagOrEnvBool(fProxyInsecure, "PROXY_INSECURE")
	if proxyCAFile != "" && proxyInsecure {
		log.Printf("WARNING: --proxy-ca and --proxy-insecure are both set; --proxy-ca takes precedence")
		proxyInsecure = false
	}

	return config{
		bindAddr:           bindAddr,
		onlyHTTPS:          onlyHTTPS,
		port:               httpPort,
		tlsPort:            flagOrEnvStr(fTlsPort, "TLS_PORT", "8443"),
		tlsCertDir:         flagOrEnvStr(fTlsCertDir, "TLS_CERT_DIR", defaultCertDir()),
		tlsCertFile:        tlsCertFile,
		tlsKeyFile:         tlsKeyFile,
		tlsSelfSignedHosts: splitList(flagOrEnvStr(fTlsSSHosts, "TLS_SELFSIGNED_HOSTS", "")),
		healthPath:         healthPath,
		headersPath:        headersPath,
		spaMode:            flagOrEnvBool(fSpa, "SPA_MODE"),
		showHeaders:        flagOrEnvBool(fShowHeaders, "SHOW_HEADERS"),
		rootDir:            rootDir,
		cacheMaxSize:       flagOrEnvInt64(fCacheMax, "CACHE_MAX_SIZE", 50000000),
		cacheMaxFileSize:   flagOrEnvInt64(fCacheMaxFile, "CACHE_MAX_FILE_SIZE", 5000000),
		proxyRoutes:        routes,
		proxyTimeout:       time.Duration(timeout) * time.Second,
		proxyCAFile:        proxyCAFile,
		proxyInsecure:      proxyInsecure,
	}
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// --- TLS ---

// defaultCertDir keeps certificates in /certs for root (the container case) and in
// the user's own directory otherwise, where an unprivileged process can write them.
func defaultCertDir() string {
	if os.Geteuid() == 0 {
		return "/certs"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".static-httpserver", "certs")
	}
	return "/certs"
}

// A persisted self-signed certificate is renewed once it gets this close to expiring.
var selfSignedRenewBefore = 30 * 24 * time.Hour

func selfSignedPaths(certDir string) (string, string) {
	return filepath.Join(certDir, "selfsigned-cert.pem"), filepath.Join(certDir, "selfsigned-key.pem")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// bindHosts returns the addresses the certificate needs to cover for clients to
// reach the server by IP. A wildcard bind listens on every interface, so every
// routable address of the machine is a valid way in.
func bindHosts(bindAddr string) []string {
	ip := net.ParseIP(bindAddr)
	if ip != nil && !ip.IsUnspecified() {
		return []string{ip.String()}
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("TLS could not list interface addresses: %v", err)
		return nil
	}

	var hosts []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsLinkLocalUnicast() {
			continue
		}
		hosts = append(hosts, ipNet.IP.String())
	}
	return hosts
}

// selfSignedHosts returns the names the generated certificate must be valid for:
// the well-known local names, the addresses the server listens on, and whatever
// the user asked for.
func selfSignedHosts(bindAddr string, extra []string) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if h, err := os.Hostname(); err == nil && h != "" {
		hosts = append(hosts, h)
	}
	hosts = append(hosts, bindHosts(bindAddr)...)
	hosts = append(hosts, extra...)

	seen := make(map[string]bool, len(hosts))
	unique := hosts[:0]
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		unique = append(unique, h)
	}
	return unique
}

func loadOrGenerateTLS(cfg config) (tls.Certificate, error) {
	// An explicit pair is a deliberate choice: fail loudly instead of quietly
	// falling back to a self-signed certificate.
	if cfg.tlsCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.tlsCertFile, cfg.tlsKeyFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("loading certificate %s and key %s: %w", cfg.tlsCertFile, cfg.tlsKeyFile, err)
		}
		log.Printf("TLS using certificate %s", cfg.tlsCertFile)
		return cert, nil
	}

	certDir := cfg.tlsCertDir
	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	if fileExists(certFile) && fileExists(keyFile) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("loading certificates from %s: %w", certDir, err)
		}
		log.Printf("TLS using certificates from %s", certDir)
		return cert, nil
	}

	hosts := selfSignedHosts(cfg.bindAddr, cfg.tlsSelfSignedHosts)

	cert, err := loadSelfSignedCert(certDir, hosts)
	if err == nil {
		log.Printf("TLS reusing self-signed certificate from %s (valid until %s)",
			certDir, cert.Leaf.NotAfter.Format(time.RFC3339))
		return cert, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Printf("TLS regenerating self-signed certificate: %v", err)
	}

	certPEM, keyPEM, err := generateSelfSignedCert(hosts)
	if err != nil {
		return tls.Certificate{}, err
	}

	if err := saveSelfSignedCert(certDir, certPEM, keyPEM); err != nil {
		log.Printf("TLS self-signed certificate kept in memory, not persisted: %v", err)
	} else {
		log.Printf("TLS self-signed certificate saved to %s", certDir)
	}
	log.Printf("TLS using self-signed certificate for %s", strings.Join(hosts, ", "))

	return tls.X509KeyPair(certPEM, keyPEM)
}

// loadSelfSignedCert returns the previously generated certificate when it is still
// usable. It reports os.ErrNotExist when there is nothing saved yet; any other error
// describes why the saved pair has to be replaced.
func loadSelfSignedCert(certDir string, hosts []string) (tls.Certificate, error) {
	certFile, keyFile := selfSignedPaths(certDir)
	if !fileExists(certFile) || !fileExists(keyFile) {
		return tls.Certificate{}, os.ErrNotExist
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("saved pair is unusable: %w", err)
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("saved certificate is unreadable: %w", err)
	}

	if time.Now().Add(selfSignedRenewBefore).After(leaf.NotAfter) {
		return tls.Certificate{}, fmt.Errorf("saved certificate expires at %s", leaf.NotAfter.Format(time.RFC3339))
	}

	for _, h := range hosts {
		if err := leaf.VerifyHostname(h); err != nil {
			return tls.Certificate{}, fmt.Errorf("saved certificate is not valid for %q", h)
		}
	}

	cert.Leaf = leaf
	return cert, nil
}

func saveSelfSignedCert(certDir string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return err
	}
	certFile, keyFile := selfSignedPaths(certDir)
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, certPEM, 0o644)
}

func generateSelfSignedCert(hosts []string) (certPEM []byte, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generating serial number: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Static HTTP Server"},
			CommonName:   hosts[0],
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Clients have required the SubjectAltName extension for years; a certificate
	// with only a CommonName fails hostname verification even when it is trusted.
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling private key: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// --- Template Variables ---

type Variables struct {
	HtmlTitle   string
	Title       string
	Message     string
	Image       string
	Facebook    string
	Twitter     string
	Youtube     string
	ShowHeaders bool
	HeadersPath string
}

func loadVariables(cfg config) Variables {
	return Variables{
		HtmlTitle:   getEnvOrDefault("HTML_TITLE", "Coming soon"),
		Title:       getEnvOrDefault("TITLE", "soon"),
		Message:     getEnvOrDefault("MESSAGE", "Our website is coming soon, follow us for updates!"),
		Image:       os.Getenv("BG_IMAGE"),
		Facebook:    os.Getenv("FACEBOOK"),
		Twitter:     os.Getenv("TWITTER"),
		Youtube:     os.Getenv("YOUTUBE"),
		ShowHeaders: cfg.showHeaders,
		HeadersPath: cfg.headersPath,
	}
}

func getEnvOrDefault(env string, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

// --- LRU File Cache ---

var errNotFound = errors.New("file not found")

type cacheEntry struct {
	key         string
	content     []byte
	contentType string
	pinned      bool
}

type fileCache struct {
	mu          sync.Mutex
	entries     map[string]*list.Element
	lruList     *list.List
	currentSize int64
	maxSize     int64
	maxFileSize int64
	rootDir     string
	disabled    bool
}

func newFileCache(cfg config) *fileCache {
	return &fileCache{
		entries:     make(map[string]*list.Element),
		lruList:     list.New(),
		maxSize:     cfg.cacheMaxSize,
		maxFileSize: cfg.cacheMaxFileSize,
		rootDir:     cfg.rootDir,
		disabled:    cfg.cacheMaxSize == 0,
	}
}

func (fc *fileCache) seedIndex(vars Variables) error {
	index := filepath.Join(fc.rootDir, "index.html")

	if _, err := os.Stat(index); err != nil {
		return nil
	}

	content, err := os.ReadFile(index)
	if err != nil {
		return fmt.Errorf("reading index.html: %w", err)
	}

	tmpl, err := template.New("index").Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing index.html template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fmt.Errorf("executing index.html template: %w", err)
	}

	entry := &cacheEntry{
		key:         "/index.html",
		content:     buf.Bytes(),
		contentType: "text/html; charset=utf-8",
		pinned:      true,
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	elem := fc.lruList.PushFront(entry)
	fc.entries["/index.html"] = elem
	fc.entries["/"] = elem
	fc.currentSize += int64(len(entry.content))

	return nil
}

func (fc *fileCache) get(urlPath string) (*cacheEntry, error) {
	fc.mu.Lock()

	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		fc.mu.Unlock()
		return entry, nil
	}

	fc.mu.Unlock()

	entry, err := fc.readFromDisk(urlPath)
	if err != nil {
		return nil, err
	}

	if fc.disabled || int64(len(entry.content)) > fc.maxFileSize {
		return entry, nil
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if elem, ok := fc.entries[urlPath]; ok {
		fc.lruList.MoveToFront(elem)
		return elem.Value.(*cacheEntry), nil
	}

	entrySize := int64(len(entry.content))

	for fc.currentSize+entrySize > fc.maxSize && fc.lruList.Len() > 0 {
		back := fc.lruList.Back()
		if back == nil {
			break
		}
		victim := back.Value.(*cacheEntry)
		if victim.pinned {
			if fc.lruList.Len() <= 1 {
				break
			}
			evicted := false
			for e := fc.lruList.Back(); e != nil; e = e.Prev() {
				v := e.Value.(*cacheEntry)
				if !v.pinned {
					fc.currentSize -= int64(len(v.content))
					delete(fc.entries, v.key)
					fc.lruList.Remove(e)
					evicted = true
					break
				}
			}
			if !evicted {
				break
			}
			continue
		}
		fc.currentSize -= int64(len(victim.content))
		delete(fc.entries, victim.key)
		fc.lruList.Remove(back)
	}

	if fc.currentSize+entrySize > fc.maxSize {
		return entry, nil
	}

	elem := fc.lruList.PushFront(entry)
	fc.entries[urlPath] = elem
	fc.currentSize += entrySize

	return entry, nil
}

func (fc *fileCache) readFromDisk(urlPath string) (*cacheEntry, error) {
	clean := filepath.Clean(urlPath)
	fsPath := filepath.Join(fc.rootDir, clean)

	absRoot, _ := filepath.Abs(fc.rootDir)
	absPath, _ := filepath.Abs(fsPath)
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return nil, errNotFound
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		return nil, errNotFound
	}

	if info.IsDir() {
		fsPath = filepath.Join(fsPath, "index.html")
		if _, err := os.Stat(fsPath); err != nil {
			return nil, errNotFound
		}
	}

	content, err := os.ReadFile(fsPath)
	if err != nil {
		return nil, errNotFound
	}

	ct := mime.TypeByExtension(filepath.Ext(fsPath))
	if ct == "" {
		ct = http.DetectContentType(content)
	}

	return &cacheEntry{
		key:         urlPath,
		content:     content,
		contentType: ct,
	}, nil
}

// --- Reverse Proxy ---

type proxyHandler struct {
	prefix string
	proxy  *httputil.ReverseProxy
}

func buildProxyHandlers(routes []proxyRoute, timeout time.Duration, caFile string, insecure bool) []proxyHandler {
	if len(routes) == 0 {
		return nil
	}

	tlsCfg := &tls.Config{}
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			log.Fatalf("proxy-ca: failed to read CA file %q: %v", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			log.Fatalf("proxy-ca: no valid certificates found in %q", caFile)
		}
		tlsCfg.RootCAs = pool
		log.Printf("Proxy TLS: using CA from %s", caFile)
	} else if insecure {
		tlsCfg.InsecureSkipVerify = true
		log.Printf("Proxy TLS: InsecureSkipVerify enabled (trusted network only)")
	}

	transport := &http.Transport{
		TLSClientConfig:       tlsCfg,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: timeout,
	}

	handlers := make([]proxyHandler, len(routes))
	for i, r := range routes {
		target := r.target
		prefix := r.prefix
		handlers[i] = proxyHandler{
			prefix: prefix,
			proxy: &httputil.ReverseProxy{
				Director: func(req *http.Request) {
					// TLS is terminated here, so the backend can only learn the
					// original scheme and host from the forwarded headers. An
					// upstream proxy's values are left untouched.
					if _, ok := req.Header["X-Forwarded-Proto"]; !ok {
						scheme := "http"
						if req.TLS != nil {
							scheme = "https"
						}
						req.Header.Set("X-Forwarded-Proto", scheme)
					}
					if _, ok := req.Header["X-Forwarded-Host"]; !ok && req.Host != "" {
						req.Header.Set("X-Forwarded-Host", req.Host)
					}

					req.URL.Scheme = target.Scheme
					req.URL.Host = target.Host
					req.Host = target.Host
					req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
					if req.URL.Path == "" || req.URL.Path[0] != '/' {
						req.URL.Path = "/" + req.URL.Path
					}
					if _, ok := req.Header["User-Agent"]; !ok {
						req.Header.Set("User-Agent", "")
					}
				},
				Transport: transport,
				ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
					log.Printf("proxy error [%s]: %v", routePrefix(prefix), err)
					http.Error(w, "Bad Gateway", http.StatusBadGateway)
				},
			},
		}
	}
	return handlers
}

// --- HTTP Handlers ---

type statusRespWr struct {
	http.ResponseWriter
	status int
}

func (w *statusRespWr) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func wrapHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srw := &statusRespWr{ResponseWriter: w, status: 200}
		h(srw, r)
		log.Printf("%s - \"%s %s %s\" %d %d \"%s\"",
			r.RemoteAddr, r.Method, r.RequestURI, r.Proto,
			srw.status, r.ContentLength, r.UserAgent())
	}
}

func serveStatic(cache *fileCache, cfg config, proxies []proxyHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == cfg.healthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}

		if r.URL.Path == cfg.headersPath && cfg.showHeaders {
			headers := make(map[string]string)
			keys := make([]string, 0, len(r.Header))
			for k := range r.Header {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				headers[k] = strings.Join(r.Header[k], ", ")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(headers)
			return
		}

		// Check proxy routes
		for _, p := range proxies {
			if r.URL.Path == p.prefix || strings.HasPrefix(r.URL.Path, p.prefix+"/") {
				p.proxy.ServeHTTP(w, r)
				return
			}
		}

		entry, err := cache.get(r.URL.Path)
		if err == nil {
			w.Header().Set("Content-Type", entry.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
			w.WriteHeader(http.StatusOK)
			w.Write(entry.content)
			return
		}

		if cfg.spaMode && filepath.Ext(r.URL.Path) == "" {
			entry, err := cache.get("/index.html")
			if err == nil {
				w.Header().Set("Content-Type", entry.contentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
				w.WriteHeader(http.StatusOK)
				w.Write(entry.content)
				return
			}
		}

		http.NotFound(w, r)
	}
}

// --- Main ---

func main() {
	cfg := loadConfig()
	cache := newFileCache(cfg)

	vars := loadVariables(cfg)
	if err := cache.seedIndex(vars); err != nil {
		log.Fatalf("Failed to process index template: %v", err)
	}

	proxies := buildProxyHandlers(cfg.proxyRoutes, cfg.proxyTimeout, cfg.proxyCAFile, cfg.proxyInsecure)
	handler := wrapHandler(serveStatic(cache, cfg, proxies))

	log.Printf("byjg/static-httpserver %s", version)
	if cfg.spaMode {
		log.Printf("SPA mode enabled")
	}
	if cache.disabled {
		log.Printf("Cache disabled")
	} else {
		log.Printf("Cache max size: %d bytes, max file size: %d bytes", cache.maxSize, cache.maxFileSize)
	}
	for _, r := range cfg.proxyRoutes {
		log.Printf("Proxy: %s -> %s (timeout: %s)", routePrefix(r.prefix), r.target, cfg.proxyTimeout)
	}

	// Start HTTP server (only if port is configured)
	if cfg.port != "" {
		httpSrv := &http.Server{
			Addr:         net.JoinHostPort(cfg.bindAddr, cfg.port),
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		go func() {
			log.Printf("HTTP listening on %s", httpSrv.Addr)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()
	}

	// Start HTTPS server
	cert, err := loadOrGenerateTLS(cfg)
	if err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}

	tlsSrv := &http.Server{
		Addr:    net.JoinHostPort(cfg.bindAddr, cfg.tlsPort),
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("HTTPS listening on %s", tlsSrv.Addr)
	log.Fatal(tlsSrv.ListenAndServeTLS("", ""))
}
