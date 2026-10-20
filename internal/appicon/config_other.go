//go:build !linux

package appicon

func defaultConfig() Config {
	return Config{MaxIconBytes: defaultMaxIconBytes}
}
