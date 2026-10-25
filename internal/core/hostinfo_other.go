//go:build !windows

package core

import "os"

// HostInfo contains host metadata recorded in every backup manifest.
type HostInfo struct {
	Hostname           string
	Username           string
	WindowsBuild       int
	WindowsInstallDate int64
}

// CurrentHostInfo reads host metadata.
func CurrentHostInfo() HostInfo {
	info := HostInfo{Username: os.Getenv("USER")}
	info.Hostname, _ = os.Hostname()
	return info
}
