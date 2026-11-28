package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeRelease builds a GitHub /releases/latest body plus the files its assets point at.
func fakeRelease(tag string, exe []byte, sum string, mutate func(m map[string]interface{})) ([]byte, func(url string, limit int64) ([]byte, error)) {
	prefix := "https://github.com/" + Repo + "/releases/download/" + tag + "/"
	m := map[string]interface{}{
		"tag_name":   tag,
		"draft":      false,
		"prerelease": false,
		"assets": []interface{}{
			map[string]interface{}{"name": "NeverSleep.exe", "browser_download_url": prefix + "NeverSleep.exe", "size": len(exe)},
			map[string]interface{}{"name": "NeverSleep.exe.sha256", "browser_download_url": prefix + "NeverSleep.exe.sha256", "size": len(sum)},
		},
	}
	if mutate != nil {
		mutate(m)
	}
	b, _ := json.Marshal(m)
	get := func(url string, limit int64) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, ".sha256"):
			return []byte(sum), nil
		case strings.HasSuffix(url, "NeverSleep.exe"):
			return exe, nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}
	return b, get
}

func goodExe() []byte {
	b := make([]byte, MinExe+16)
	b[0], b[1] = 'M', 'Z'
	for i := 2; i < len(b); i++ {
		b[i] = byte(i * 7)
	}
	return b
}

func hashOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestPlanUpdateAcceptsGoodRelease(t *testing.T) {
	exe := goodExe()
	rel, get := fakeRelease("v2.2.0", exe, hashOf(exe)+"  NeverSleep.exe\n", nil)
	tag, got, err := PlanUpdate(rel, "2.1.0", get)
	if err != nil || tag != "v2.2.0" || len(got) != len(exe) {
		t.Fatalf("tag=%q len=%d err=%v", tag, len(got), err)
	}
}

func TestPlanUpdateRejections(t *testing.T) {
	exe := goodExe()
	sum := hashOf(exe) + "  NeverSleep.exe\n"
	cases := []struct {
		name   string
		tag    string
		cur    string
		exe    []byte
		sum    string
		mutate func(m map[string]interface{})
	}{
		{"same version", "v2.1.0", "2.1.0", exe, sum, nil},
		{"older version", "v2.0.0", "2.1.0", exe, sum, nil},
		{"draft", "v2.2.0", "2.1.0", exe, sum, func(m map[string]interface{}) { m["draft"] = true }},
		{"prerelease", "v2.2.0", "2.1.0", exe, sum, func(m map[string]interface{}) { m["prerelease"] = true }},
		{"hash mismatch", "v2.2.0", "2.1.0", exe, strings.Repeat("0", 64) + "  NeverSleep.exe", nil},
		{"bad hash file", "v2.2.0", "2.1.0", exe, "not-a-hash", nil},
		{"not an exe", "v2.2.0", "2.1.0", append([]byte{'X', 'Y'}, exe[2:]...), "", nil},
		{"too small", "v2.2.0", "2.1.0", []byte("MZ"), hashOf([]byte("MZ")), nil},
		{"asset outside the repo", "v2.2.0", "2.1.0", exe, sum, func(m map[string]interface{}) {
			a := m["assets"].([]interface{})
			a[0].(map[string]interface{})["browser_download_url"] = "https://evil.example/NeverSleep.exe"
		}},
		{"missing hash asset", "v2.2.0", "2.1.0", exe, sum, func(m map[string]interface{}) {
			m["assets"] = []interface{}{m["assets"].([]interface{})[0]}
		}},
		{"renamed exe asset", "v2.2.0", "2.1.0", exe, sum, func(m map[string]interface{}) {
			a := m["assets"].([]interface{})
			a[0].(map[string]interface{})["name"] = "Never Sleep.exe"
		}},
	}
	for _, c := range cases {
		rel, get := fakeRelease(c.tag, c.exe, c.sum, c.mutate)
		if tag, b, err := PlanUpdate(rel, c.cur, get); err == nil {
			t.Fatalf("%s: expected rejection, got %q (%d bytes)", c.name, tag, len(b))
		}
	}
	if _, _, err := PlanUpdate([]byte("<html>"), "2.1.0", func(string, int64) ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("non-JSON body accepted")
	}
}

func TestFirstHashToken(t *testing.T) {
	h := strings.Repeat("a", 64)
	if FirstHashToken(h+"  NeverSleep.exe") != h {
		t.Fatal("plain hash")
	}
	if FirstHashToken("  "+strings.Repeat("B", 64)) != strings.Repeat("b", 64) {
		t.Fatal("upper case hash should be lowered")
	}
	if FirstHashToken("") != "" || FirstHashToken(strings.Repeat("g", 64)) != "" {
		t.Fatal("junk hash accepted")
	}
}
