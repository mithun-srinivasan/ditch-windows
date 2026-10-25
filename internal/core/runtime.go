package core

import "runtime"

func runtimeVersion() string { return runtime.Version() }
func runtimeOS() string      { return runtime.GOOS }
func runtimeArch() string    { return runtime.GOARCH }
