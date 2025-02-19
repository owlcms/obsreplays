package recording

import (
	"runtime"
)

// Configuration variables exported for use within the recording package
var (
	FfmpegPath   string
	FfmpegCamera string
	FfmpegFormat string
	FfmpegParams string
	NoVideo      bool
	videoDir     string
)

// SetNoVideo sets the noVideo flag
func SetNoVideo(value bool) {
	NoVideo = value
}

// SetVideoDir sets the directory where videos will be stored
func SetVideoDir(dir string) {
	videoDir = dir
}

// GetVideoDir returns the absolute directory where videos are stored
func GetVideoDir() string {
	return videoDir
}

// SetFfmpegConfig sets the ffmpeg configuration parameters
func SetFfmpegConfig(path, camera, format string, params string) {
	if path == "" {
		if runtime.GOOS == "windows" {
			FfmpegPath = "ffmpeg.exe"
		} else {
			FfmpegPath = "ffmpeg"
		}
	} else {
		FfmpegPath = path
	}
	FfmpegCamera = camera
	FfmpegFormat = format
	FfmpegParams = params
}
