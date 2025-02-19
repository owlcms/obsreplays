//go:build windows || linux

package recording

import "github.com/owlcms/obsreplays/internal/config"

// SetCameraConfigs sets the available camera configurations.
func SetCameraConfigs(configs []config.CameraConfiguration) {
	config.CameraConfigs = configs
}
