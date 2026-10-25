//go:build windows

package core

import (
	"os"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

// HostInfo contains host metadata recorded in every backup manifest.
type HostInfo struct {
	Hostname           string
	Username           string
	WindowsBuild       int
	WindowsInstallDate int64
}

// CurrentHostInfo reads host metadata, ignoring unavailable values.
func CurrentHostInfo() HostInfo {
	info := HostInfo{Username: os.Getenv("USERNAME")}
	info.Hostname, _ = os.Hostname()
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if v, _, err := k.GetStringValue("CurrentBuildNumber"); err == nil {
			info.WindowsBuild, _ = strconv.Atoi(v)
		}
		if v, _, err := k.GetIntegerValue("InstallDate"); err == nil {
			info.WindowsInstallDate = int64(v)
		}
	}
	return info
}
