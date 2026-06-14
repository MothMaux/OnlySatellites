package config

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pelletier/go-toml/v2"
)

type ImageDirConfig struct {
	IsFilled    bool   `toml:"isFilled"`
	VPix        int    `toml:"vPix"`
	Sensor      string `toml:"sensor"`
	IsCorrected bool   `toml:"corrected"`
	Composite   string `toml:"composite"`
}

type PassTypeConfig struct {
	DatasetFile string
	RawDataFile string
	Downlink    string
	ImageDirs   map[string]ImageDirConfig
}

type PassesConfig struct {
	FolderIncludes map[string]string `toml:"folderincludes"`
}

type PassConfig struct {
	Composites map[string]string         `toml:"composites"`
	PassTypes  map[string]PassTypeConfig `toml:"passTypes"`
	Passes     PassesConfig              `toml:"passes"`
}

type SettingsTree map[string]any
type SettingsFlat map[string]any

var (
	treeStore atomic.Value // SettingsTree
	flatStore atomic.Value // SettingsFlat
	cfgPath   string       // config file location
	mu        sync.Mutex
)

func flatten(prefix string, in map[string]any, out map[string]any) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		switch val := v.(type) {
		case map[string]any:
			flatten(key, val, out)
		default:
			out[key] = val
		}
	}
}

func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	cfgPath = path

	tree := SettingsTree(raw)
	flat := make(SettingsFlat)
	flatten("", tree, flat)

	treeStore.Store(tree)
	flatStore.Store(flat)

	return nil
}

func Get(key string) (any, bool) {
	flat := flatStore.Load().(SettingsFlat)
	v, ok := flat[key]
	return v, ok
}

func MustGet(key string) any {
	v, ok := Get(key)
	if !ok {
		panic("missing config key: " + key)
	}
	return v
}

func GetString(key string) string {
	if v, ok := Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "nilStrAddr"
}

func GetInt(key string) int {
	if v, ok := Get(key); ok {
		switch val := v.(type) {
		case int64:
			return int(val)
		case float64:
			return int(val)
		case int:
			return val
		}
	}
	return 0
}

func GetBool(key string) bool {
	if v, ok := Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func GetNode(path string) (map[string]any, bool) {
	tree := treeStore.Load().(SettingsTree)

	parts := strings.Split(path, ".")
	var current any = tree

	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[p]
		if !ok {
			return nil, false
		}
	}

	out, ok := current.(map[string]any)
	return out, ok
}

// Set

func setInTree(tree map[string]any, key string, value any) error {
	parts := strings.Split(key, ".")
	last := len(parts) - 1

	current := tree
	for i, p := range parts {
		if i == last {
			current[p] = value
			return nil
		}

		next, ok := current[p]
		if !ok {
			newMap := make(map[string]any)
			current[p] = newMap
			current = newMap
			continue
		}

		m, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("path conflict at %s", p)
		}

		current = m
	}

	return nil
}

func saveLocked(tree SettingsTree) error {
	data, err := toml.Marshal(tree)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(cfgPath, data, 0644)
}

func Set(key string, value any) error {
	mu.Lock()
	defer mu.Unlock()

	tree := treeStore.Load().(SettingsTree)

	if err := setInTree(tree, key, value); err != nil {
		return err
	}

	newFlat := make(SettingsFlat)
	flatten("", tree, newFlat)

	treeStore.Store(tree)
	flatStore.Store(newFlat)

	return saveLocked(tree)
}

// Defaults & Loaders

func makeDirectories() error {
	dirs := []string{
		GetString("paths.data"),
		GetString("paths.live_output"),
		GetString("paths.logs"),
		GetString("paths.thumbnails"),
	}
	for _, dir := range dirs {
		if dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}
	return nil
}
