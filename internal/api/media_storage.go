package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	saveErrorStoragePathInvalid     = "STORAGE_PATH_INVALID"
	saveErrorStoragePrepareFailed   = "STORAGE_PREPARE_FAILED"
	saveErrorTextEncodeFailed       = "TEXT_ENCODE_FAILED"
	saveErrorTextWriteFailed        = "TEXT_WRITE_FAILED"
	saveErrorMediaClientUnavailable = "MEDIA_CLIENT_UNAVAILABLE"
	saveErrorMediaTooLarge          = "MEDIA_TOO_LARGE"
	saveErrorMediaDownloadTimeout   = "MEDIA_DOWNLOAD_TIMEOUT"
	saveErrorMediaDownloadCanceled  = "MEDIA_DOWNLOAD_CANCELED"
	saveErrorMediaInvalid           = "MEDIA_INVALID"
	saveErrorMediaDownloadFailed    = "MEDIA_DOWNLOAD_FAILED"
	saveErrorMediaMetadataFailed    = "MEDIA_METADATA_FAILED"
	saveErrorResourceFailed         = "RESOURCE_SAVE_FAILED"
)

type mediaPersistencePolicy struct {
	Text   bool
	Images bool
	Videos bool
}

type mediaPersistenceResult struct {
	Kind         string
	Ordinal      int
	RemoteURL    string
	Status       string
	RelativePath string
	MIMEType     string
	SizeBytes    int64
	SHA256       string
	Error        string
	cause        error
}

type mediaPersistenceTask struct {
	result   mediaPersistenceResult
	download downloadTask
	enabled  bool
}

func failedPersistenceResult(result mediaPersistenceResult, code string, cause error) mediaPersistenceResult {
	result.Status = "failed"
	result.RelativePath = ""
	result.MIMEType = ""
	result.SizeBytes = 0
	result.SHA256 = ""
	result.Error = code
	result.cause = cause
	return result
}

func mediaPersistenceErrorCode(err error) string {
	switch {
	case errors.Is(err, errMediaTooLarge):
		return saveErrorMediaTooLarge
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, errDownloadIdleTimeout):
		return saveErrorMediaDownloadTimeout
	case errors.Is(err, context.Canceled):
		return saveErrorMediaDownloadCanceled
	case errors.Is(err, errInvalidMedia):
		return saveErrorMediaInvalid
	default:
		return saveErrorMediaDownloadFailed
	}
}

func persistVersionResources(
	ctx context.Context,
	client *http.Client,
	volumeDir, workID string,
	versionNumber int64,
	data map[string]any,
	policy mediaPersistencePolicy,
	coordinator *downloadCoordinator,
) []mediaPersistenceResult {
	results := make([]mediaPersistenceResult, 0)
	versionDir, relativeVersionDir, err := versionStorageDirectory(volumeDir, workID, versionNumber)
	if err != nil {
		result := failedPersistenceResult(mediaPersistenceResult{Kind: "text"}, saveErrorStoragePathInvalid, err)
		return []mediaPersistenceResult{result}
	}

	if policy.Text {
		results = append(results, persistVersionText(volumeDir, versionDir, relativeVersionDir, data))
	} else {
		results = append(results, mediaPersistenceResult{Kind: "text", Status: "disabled"})
	}

	tasks := versionMediaTasks(data, policy)
	if len(tasks) == 0 {
		return results
	}
	if coordinator == nil {
		coordinator = newDownloadCoordinator(defaultDownloadConcurrency)
	}
	if client == nil {
		cause := errors.New("HTTP client is unavailable")
		for _, task := range tasks {
			result := task.result
			if task.enabled {
				result = failedPersistenceResult(result, saveErrorMediaClientUnavailable, cause)
			}
			results = append(results, result)
		}
		return results
	}

	tempDir := filepath.Join(volumeDir, "Temp", safeFilename(workID), fmt.Sprintf("v%d", versionNumber))
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return appendFailedPersistenceTasks(results, tasks, err)
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return appendFailedPersistenceTasks(results, tasks, err)
	}

	mediaResults := make([]mediaPersistenceResult, len(tasks))
	var wait sync.WaitGroup
	for index, task := range tasks {
		index, task := index, task
		if !task.enabled {
			mediaResults[index] = task.result
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			result := task.result
			err := coordinator.withSlot(ctx, func() error {
				downloadContext := ctx
				cancel := func() {}
				if coordinator.totalTimeout > 0 {
					downloadContext, cancel = context.WithTimeout(ctx, coordinator.totalTimeout)
				}
				defer cancel()
				return downloadFile(
					downloadContext,
					client,
					mediaHeaders(),
					tempDir,
					versionDir,
					task.download,
					coordinator.idleTimeout,
					true,
					coordinator.maxMediaBytes,
				)
			})
			if err != nil {
				mediaResults[index] = failedPersistenceResult(result, mediaPersistenceErrorCode(err), err)
				return
			}

			path, extension, err := completedDownloadPath(versionDir, task.download)
			if err != nil {
				mediaResults[index] = failedPersistenceResult(result, saveErrorMediaMetadataFailed, err)
				return
			}
			metadata, err := artifactMetadata(path, extension)
			if err != nil {
				mediaResults[index] = failedPersistenceResult(result, saveErrorMediaMetadataFailed, err)
				return
			}
			result.Status = "stored"
			result.RelativePath = filepath.ToSlash(filepath.Join(relativeVersionDir, filepath.Base(path)))
			result.MIMEType = metadata.mimeType
			result.SizeBytes = metadata.size
			result.SHA256 = metadata.sha256
			mediaResults[index] = result
		}()
	}
	wait.Wait()
	return append(results, mediaResults...)
}

func versionMediaTasks(data map[string]any, policy mediaPersistencePolicy) []mediaPersistenceTask {
	urls, lives, err := mediaURLs(data)
	if err != nil {
		return nil
	}
	tasks := make([]mediaPersistenceTask, 0, len(urls)+len(lives)+1)
	if stringValue(data["作品类型"]) == "视频" {
		// Ordinal 0 is reserved for the video cover so ordinary media lists can exclude it.
		if coverURL := strings.TrimSpace(firstString(data["封面地址"])); coverURL != "" {
			tasks = append(tasks, mediaPersistenceTask{
				result:   mediaPersistenceResult{Kind: "image", Ordinal: 0, RemoteURL: coverURL, Status: persistenceStatus(policy.Images)},
				download: downloadTask{url: coverURL, baseName: "cover_000", extension: "jpeg"},
				enabled:  policy.Images,
			})
		}
		if len(urls) > 0 && strings.TrimSpace(urls[0]) != "" {
			tasks = append(tasks, mediaPersistenceTask{
				result:   mediaPersistenceResult{Kind: "video", Ordinal: 1, RemoteURL: urls[0], Status: persistenceStatus(policy.Videos)},
				download: downloadTask{url: urls[0], baseName: "video_001", extension: "mp4"},
				enabled:  policy.Videos,
			})
		}
		return tasks
	}

	for index, mediaURL := range urls {
		ordinal := index + 1
		if strings.TrimSpace(mediaURL) != "" {
			tasks = append(tasks, mediaPersistenceTask{
				result:   mediaPersistenceResult{Kind: "image", Ordinal: ordinal, RemoteURL: mediaURL, Status: persistenceStatus(policy.Images)},
				download: downloadTask{url: mediaURL, baseName: fmt.Sprintf("image_%03d", ordinal), extension: "jpeg"},
				enabled:  policy.Images,
			})
		}
		if index < len(lives) {
			if liveURL := firstString(lives[index]); liveURL != "" {
				tasks = append(tasks, mediaPersistenceTask{
					result:   mediaPersistenceResult{Kind: "video", Ordinal: ordinal, RemoteURL: liveURL, Status: persistenceStatus(policy.Videos)},
					download: downloadTask{url: liveURL, baseName: fmt.Sprintf("live_%03d", ordinal), extension: "mp4"},
					enabled:  policy.Videos,
				})
			}
		}
	}
	return tasks
}

func persistenceStatus(enabled bool) string {
	if enabled {
		return "pending"
	}
	return "disabled"
}

func appendFailedPersistenceTasks(results []mediaPersistenceResult, tasks []mediaPersistenceTask, err error) []mediaPersistenceResult {
	for _, task := range tasks {
		result := task.result
		if task.enabled {
			result = failedPersistenceResult(result, saveErrorStoragePrepareFailed, err)
		}
		results = append(results, result)
	}
	return results
}

func versionStorageDirectory(volumeDir, workID string, versionNumber int64) (string, string, error) {
	cleanID := safeFilename(workID)
	if cleanID == "" {
		return "", "", errors.New("work ID cannot be used as a storage path")
	}
	if versionNumber < 1 {
		return "", "", errors.New("version number must be positive")
	}
	relative := filepath.Join("Download", cleanID, fmt.Sprintf("v%d", versionNumber))
	return filepath.Join(volumeDir, relative), relative, nil
}

func persistVersionText(volumeDir, versionDir, relativeVersionDir string, data map[string]any) mediaPersistenceResult {
	result := mediaPersistenceResult{Kind: "text"}
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return failedPersistenceResult(result, saveErrorTextEncodeFailed, err)
	}
	content = append(content, '\n')
	target := filepath.Join(versionDir, "work.json")
	absolute, err := filepath.Abs(target)
	if err != nil {
		return failedPersistenceResult(result, saveErrorStoragePathInvalid, err)
	}
	root, err := filepath.Abs(volumeDir)
	if err != nil {
		return failedPersistenceResult(result, saveErrorStoragePathInvalid, err)
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return failedPersistenceResult(result, saveErrorStoragePathInvalid, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		cause := errors.New("text artifact escaped the configured volume")
		return failedPersistenceResult(result, saveErrorStoragePathInvalid, cause)
	}
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return failedPersistenceResult(result, saveErrorStoragePrepareFailed, err)
	}
	if err := writeAtomicFile(target, content, 0o600); err != nil {
		return failedPersistenceResult(result, saveErrorTextWriteFailed, err)
	}
	hash := sha256.Sum256(content)
	result.Status = "stored"
	result.RelativePath = filepath.ToSlash(filepath.Join(relativeVersionDir, "work.json"))
	result.MIMEType = "application/json"
	result.SizeBytes = int64(len(content))
	result.SHA256 = hex.EncodeToString(hash[:])
	return result
}

func writeAtomicFile(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".xhs-artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

type artifactFileMetadata struct {
	mimeType string
	size     int64
	sha256   string
}

func artifactMetadata(path, extension string) (artifactFileMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return artifactFileMetadata{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return artifactFileMetadata{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return artifactFileMetadata{}, err
	}
	return artifactFileMetadata{
		mimeType: mediaMIMEType(extension),
		size:     info.Size(),
		sha256:   hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func mediaMIMEType(extension string) string {
	switch strings.ToLower(extension) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	case "bmp":
		return "image/bmp"
	case "avif":
		return "image/avif"
	case "heic":
		return "image/heic"
	case "mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

func completedDownloadPath(downloadDir string, task downloadTask) (string, string, error) {
	extensions := []string{task.extension}
	if task.extension == "jpeg" {
		extensions = append(extensions, "jpg", "png", "webp", "gif", "bmp", "avif", "heic")
	}
	for _, extension := range extensions {
		path := filepath.Join(downloadDir, task.baseName+"."+extension)
		exists, err := regularFileExists(path)
		if err != nil {
			return "", "", err
		}
		if exists {
			return path, extension, nil
		}
	}
	return "", "", fmt.Errorf("download %s completed without a final artifact", task.baseName)
}
