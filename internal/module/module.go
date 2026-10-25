// Package module implements the built-in capture and restore modules. Each
// module is a thin planner around a well-known Windows tool or registry area;
// the actual file transport is always restic.
package module

import "github.com/mithun-srinivasan/ditch-windows/internal/core"

// All returns the modules in display order.
func All() []core.Module {
	return []core.Module{
		NewWinget(),
		NewDefaultApps(),
		NewWifi(),
		NewFonts(),
		NewTheme(),
		NewFiles(),
	}
}
