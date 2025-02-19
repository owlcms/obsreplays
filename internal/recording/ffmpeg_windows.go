//go:build windows && !darwin && !linux

package recording

import (
	"os/exec"
	"syscall"

	"github.com/owlcms/obsreplays/internal/logging"
	"golang.org/x/sys/windows"
)

// createFfmpegCmd creates an exec.Cmd for ffmpeg with Windows-specific process attributes
func createFfmpegCmd(args []string) *exec.Cmd {
	path := "ffmpeg.exe"

	// If no path configured, try to find ffmpeg.exe in PATH
	var err error
	path, err = exec.LookPath("ffmpeg.exe")
	if err != nil {
		logging.ErrorLogger.Printf("ffmpeg.exe not found in PATH: %v", err)
		// Use default name, will fail if not in current directory
		path = "ffmpeg.exe"
	}

	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}

	return cmd
}
