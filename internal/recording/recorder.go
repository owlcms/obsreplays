package recording

import (
	"fmt"
	"io"
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

// InitializeRecorder sets up the OBS client connection
func InitializeRecorder() error {
	obsClient = NewOBSWebSocketClient()
	if err := obsClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to OBS WebSocket: %v", err)
	}
	return nil
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
	// reset the Replay Source plugin and start recording
	if err := obsClient.TriggerHotkey("OBS_KEY_F6"); err != nil {
		return fmt.Errorf("failed to send F6 hotkey to OBS: %w", err)
	}
	if err := obsClient.TriggerHotkey("OBS_KEY_F7"); err != nil {
		return fmt.Errorf("failed to send F7 hotkey to OBS: %w", err)
	}

	httpServer.SendStatus(httpServer.Recording, fmt.Sprintf("Recording: %s - %s attempt %d",
		strings.ReplaceAll(fullName, "_", " "),
		liftTypeKey,
		attemptNumber))

	logging.InfoLogger.Printf("Started recording")
	return nil
}

// StopRecording stops the current recordings and trims the videos
func StopRecording(decisionTime int64) error {
	captureDir := filepath.Join(os.Getenv("USERPROFILE"), "Videos", "Captures")

	// Stop recording and free files
	if err := obsClient.TriggerHotkey("OBS_KEY_F8"); err != nil {
		return fmt.Errorf("failed to send F8 hotkey to OBS: %w", err)
	}
	if err := obsClient.TriggerHotkey("OBS_KEY_F6"); err != nil {
		return fmt.Errorf("failed to send F6 hotkey to OBS: %w", err)
	}

	// Give OBS a moment to finish writing files
	time.Sleep(3 * time.Second)

	// Find Camera*.flv files in captures directory
	files, err := os.ReadDir(captureDir)
	if err != nil {
		return fmt.Errorf("failed to read captures directory: %w", err)
	}

	var cameraFiles []string
	for _, file := range files {
		name := file.Name()
		if strings.HasSuffix(name, ".flv") && strings.Contains(name, "Camera") {
			// Extract just the "Camera*" portion from the filename
			parts := strings.Split(name, "Camera")
			if len(parts) > 1 {
				cameraNum := strings.TrimSuffix(parts[1], ".flv")
				simpleFileName := fmt.Sprintf("Camera%s.flv", cameraNum)
				cameraFiles = append(cameraFiles, filepath.Join(captureDir, simpleFileName))
			}
		}
	}

	if len(cameraFiles) == 0 {
		return fmt.Errorf("no camera files found in captures directory %s", captureDir)
	}

	// Calculate trimming parameters
	startTime := state.LastStartTime
	trimDuration := state.LastTimerStopTime - startTime - 5000
	logging.InfoLogger.Printf("Duration to be trimmed: %d milliseconds", trimDuration)

	// First pass: trim each camera file to simple MP4
	var trimmedFiles []string
	for _, cameraFile := range cameraFiles {
		// Use simple Camera*.mp4 name for trimmed file
		baseFileName := strings.TrimSuffix(filepath.Base(cameraFile), ".flv")
		trimmedFile := filepath.Join(captureDir, baseFileName+".mp4")
		trimmedFiles = append(trimmedFiles, trimmedFile)

		cameraNum := strings.TrimPrefix(baseFileName, "Camera")

		attemptInfo := fmt.Sprintf("%s - %s attempt %d",
			strings.ReplaceAll(state.CurrentAthlete, "_", " "),
			state.CurrentLiftType,
			state.CurrentAttempt)

		httpServer.SendStatus(httpServer.Trimming, fmt.Sprintf("Trimming video for Camera %s: %s", cameraNum, attemptInfo))

		// Trim and convert to MP4
		args := buildTrimmingArgs(trimDuration, cameraFile, trimmedFile)
		cmd := createFfmpegCmd(args)
		logging.InfoLogger.Printf("Executing trim command for Camera %s: %s", cameraNum, cmd.String())

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to trim video for Camera %s: %w", cameraNum, err)
		}

		// Remove the original .flv file
		if err := os.Remove(cameraFile); err != nil {
			logging.WarningLogger.Printf("Failed to remove source .flv file for Camera %s: %v", cameraNum, err)
		}
	}

	// Create session directory for final copies
	sessionDir := state.CurrentSession
	if sessionDir == "" {
		sessionDir = "unsorted"
	}
	sessionDir = strings.ReplaceAll(sessionDir, " ", "_")
	fullSessionDir := filepath.Join(config.GetVideoDir(), sessionDir)
	if err := os.MkdirAll(fullSessionDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	// Second pass: copy trimmed files to final destination
	timestamp := time.Now().Format("2006-01-02_15h04m05s")
	baseFileName := fmt.Sprintf("%s_%s_%s_attempt%d",
		timestamp,
		strings.ReplaceAll(state.CurrentAthlete, " ", "_"),
		state.CurrentLiftType,
		state.CurrentAttempt)

	var finalFiles []string
	for _, trimmedFile := range trimmedFiles {
		cameraNum := strings.TrimPrefix(filepath.Base(trimmedFile), "Camera")
		cameraNum = strings.TrimSuffix(cameraNum, ".mp4")
		finalFileName := filepath.Join(fullSessionDir, fmt.Sprintf("%s_Camera%s.mp4", baseFileName, cameraNum))
		finalFiles = append(finalFiles, finalFileName)

		// Copy the MP4 file to final destination (using io.Copy to keep the original)
		sourceFile, err := os.Open(trimmedFile)
		if err != nil {
			return fmt.Errorf("failed to open source file for Camera %s: %w", cameraNum, err)
		}
		defer sourceFile.Close()

		destFile, err := os.Create(finalFileName)
		if err != nil {
			return fmt.Errorf("failed to create destination file for Camera %s: %w", cameraNum, err)
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, sourceFile); err != nil {
			return fmt.Errorf("failed to copy video to final location for Camera %s: %w", cameraNum, err)
		}
	}

	httpServer.SendStatus(httpServer.Ready, "Videos ready")
	logging.InfoLogger.Printf("Processed videos: %v", finalFiles)

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
