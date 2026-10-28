package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my-vpn/internal/config"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTransactionRestoresExistingAndRemovesNewFiles(t *testing.T) {
	dir := tempDir(t)
	old := filepath.Join(dir, "config.json")
	created := filepath.Join(dir, "new-service")
	if err := os.WriteFile(old, []byte("old config"), 0640); err != nil {
		t.Fatal(err)
	}
	tx, err := newTransaction(dir, []string{old, created})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(old, []byte("broken config"), 0600)
	os.Chmod(old, 0600)
	os.WriteFile(created, []byte("new unit"), 0644)
	if err := tx.restore(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(old)
	if err != nil || string(data) != "old config" {
		t.Fatal("did not restore previous config")
	}
	info, _ := os.Stat(old)
	if info.Mode().Perm() != 0640 {
		t.Fatal("did not restore config permissions")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatal("did not remove newly created file")
	}
	manifest, _ := os.Stat(filepath.Join(tx.dir, "manifest.json"))
	if manifest.Mode().Perm() != 0600 {
		t.Fatal("backup manifest permissions")
	}
}

func TestTransactionContinuesAfterRecoveryFailure(t *testing.T) {
	dir := tempDir(t)
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("old a"), 0600)
	os.WriteFile(b, []byte("old b"), 0600)
	tx, err := newTransaction(dir, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(tx.dir, "0.bak"))
	os.WriteFile(b, []byte("new b"), 0600)
	if tx.restore() == nil {
		t.Fatal("missing backup not reported")
	}
	data, _ := os.ReadFile(b)
	if string(data) != "old b" {
		t.Fatal("did not continue restoring remaining files")
	}
}

func TestOperationLock(t *testing.T) {
	path := filepath.Join(tempDir(t), "lock")
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := acquireLock(path); err == nil {
		release()
		t.Fatal("concurrent lock succeeded")
	}
	unlock()
	unlock, err = acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestPortChecksDetectOccupiedPort(t *testing.T) {
	l, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if PortAvailable("0.0.0.0", port) {
		t.Fatal("occupied port reported free")
	}
	l.Close()
	if !PortAvailable("0.0.0.0", port) {
		t.Fatal("released port reported occupied")
	}
}

func TestDigestRejectsTampering(t *testing.T) {
	data := []byte("official binary archive")
	sum := sha256.Sum256(data)
	digest := []byte(fmt.Sprintf("SHA2-256= %x\r\nSHA2-512= ignored\n", sum))
	if err := VerifyDigest(data, digest); err != nil {
		t.Fatal(err)
	}
	if VerifyDigest([]byte("tampered"), digest) == nil {
		t.Fatal("accepted modified archive")
	}
	if VerifyDigest(data, []byte("no digest")) == nil {
		t.Fatal("accepted missing hash")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestStageReleaseOnlyExtractsVerifiedBinary(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, _ := writer.Create("../../escape")
	file.Write([]byte("bad"))
	file, _ = writer.Create("xray")
	file.Write([]byte("test binary"))
	writer.Close()
	sum := sha256.Sum256(archive.Bytes())
	s := New()
	s.Arch = "amd64"
	s.HTTP = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		data := archive.Bytes()
		if strings.HasSuffix(r.URL.Path, ".dgst") {
			data = []byte(fmt.Sprintf("SHA256=%x\n", sum))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	base := "https://github.com/XTLS/Xray-core/releases/download/v1/"
	release := Release{Tag: "v1", Assets: []ReleaseAsset{{Name: "Xray-linux-64.zip", URL: base + "x.zip"}, {Name: "Xray-linux-64.zip.dgst", URL: base + "x.zip.dgst"}}}
	dir := tempDir(t)
	binary, err := s.StageRelease(context.Background(), release, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(binary)
	if string(data) != "test binary" {
		t.Fatal("wrong binary")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "xray" {
		t.Fatal("unexpected extraction")
	}
	release.Assets[0].URL = "https://example.com/xray.zip"
	if _, err := s.StageRelease(context.Background(), release, dir); err == nil {
		t.Fatal("accepted unofficial URL")
	}
}

func TestLoadUsesLiveNativeConfigAndChecksOwnership(t *testing.T) {
	dir := tempDir(t)
	s := New()
	s.Paths = Paths{State: filepath.Join(dir, "state.json"), Config: filepath.Join(dir, "config.json"), Unit: filepath.Join(dir, "unit"), Binary: filepath.Join(dir, "xray")}
	p, err := config.Generate()
	if err != nil {
		t.Fatal(err)
	}
	p.Address = "203.0.113.10"
	raw, _ := config.Build(p)
	state := config.State{Schema: 1, Version: "v1", Profile: p, Config: raw}
	data, _ := json.Marshal(state)
	os.WriteFile(s.Paths.State, data, 0600)
	os.WriteFile(s.Paths.Unit, []byte(managedMarker+"\n"), 0644)
	p.Port = 8443
	raw, _ = config.Build(p)
	os.WriteFile(s.Paths.Config, raw, 0640)
	actual, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Profile.Port != 8443 {
		t.Fatal("ignored live native config")
	}
	os.WriteFile(s.Paths.Unit, []byte("unmanaged"), 0644)
	if _, err := s.Load(); err == nil {
		t.Fatal("accepted unmanaged unit")
	}
}
