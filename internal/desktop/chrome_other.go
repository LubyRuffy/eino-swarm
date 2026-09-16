//go:build !darwin

package desktop

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func keepTrafficLightsAligned(_ application.Window) {}

func positionTrafficLights(_ unsafe.Pointer) float64 { return 0 }
