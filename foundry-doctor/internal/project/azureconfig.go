package project

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
)

// readDefaultEnvironment returns defaultEnvironment from .azure/config.json,
// the file azd uses to remember the default environment. Problems become
// warnings and yield "". Only that one string field is used.
func readDefaultEnvironment(r *os.Root, warn map[string]bool) string {
	rel := path.Join(AzureDir, AzureConfigFile)
	fi, err := r.Lstat(rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warn[rel+" is unreadable; default environment unknown"] = true
		}
		return ""
	}
	if !fi.Mode().IsRegular() || fi.Size() > MaxAzureConfigSize {
		warn[rel+" is not a regular file or exceeds the size limit; default environment unknown"] = true
		return ""
	}
	f, err := r.Open(rel)
	if err != nil {
		warn[rel+" is unreadable; default environment unknown"] = true
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxAzureConfigSize+1))
	var cfg struct {
		DefaultEnvironment string `json:"defaultEnvironment"`
	}
	if err != nil || len(data) > MaxAzureConfigSize || json.Unmarshal(data, &cfg) != nil {
		warn[rel+" is not valid JSON; default environment unknown"] = true
		return ""
	}
	if cfg.DefaultEnvironment != "" && !envNameRE.MatchString(cfg.DefaultEnvironment) {
		warn[rel+": defaultEnvironment is not a valid environment name; ignored"] = true
		return ""
	}
	return cfg.DefaultEnvironment
}
