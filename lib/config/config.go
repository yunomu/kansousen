package config

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

func openURL(url string) (io.ReadCloser, error) {
	switch {
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		res, err := http.Get(url)
		if err != nil {
			return nil, err
		}

		return res.Body, nil
	case strings.HasPrefix(url, "file://"):
		fallthrough
	default:
		file := strings.TrimPrefix(url, "file:///")
		return os.Open(file)
	}
}

func Load(url string) (map[string]string, error) {
	in, err := openURL(url)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	d := json.NewDecoder(in)
	ret := map[string]string{}
	if err := d.Decode(&ret); err != nil {
		return nil, err
	}

	return ret, nil
}
