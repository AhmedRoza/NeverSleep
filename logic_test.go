package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v2.0.1", "2.0.0", true}, {"v2.0.0", "2.0.0", false}, {"v1.9.9", "2.0.0", false},
		{"v10.0.0", "9.9.9", true}, {"junk", "2.0.0", false}, {"v2.0", "2.0.0", false},
	}
	for _, c := range cases {
		if NewerVersion(c.a, c.b) != c.want {
			t.Errorf("%s vs %s", c.a, c.b)
		}
	}
}

func TestSettingsMigrateFromV13(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "settings.json")
	os.WriteFile(f, []byte(`{"keepScreenOn":true,"durationMinutes":187,"acOnly":true,"autoStart":true}`), 0o644)
	s := LoadSettings(f)
	if !s.KeepScreenOn || s.DurationMinutes != 187 || !s.AcOnly || !s.AutoStart || !s.ShowElapsed || !s.AutoUpdate {
		t.Fatalf("%+v", s)
	}
	os.WriteFile(f, []byte(`{"durationMinutes":99999,"keepScreenOn":"x"}`), 0o644)
	s = LoadSettings(f)
	if s.DurationMinutes != 0 || s.KeepScreenOn {
		t.Fatalf("invalid values accepted %+v", s)
	}
	os.WriteFile(f, []byte(`not json`), 0o644)
	if LoadSettings(f) != DefaultSettings() {
		t.Fatal("corrupt file")
	}
	if err := SaveSettings(f, Settings{DurationMinutes: 60}); err != nil || LoadSettings(f).DurationMinutes != 60 {
		t.Fatal("roundtrip")
	}
}

func TestWanted(t *testing.T) {
	s := Settings{KeepScreenOn: true, AcOnly: true}
	if a, d := Wanted(true, s, true); a || d {
		t.Fatal("battery should pause")
	}
	if a, d := Wanted(true, s, false); !a || !d {
		t.Fatal("ac on")
	}
	if a, _ := Wanted(false, s, false); a {
		t.Fatal("inactive")
	}
}

func TestHash(t *testing.T) {
	h := "5d4145067c40791b6d5f931cd21d3d6b6e2a4f46371554c0c6a5e018cc31bb0d"
	if FirstHashToken(h+"  x.exe\n") != h || FirstHashToken("zz") != "" {
		t.Fatal("hash")
	}
}

func fakeRelease(tag string, size int) []byte {
	p := "https://github.com/" + Repo + "/releases/download/" + tag + "/"
	b, _ := json.Marshal(map[string]interface{}{"tag_name": tag, "assets": []map[string]interface{}{
		{"name": assetExe, "browser_download_url": p + assetExe, "size": size},
		{"name": assetHash, "browser_download_url": p + assetHash, "size": 70},
	}})
	return b
}

func TestPlanUpdate(t *testing.T) {
	exe := append([]byte("MZ"), make([]byte, 2<<20)...)
	sum := sha256.Sum256(exe)
	h := hex.EncodeToString(sum[:])
	get := func(hash string) Getter {
		return func(u string, l int64) ([]byte, error) {
			if strings.HasSuffix(u, ".sha256") {
				return []byte(hash + "  NeverSleep.exe\n"), nil
			}
			return exe, nil
		}
	}
	tag, got, err := PlanUpdate(fakeRelease("v2.1.0", len(exe)), "2.0.0", get(h))
	if err != nil || tag != "v2.1.0" || len(got) != len(exe) {
		t.Fatal("good update rejected", err)
	}
	if _, _, err := PlanUpdate(fakeRelease("v2.1.0", len(exe)), "2.0.0", get(strings.Repeat("0", 64))); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	if _, _, err := PlanUpdate(fakeRelease("v2.0.0", len(exe)), "2.0.0", get(h)); err != ErrNoUpdate {
		t.Fatal("same version accepted")
	}
	if _, _, err := PlanUpdate(fakeRelease("v1.9.0", len(exe)), "2.0.0", get(h)); err != ErrNoUpdate {
		t.Fatal("downgrade accepted")
	}
	if _, _, err := PlanUpdate(fakeRelease("v2.1.0", len(exe)+5), "2.0.0", get(h)); err == nil {
		t.Fatal("size mismatch accepted")
	}
	bad := bytes.Replace(fakeRelease("v2.1.0", len(exe)), []byte("github.com/"+Repo), []byte("github.com/evil/x"), -1)
	if _, _, err := PlanUpdate(bad, "2.0.0", get(h)); err == nil {
		t.Fatal("foreign url accepted")
	}
	exe2 := append([]byte("PK"), exe[2:]...)
	s2 := sha256.Sum256(exe2)
	getBad := func(u string, l int64) ([]byte, error) {
		if strings.HasSuffix(u, ".sha256") {
			return []byte(hex.EncodeToString(s2[:])), nil
		}
		return exe2, nil
	}
	if _, _, err := PlanUpdate(fakeRelease("v2.1.0", len(exe2)), "2.0.0", getBad); err == nil {
		t.Fatal("non-exe accepted")
	}
	if _, _, err := PlanUpdate([]byte(`{"tag_name":"v3.0.0","prerelease":true}`), "2.0.0", get(h)); err != ErrNoUpdate {
		t.Fatal("prerelease accepted")
	}
}

func TestThemeAccent(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "settings.json")
	os.WriteFile(f, []byte(`{"theme":"light","accent":"#FF8800"}`), 0o644)
	s := LoadSettings(f)
	if s.Theme != "light" || s.Accent != "#ff8800" {
		t.Fatalf("%+v", s)
	}
	os.WriteFile(f, []byte(`{"theme":"neon","accent":"red;x"}`), 0o644)
	s = LoadSettings(f)
	if s.Theme != "amoled" || s.Accent != "#00e5c8" {
		t.Fatalf("invalid accepted %+v", s)
	}
	if ValidAccent("#12345") || ValidAccent("123456#") || ValidAccent("#12345g") || !ValidAccent("#0aF9c1") {
		t.Fatal("accent validation")
	}
}
