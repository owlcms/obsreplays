package recording

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/owlcms/obsreplays/internal/config"
	"github.com/owlcms/obsreplays/internal/httpServer"
	"github.com/owlcms/obsreplays/internal/logging"
	"github.com/owlcms/obsreplays/internal/state"
)

var (
	currentFileNames []string
	obsClient        *OBSWebSocketClient
)

func init() {
	obsClient = NewOBSWebSocketClient()
	if err := obsClient.Connect(); err != nil {
		logging.ErrorLogger.Fatalf("Failed to connect to OBS WebSocket: %v", err)
	}
}

// buildTrimmingArgs builds the ffmpeg arguments for trimming
func buildTrimmingArgs(trimDuration int64, currentFileName, finalFileName string) []string {
	args := []string{"-y"}
	if trimDuration > 0 {
		args = append(args, "-ss", fmt.Sprintf("%d", trimDuration/1000))
	}
	args = append(args,
		"-i", currentFileName,
		"-c", "copy",
	)
	args = append(args, finalFileName)
	return args
}

// StartRecording starts recording videos using OBS
func StartRecording(fullName, liftTypeKey string, attemptNumber int) error {
	if err := os.MkdirAll(config.GetVideoDir(), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create video directory: %w", err)
	}

	fullName = strings.ReplaceAll(fullName, " ", "_")

	var fileNames []string

	fileName := filepath.Join(config.GetVideoDir(), fmt.Sprintf("%s_%s_attempt%d_%d.mp4", fullName, liftTypeKey, attemptNumber, state.LastStartTime))
	fileNames = append(fileNames, fileName)

	currentFileNames = fileNames
	state.LastTimerStopTime = 0

	if err := obsClient.TriggerHotkey("OBS_KEY_F7"); err != nil {
		return fmt.Errorf("failed to send F7 hotkey to OBS: %w", err)
	}

	httpServer.SendStatus(httpServer.Recording, fmt.Sprintf("Recording: %s - %s attempt %d",
		strings.ReplaceAll(fullName, "_", " "),
		liftTypeKey,
		attemptNumber))

	logging.InfoLogger.Printf("Started recording videos: %v", fileNames)
	return nil
}

// StopRecording stops the current recordings and trims the videos
func StopRecording(decisionTime int64) error {
	if len(currentFileNames) == 0 && !config.NoVideo {
		return fmt.Errorf("no ongoing recordings to stop")
	}

	if err := obsClient.TriggerHotkey("OBS_KEY_F8"); err != nil {
		return fmt.Errorf("failed to send F8 hotkey to OBS: %w", err)
	}

	startTime := state.LastStartTime
	trimDuration := state.LastTimerStopTime - startTime - 5000
	logging.InfoLogger.Printf("Duration to be trimmed: %d milliseconds", trimDuration)

	timestamp := time.Now().Format("2006-01-02_15h04m05s")
	var finalFileNames []string

	// Create session directory if it doesn't exist
	sessionDir := state.CurrentSession
	if sessionDir == "" {
		sessionDir = "unsorted"
	}
	sessionDir = strings.ReplaceAll(sessionDir, " ", "_")
	fullSessionDir := filepath.Join(config.GetVideoDir(), sessionDir)
	if err := os.MkdirAll(fullSessionDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	for i, currentFileName := range currentFileNames {
		baseFileName := strings.TrimSuffix(filepath.Base(currentFileName), filepath.Ext(currentFileName))
		baseFileName = baseFileName[:len(baseFileName)-len(fmt.Sprintf("_%d", state.LastStartTime))]
		finalFileName := filepath.Join(fullSessionDir, fmt.Sprintf("%s_%s.mp4", timestamp, baseFileName))
		finalFileNames = append(finalFileNames, finalFileName)

		attemptInfo := fmt.Sprintf("%s - %s attempt %d",
			strings.ReplaceAll(state.CurrentAthlete, "_", " "),
			state.CurrentLiftType,
			state.CurrentAttempt)

		httpServer.SendStatus(httpServer.Trimming, fmt.Sprintf("Trimming video for Camera %d: %s", i+1, attemptInfo))

		var err error
		if startTime == 0 {
			logging.InfoLogger.Printf("Start time is 0, not trimming the video for Camera %d", i+1)
			if config.NoVideo {
				logging.InfoLogger.Printf("Simulating rename video for Camera %d: %s -> %s", i+1, currentFileName, finalFileName)
			} else if err = os.Rename(currentFileName, finalFileName); err != nil {
				return fmt.Errorf("failed to rename video file for Camera %d to %s: %w", i+1, finalFileName, err)
			}
		} else {
			for j := 0; j < 5; j++ {
				args := buildTrimmingArgs(trimDuration, currentFileName, finalFileName)
				cmd := createFfmpegCmd(args)

				if j == 0 {
					logging.InfoLogger.Printf("Executing trim command for Camera %d: %s", i+1, cmd.String())
				}

				if err = cmd.Run(); err != nil {
					logging.ErrorLogger.Printf("Waiting for input video for Camera %d (attempt %d/5): %v", i+1, j+1, err)
					time.Sleep(1 * time.Second)
				} else {
					break
				}
				if j == 4 {
					return fmt.Errorf("failed to open input video for Camera %d after 5 attempts: %w", i+1, err)
				}
			}
			if err = os.Remove(currentFileName); err != nil {
				return fmt.Errorf("failed to remove untrimmed video file for Camera %d: %w", i+1, err)
			}
		}
	}

	// Send single "Videos ready" message after all cameras are done
	httpServer.SendStatus(httpServer.Ready, "Videos ready")

	logging.InfoLogger.Printf("Stopped recording and saved videos: %v", finalFileNames)
	currentFileNames = nil

	return nil
}

func ForceStopRecordings() {
	if config.NoVideo {
		for i, fileName := range currentFileNames {
			logging.InfoLogger.Printf("Simulating forced stop recording video for Camera %d: %s", i+1, fileName)
		}
	} else {
		if err := obsClient.TriggerHotkey("OBS_KEY_F8"); err != nil {
			logging.ErrorLogger.Printf("Failed to send F8 hotkey to OBS: %v", err)
		}
	}
}

// GetStartTimeMillis returns the start time in milliseconds
func GetStartTimeMillis() string {
	return strconv.FormatInt(state.LastStartTime, 10)
}
