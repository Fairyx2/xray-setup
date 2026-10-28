package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const releaseAPI = "https://api.github.com/repos/XTLS/Xray-core/releases/latest"

// Release 只接收官方发行 API 中安装所需的元数据，不执行任何远程 shell 脚本。
type Release struct {
	Tag        string         `json:"tag_name"`   // GitHub 正式发行版本标签。
	Draft      bool           `json:"draft"`      // 草稿不允许安装。
	Prerelease bool           `json:"prerelease"` // 第一版只安装正式版本。
	Assets     []ReleaseAsset `json:"assets"`     // 官方 zip 及对应 .dgst 校验文件。
}

type ReleaseAsset struct {
	Name string `json:"name"`                 // 按 CPU 架构精确匹配文件名。
	URL  string `json:"browser_download_url"` // 仅接受官方仓库的 HTTPS 下载地址。
}

func AssetName(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "Xray-linux-64.zip", nil
	case "arm64":
		return "Xray-linux-arm64-v8a.zip", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

func (s *Service) fetch(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "my-vpn/0.1")
	response, err := s.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, endpoint)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	return data, nil
}

func officialAssetURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && strings.HasPrefix(u.Path, "/XTLS/Xray-core/releases/download/")
}

func (s *Service) Latest(ctx context.Context) (Release, error) {
	data, err := s.fetch(ctx, releaseAPI, 2<<20)
	if err != nil {
		return Release{}, err
	}
	var release Release
	if err := json.Unmarshal(data, &release); err != nil {
		return release, err
	}
	if release.Draft || release.Prerelease || release.Tag == "" {
		return release, fmt.Errorf("no stable release")
	}
	return release, nil
}

var digestPattern = regexp.MustCompile(`(?im)^\s*(?:SHA2?-?256|SHA256)\s*=\s*([a-f0-9]{64})\s*$`)

func VerifyDigest(data, digest []byte) error {
	match := digestPattern.FindSubmatch(digest)
	if len(match) != 2 {
		return fmt.Errorf("missing SHA-256 digest")
	}
	expected, err := hex.DecodeString(string(match[1]))
	if err != nil {
		return err
	}
	actual := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), hex.EncodeToString(expected)) {
		return fmt.Errorf("SHA-256 mismatch")
	}
	return nil
}

func (s *Service) StageRelease(ctx context.Context, release Release, dir string) (string, error) {
	name, err := AssetName(s.Arch)
	if err != nil {
		return "", err
	}
	var archiveURL, digestURL string
	for _, asset := range release.Assets {
		if asset.Name == name {
			archiveURL = asset.URL
		}
		if asset.Name == name+".dgst" {
			digestURL = asset.URL
		}
	}
	if !officialAssetURL(archiveURL) || !officialAssetURL(digestURL) {
		return "", fmt.Errorf("official release assets not found")
	}
	archive, err := s.fetch(ctx, archiveURL, 128<<20)
	if err != nil {
		return "", err
	}
	digest, err := s.fetch(ctx, digestURL, 16<<10)
	if err != nil {
		return "", err
	}
	if err := VerifyDigest(archive, digest); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, "xray.zip")
	if err := os.WriteFile(zipPath, archive, 0600); err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		// 仅解压根目录下的 xray，拒绝压缩包路径穿越、符号链接及异常大小。
		if file.Name != "xray" {
			continue
		}
		if file.Mode()&os.ModeType != 0 || file.UncompressedSize64 > 256<<20 {
			return "", fmt.Errorf("invalid xray binary entry")
		}
		input, err := file.Open()
		if err != nil {
			return "", err
		}
		defer input.Close()
		path := filepath.Join(dir, "xray")
		output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(output, io.LimitReader(input, (256<<20)+1))
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return path, nil
	}
	return "", fmt.Errorf("xray binary missing from archive")
}
