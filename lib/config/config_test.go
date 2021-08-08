package config

import (
	"testing"

	"io/ioutil"
	"os"
	"path/filepath"
)

func TestOpenURL(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-config")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "config.json")
	if err := ioutil.WriteFile(file, []byte(`{"key":"value"}`), 0666); err != nil {
		t.Fatalf("ioutil.WriteFile: file=%v, err=%v", file, err)
	}

	cfg, err := Load("file:///" + file)
	if err != nil {
		t.Fatalf("openURL: %v", err)
	}

	val, ok := cfg["key"]
	if !ok {
		t.Fatalf("key not found")
	}
	if val != "value" {
		t.Errorf("val is not `value`: `%v`", val)
	}
}
