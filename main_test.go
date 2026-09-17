package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func leafOf(t *testing.T, cert tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing leaf certificate: %v", err)
	}
	return leaf
}

func TestGenerateSelfSignedCertHasSubjectAltNames(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSignedCert([]string{"localhost", "127.0.0.1", "::1", "www.example.org"})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	leaf := leafOf(t, cert)

	for _, host := range []string{"localhost", "127.0.0.1", "::1", "www.example.org"} {
		if err := leaf.VerifyHostname(host); err != nil {
			t.Errorf("certificate is not valid for %q: %v", host, err)
		}
	}
	if err := leaf.VerifyHostname("other.example.org"); err == nil {
		t.Error("certificate unexpectedly valid for a host that was not requested")
	}
	if len(leaf.IPAddresses) != 2 {
		t.Errorf("expected 2 IP SANs, got %v", leaf.IPAddresses)
	}
}

func TestLoadOrGenerateTLSPersistsAndReuses(t *testing.T) {
	dir := t.TempDir()
	cfg := config{tlsCertDir: dir, tlsSelfSignedHosts: []string{"first.example.org"}}

	first, err := loadOrGenerateTLS(cfg)
	if err != nil {
		t.Fatalf("first loadOrGenerateTLS: %v", err)
	}

	certFile, keyFile := selfSignedPaths(dir)
	if !fileExists(certFile) || !fileExists(keyFile) {
		t.Fatal("self-signed certificate was not persisted")
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file permissions = %04o, want 0600", perm)
	}

	second, err := loadOrGenerateTLS(cfg)
	if err != nil {
		t.Fatalf("second loadOrGenerateTLS: %v", err)
	}
	if leafOf(t, first).SerialNumber.Cmp(leafOf(t, second).SerialNumber) != 0 {
		t.Error("expected the saved certificate to be reused, but a new one was generated")
	}
}

func TestLoadOrGenerateTLSRegeneratesForNewHost(t *testing.T) {
	dir := t.TempDir()

	first, err := loadOrGenerateTLS(config{tlsCertDir: dir})
	if err != nil {
		t.Fatalf("first loadOrGenerateTLS: %v", err)
	}

	second, err := loadOrGenerateTLS(config{tlsCertDir: dir, tlsSelfSignedHosts: []string{"added.example.org"}})
	if err != nil {
		t.Fatalf("second loadOrGenerateTLS: %v", err)
	}

	if leafOf(t, first).SerialNumber.Cmp(leafOf(t, second).SerialNumber) == 0 {
		t.Fatal("expected a new certificate when a host is added")
	}
	if err := leafOf(t, second).VerifyHostname("added.example.org"); err != nil {
		t.Errorf("regenerated certificate does not cover the new host: %v", err)
	}
}

func TestLoadSelfSignedCertRejectsExpiring(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"localhost"}

	certPEM, keyPEM, err := generateSelfSignedCert(hosts)
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	if err := saveSelfSignedCert(dir, certPEM, keyPEM); err != nil {
		t.Fatalf("saveSelfSignedCert: %v", err)
	}

	if _, err := loadSelfSignedCert(dir, hosts); err != nil {
		t.Fatalf("fresh certificate should load: %v", err)
	}

	// The certificate is valid for a year; asking for renewal two years ahead of
	// expiry makes the same file look like it is about to expire.
	original := selfSignedRenewBefore
	selfSignedRenewBefore = 2 * 365 * 24 * time.Hour
	defer func() { selfSignedRenewBefore = original }()

	if _, err := loadSelfSignedCert(dir, hosts); err == nil {
		t.Error("expected an expiring certificate to be rejected")
	}
}

func TestLoadOrGenerateTLSPrefersProvidedCertificates(t *testing.T) {
	dir := t.TempDir()

	certPEM, keyPEM, err := generateSelfSignedCert([]string{"provided.example.org"})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o644); err != nil {
		t.Fatalf("writing cert.pem: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		t.Fatalf("writing key.pem: %v", err)
	}

	cert, err := loadOrGenerateTLS(config{tlsCertDir: dir})
	if err != nil {
		t.Fatalf("loadOrGenerateTLS: %v", err)
	}
	if err := leafOf(t, cert).VerifyHostname("provided.example.org"); err != nil {
		t.Errorf("provided certificate was not used: %v", err)
	}
	if fileExists(filepath.Join(dir, "selfsigned-cert.pem")) {
		t.Error("a self-signed certificate was generated even though cert.pem/key.pem exist")
	}
}

func TestLoadOrGenerateTLSFallsBackWhenDirIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: an unwritable directory cannot be simulated")
	}

	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatalf("creating read-only dir: %v", err)
	}

	cert, err := loadOrGenerateTLS(config{tlsCertDir: dir})
	if err != nil {
		t.Fatalf("loadOrGenerateTLS should still serve an in-memory certificate: %v", err)
	}
	if err := leafOf(t, cert).VerifyHostname("localhost"); err != nil {
		t.Errorf("in-memory certificate is not valid for localhost: %v", err)
	}
}

func TestSelfSignedHostsDeduplicates(t *testing.T) {
	hosts := selfSignedHosts("0.0.0.0", []string{"localhost", " example.org ", "", "example.org"})

	seen := map[string]int{}
	for _, h := range hosts {
		seen[h]++
	}
	for h, n := range seen {
		if n > 1 {
			t.Errorf("host %q appears %d times", h, n)
		}
	}
	if seen["example.org"] != 1 {
		t.Errorf("trimmed host was not kept: %v", hosts)
	}
	for _, want := range []string{"localhost", "127.0.0.1", "::1"} {
		if seen[want] == 0 {
			t.Errorf("default host %q missing from %v", want, hosts)
		}
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList("  "); got != nil {
		t.Errorf("splitList(blank) = %v, want nil", got)
	}
	got := splitList("a,b")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitList(\"a,b\") = %v", got)
	}
}

func TestServeStaticOverTLSUsesGeneratedCertificate(t *testing.T) {
	dir := t.TempDir()
	cert, err := loadOrGenerateTLS(config{tlsCertDir: dir})
	if err != nil {
		t.Fatalf("loadOrGenerateTLS: %v", err)
	}

	leaf := leafOf(t, cert)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.(*tls.Conn).Handshake()
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	// A client that trusts the certificate must also accept the hostname; this is
	// what fails when the certificate carries no SubjectAltName.
	conn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", port), &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatalf("TLS handshake against the generated certificate failed: %v", err)
	}
	conn.Close()
}

func TestLoadOrGenerateTLSUsesExplicitFiles(t *testing.T) {
	dir := t.TempDir()

	certPEM, keyPEM, err := generateSelfSignedCert([]string{"explicit.example.org"})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	// Names that do not match the cert.pem/key.pem convention, as certbot uses.
	certFile := filepath.Join(dir, "fullchain.pem")
	keyFile := filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		t.Fatalf("writing fullchain.pem: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("writing privkey.pem: %v", err)
	}

	cert, err := loadOrGenerateTLS(config{tlsCertDir: dir, tlsCertFile: certFile, tlsKeyFile: keyFile})
	if err != nil {
		t.Fatalf("loadOrGenerateTLS: %v", err)
	}
	if err := leafOf(t, cert).VerifyHostname("explicit.example.org"); err != nil {
		t.Errorf("explicit certificate was not used: %v", err)
	}
	if fileExists(filepath.Join(dir, "selfsigned-cert.pem")) {
		t.Error("a self-signed certificate was generated even though explicit files were given")
	}
}

func TestLoadOrGenerateTLSFailsOnMissingExplicitFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := loadOrGenerateTLS(config{
		tlsCertDir:  dir,
		tlsCertFile: filepath.Join(dir, "absent.pem"),
		tlsKeyFile:  filepath.Join(dir, "absent-key.pem"),
	})
	if err == nil {
		t.Fatal("expected an error instead of a silent fallback to a self-signed certificate")
	}
	if fileExists(filepath.Join(dir, "selfsigned-cert.pem")) {
		t.Error("fell back to generating a self-signed certificate")
	}
}

func TestSelfSignedHostsIncludesExplicitBindAddress(t *testing.T) {
	hosts := selfSignedHosts("127.0.0.1", nil)
	for _, h := range hosts {
		if h == "127.0.0.1" {
			return
		}
	}
	t.Errorf("bind address missing from %v", hosts)
}

func TestBindHostsExplicitAddress(t *testing.T) {
	got := bindHosts("192.168.1.10")
	if len(got) != 1 || got[0] != "192.168.1.10" {
		t.Errorf("bindHosts(\"192.168.1.10\") = %v, want [192.168.1.10]", got)
	}
	if got := bindHosts("127.0.0.1"); len(got) != 1 || got[0] != "127.0.0.1" {
		t.Errorf("bindHosts(loopback) = %v, want [127.0.0.1]", got)
	}
}

func TestBindHostsWildcardSkipsLoopbackAndLinkLocal(t *testing.T) {
	for _, wildcard := range []string{"0.0.0.0", "::"} {
		for _, h := range bindHosts(wildcard) {
			ip := net.ParseIP(h)
			if ip == nil {
				t.Errorf("bindHosts(%q) returned a non-IP %q", wildcard, h)
				continue
			}
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				t.Errorf("bindHosts(%q) returned %q, which should be filtered out", wildcard, h)
			}
		}
	}
}

func TestGeneratedCertificateCoversWildcardBindAddresses(t *testing.T) {
	dir := t.TempDir()

	cert, err := loadOrGenerateTLS(config{tlsCertDir: dir, bindAddr: "0.0.0.0"})
	if err != nil {
		t.Fatalf("loadOrGenerateTLS: %v", err)
	}
	leaf := leafOf(t, cert)

	for _, h := range bindHosts("0.0.0.0") {
		if err := leaf.VerifyHostname(h); err != nil {
			t.Errorf("certificate is not valid for the bound address %q: %v", h, err)
		}
	}
}

func TestDefaultCertDirDependsOnUser(t *testing.T) {
	got := defaultCertDir()

	if os.Geteuid() == 0 {
		if got != "/certs" {
			t.Errorf("defaultCertDir() as root = %q, want /certs", got)
		}
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}
	want := filepath.Join(home, ".static-httpserver", "certs")
	if got != want {
		t.Errorf("defaultCertDir() = %q, want %q", got, want)
	}
}

func TestSaveSelfSignedCertCreatesPrivateDirectory(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "nested", "certs")

	certPEM, keyPEM, err := generateSelfSignedCert([]string{"localhost"})
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	if err := saveSelfSignedCert(certDir, certPEM, keyPEM); err != nil {
		t.Fatalf("saveSelfSignedCert: %v", err)
	}

	info, err := os.Stat(certDir)
	if err != nil {
		t.Fatalf("stat cert dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("cert directory permissions = %04o, want 0700", perm)
	}
}

// --- Reverse proxy ---

func TestParseProxyRoutesAcceptsCatchAll(t *testing.T) {
	routes, err := parseProxyRoutes([]string{"/=http://backend:3000"})
	if err != nil {
		t.Fatalf("parseProxyRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].prefix != "" {
		t.Errorf("catch-all prefix = %q, want empty", routes[0].prefix)
	}
	if got := routePrefix(routes[0].prefix); got != "/" {
		t.Errorf("routePrefix() = %q, want /", got)
	}
}

func TestParseProxyRoutesOrdersMostSpecificFirst(t *testing.T) {
	routes, err := parseProxyRoutes([]string{
		"/=http://root:3000",
		"/api=http://api:3000",
		"/api/admin=http://admin:3000",
	})
	if err != nil {
		t.Fatalf("parseProxyRoutes: %v", err)
	}

	want := []string{"/api/admin", "/api", ""}
	for i, prefix := range want {
		if routes[i].prefix != prefix {
			t.Errorf("route %d prefix = %q, want %q", i, routes[i].prefix, prefix)
		}
	}
}

func TestParseProxyRoutesRejectsBadInput(t *testing.T) {
	for _, spec := range []string{"api=http://backend:3000", "/api", "/api=ftp://backend:3000"} {
		if _, err := parseProxyRoutes([]string{spec}); err == nil {
			t.Errorf("parseProxyRoutes(%q) accepted an invalid route", spec)
		}
	}
}

// proxyTestServer records what the backend actually received.
type proxyTestServer struct {
	path  string
	proto string
	host  string
}

func newProxyBackend(t *testing.T, rec *proxyTestServer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.proto = r.Header.Get("X-Forwarded-Proto")
		rec.host = r.Header.Get("X-Forwarded-Host")
		fmt.Fprint(w, "from backend")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func frontHandler(t *testing.T, specs []string) http.HandlerFunc {
	t.Helper()
	routes, err := parseProxyRoutes(specs)
	if err != nil {
		t.Fatalf("parseProxyRoutes: %v", err)
	}
	cfg := config{
		rootDir:          t.TempDir(),
		cacheMaxSize:     1000,
		cacheMaxFileSize: 1000,
		healthPath:       "/_health",
		headersPath:      "/_headers",
	}
	return serveStatic(newFileCache(cfg), cfg, buildProxyHandlers(routes, time.Second, "", false))
}

func TestCatchAllProxyForwardsEveryPathUnchanged(t *testing.T) {
	var rec proxyTestServer
	backend := newProxyBackend(t, &rec)
	handler := frontHandler(t, []string{"/=" + backend.URL})

	for _, path := range []string{"/", "/some/page", "/assets/app.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, w.Code)
		}
		if rec.path != path {
			t.Errorf("backend received %q for request %q, want it unchanged", rec.path, path)
		}
	}
}

func TestMoreSpecificRouteWinsOverCatchAll(t *testing.T) {
	var catchAll, specific proxyTestServer
	catchAllBackend := newProxyBackend(t, &catchAll)
	specificBackend := newProxyBackend(t, &specific)

	handler := frontHandler(t, []string{"/=" + catchAllBackend.URL, "/api=" + specificBackend.URL})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))

	if specific.path != "/users" {
		t.Errorf("specific backend received %q, want /users (prefix stripped)", specific.path)
	}
	if catchAll.path != "" {
		t.Errorf("catch-all backend received %q, it should not have been used", catchAll.path)
	}
}

func TestCatchAllProxyDoesNotShadowHealth(t *testing.T) {
	var rec proxyTestServer
	backend := newProxyBackend(t, &rec)
	handler := frontHandler(t, []string{"/=" + backend.URL})

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/_health", nil))

	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("/_health returned %q, want the local health response", body)
	}
	if rec.path != "" {
		t.Errorf("/_health was proxied to the backend as %q", rec.path)
	}
}

func TestCatchAllProxyReachesTheBackendHealthEndpoint(t *testing.T) {
	var rec proxyTestServer
	backend := newProxyBackend(t, &rec)
	handler := frontHandler(t, []string{"/=" + backend.URL})

	// The built-in endpoint lives under /_health, so a backend keeps its own.
	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.path != "/health" {
		t.Errorf("backend received %q, want /health to be proxied through", rec.path)
	}
}

func TestHealthAndHeadersPathsAreConfigurable(t *testing.T) {
	cfg := config{
		rootDir:          t.TempDir(),
		cacheMaxSize:     1000,
		cacheMaxFileSize: 1000,
		healthPath:       "/x-healthz",
		headersPath:      "/x-headers",
		showHeaders:      true,
	}
	handler := serveStatic(newFileCache(cfg), cfg, nil)

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/x-healthz", nil))
	if body := w.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("/x-healthz returned %q, want the health response", body)
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x-headers", nil)
	req.Header.Set("X-Test", "value")
	handler(w, req)
	if !strings.Contains(w.Body.String(), "X-Test") {
		t.Errorf("/x-headers returned %q, want the request headers", w.Body.String())
	}

	// The default paths are no longer served once they have been moved.
	w = httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/_health", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("/_health returned %d, want 404 after the path was changed", w.Code)
	}
}

func TestHeadersEndpointNeedsShowHeaders(t *testing.T) {
	cfg := config{
		rootDir:          t.TempDir(),
		cacheMaxSize:     1000,
		cacheMaxFileSize: 1000,
		healthPath:       "/_health",
		headersPath:      "/_headers",
	}
	handler := serveStatic(newFileCache(cfg), cfg, nil)

	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/_headers", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("/_headers returned %d without --show-headers, want 404", w.Code)
	}
}

func TestProxySetsForwardedHeaders(t *testing.T) {
	var rec proxyTestServer
	backend := newProxyBackend(t, &rec)
	handler := frontHandler(t, []string{"/api=" + backend.URL})

	// A request that arrived over TLS, as the HTTPS listener produces.
	req := httptest.NewRequest(http.MethodGet, "https://www.example.org/api/users", nil)
	req.TLS = &tls.ConnectionState{}
	handler(httptest.NewRecorder(), req)

	if rec.proto != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want https", rec.proto)
	}
	if rec.host != "www.example.org" {
		t.Errorf("X-Forwarded-Host = %q, want www.example.org", rec.host)
	}

	rec = proxyTestServer{}
	plain := httptest.NewRequest(http.MethodGet, "http://www.example.org/api/users", nil)
	handler(httptest.NewRecorder(), plain)
	if rec.proto != "http" {
		t.Errorf("X-Forwarded-Proto over plain HTTP = %q, want http", rec.proto)
	}
}

func TestProxyKeepsUpstreamForwardedHeaders(t *testing.T) {
	var rec proxyTestServer
	backend := newProxyBackend(t, &rec)
	handler := frontHandler(t, []string{"/api=" + backend.URL})

	req := httptest.NewRequest(http.MethodGet, "http://internal/api/users", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "public.example.org")
	handler(httptest.NewRecorder(), req)

	if rec.proto != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want the upstream value https", rec.proto)
	}
	if rec.host != "public.example.org" {
		t.Errorf("X-Forwarded-Host = %q, want the upstream value", rec.host)
	}
}
