package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	assetExe  = "NeverSleep.exe"
	assetHash = "NeverSleep.exe.sha256"
	MaxExe    = 40 << 20
	MinExe    = 1 << 20
)

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

type Getter func(url string, limit int64) ([]byte, error)

var ErrNoUpdate = errors.New("no update")

// PlanUpdate decides whether a release is a valid newer build and returns its verified bytes.
// The SHA-256 comes from the same release, so it detects corrupt or truncated downloads and
// mismatched assets. It does NOT prove who published the file.
func PlanUpdate(relJSON []byte, cur string, get Getter) (string, []byte, error) {
	var rel ghRelease
	if json.Unmarshal(relJSON, &rel) != nil || rel.Draft || rel.Pre || !NewerVersion(rel.Tag, cur) {
		return "", nil, ErrNoUpdate
	}
	prefix := "https://github.com/" + Repo + "/releases/download/" + rel.Tag + "/"
	var exeURL, hashURL string
	var size int64
	for _, a := range rel.Assets {
		switch a.Name {
		case assetExe:
			exeURL, size = a.URL, a.Size
		case assetHash:
			hashURL = a.URL
		}
	}
	if exeURL == "" || hashURL == "" || size < MinExe || size > MaxExe {
		return "", nil, errors.New("assets missing or size out of range")
	}
	if !strings.HasPrefix(exeURL, prefix) || !strings.HasPrefix(hashURL, prefix) {
		return "", nil, errors.New("asset url outside the project release")
	}
	hb, err := get(hashURL, 1024)
	if err != nil {
		return "", nil, err
	}
	want := FirstHashToken(string(hb))
	if want == "" {
		return "", nil, errors.New("bad hash file")
	}
	exe, err := get(exeURL, MaxExe)
	if err != nil {
		return "", nil, err
	}
	if int64(len(exe)) != size || len(exe) < MinExe || exe[0] != 'M' || exe[1] != 'Z' {
		return "", nil, errors.New("not the expected executable")
	}
	sum := sha256.Sum256(exe)
	if hex.EncodeToString(sum[:]) != want {
		return "", nil, errors.New("sha256 mismatch")
	}
	return rel.Tag, exe, nil
}
