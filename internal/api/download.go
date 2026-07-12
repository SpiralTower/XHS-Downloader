package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const defaultDownloadConcurrency = 4

var (
	errDownloadIdleTimeout = errors.New("media download idle timeout")
	errInvalidMedia        = errors.New("invalid media content")
)

type downloadTask struct {
	url       string
	baseName  string
	extension string
}

type downloadLimits struct {
	totalTimeout time.Duration
	idleTimeout  time.Duration
}

type workDownloadLock struct {
	token chan struct{}
	refs  int
}

type downloadCoordinator struct {
	slots chan struct{}

	totalTimeout time.Duration
	idleTimeout  time.Duration

	mu    sync.Mutex
	works map[string]*workDownloadLock
}

func newDownloadCoordinator(concurrency int, limits ...downloadLimits) *downloadCoordinator {
	if concurrency < 1 {
		concurrency = 1
	}
	var configured downloadLimits
	if len(limits) > 0 {
		configured = limits[0]
	}
	return &downloadCoordinator{
		slots:        make(chan struct{}, concurrency),
		totalTimeout: configured.totalTimeout,
		idleTimeout:  configured.idleTimeout,
		works:        make(map[string]*workDownloadLock),
	}
}

func (c *downloadCoordinator) lockWork(ctx context.Context, workID string) (func(), error) {
	c.mu.Lock()
	lock := c.works[workID]
	if lock == nil {
		lock = &workDownloadLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		c.works[workID] = lock
	}
	lock.refs++
	c.mu.Unlock()

	select {
	case <-lock.token:
		var once sync.Once
		return func() {
			once.Do(func() {
				lock.token <- struct{}{}
				c.releaseWork(workID, lock)
			})
		}, nil
	case <-ctx.Done():
		c.releaseWork(workID, lock)
		return nil, ctx.Err()
	}
}

func (c *downloadCoordinator) releaseWork(workID string, lock *workDownloadLock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock.refs--
	if lock.refs == 0 && c.works[workID] == lock {
		delete(c.works, workID)
	}
}

func (c *downloadCoordinator) withSlot(ctx context.Context, function func() error) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
		return function()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func downloadWork(
	ctx context.Context,
	client *http.Client,
	volumeDir string,
	data map[string]any,
	indexes []any,
	coordinator *downloadCoordinator,
) error {
	urls, lives, err := mediaURLs(data)
	if err != nil || len(urls) == 0 {
		return errors.New("no downloadable media URL found")
	}
	selected, err := selectedIndexes(indexes)
	if err != nil {
		return err
	}
	if coordinator == nil {
		coordinator = newDownloadCoordinator(defaultDownloadConcurrency)
	}

	baseName := downloadBaseName(data)
	if baseName == "" {
		return errors.New("unable to generate download filename")
	}

	tasks := make([]downloadTask, 0, len(urls)+len(lives))
	if stringValue(data["作品类型"]) == "视频" {
		if urls[0] != "" {
			tasks = append(tasks, downloadTask{url: urls[0], baseName: baseName, extension: "mp4"})
		}
	} else {
		for index, mediaURL := range urls {
			position := index + 1
			if selected != nil {
				if _, ok := selected[position]; !ok {
					continue
				}
			}
			name := fmt.Sprintf("%s_%d", baseName, position)
			if mediaURL != "" {
				tasks = append(tasks, downloadTask{url: mediaURL, baseName: name, extension: "jpeg"})
			}
			if index < len(lives) {
				if liveURL := firstString(lives[index]); liveURL != "" {
					tasks = append(tasks, downloadTask{url: liveURL, baseName: name + "_live", extension: "mp4"})
				}
			}
		}
	}
	if len(tasks) == 0 {
		return errors.New("selected indexes produced no download tasks")
	}

	downloadDir := filepath.Join(volumeDir, "Download")
	tempDir := filepath.Join(volumeDir, "Temp")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}

	var wait sync.WaitGroup
	var errorMu sync.Mutex
	var downloadErrors []error
	for _, task := range tasks {
		task := task
		wait.Add(1)
		go func() {
			defer wait.Done()
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
					downloadDir,
					task,
					coordinator.idleTimeout,
					true,
				)
			})
			if err != nil {
				errorMu.Lock()
				downloadErrors = append(downloadErrors, err)
				errorMu.Unlock()
			}
		}()
	}
	wait.Wait()
	return errors.Join(downloadErrors...)
}

func downloadBaseName(data map[string]any) string {
	workID := safeFilename(stringValue(data["作品ID"]))
	details := safeFilename(strings.Join([]string{
		stringValue(data["发布时间"]),
		stringValue(data["作者昵称"]),
		stringValue(data["作品标题"]),
	}, "_"))
	switch {
	case workID == "":
		return details
	case details == "":
		return workID
	default:
		return safeFilename(workID + "_" + details)
	}
}

type partialMetadata struct {
	URL          string
	ETag         string
	LastModified string
}

func (m partialMetadata) validator() string {
	if m.ETag != "" {
		return m.ETag
	}
	return m.LastModified
}

func downloadFile(
	ctx context.Context,
	client *http.Client,
	headers http.Header,
	tempDir, downloadDir string,
	task downloadTask,
	idleTimeout time.Duration,
	allowResumeRetry bool,
) error {
	secureURL, err := normalizedMediaRequestURL(task.url)
	if err != nil {
		return err
	}
	task.url = secureURL
	exists, err := completedDownloadExists(downloadDir, task)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	partial := filepath.Join(tempDir, task.baseName+"."+task.extension+".part")
	metadataPath := partial + ".json"
	offset, metadata, err := loadPartialState(partial, metadataPath, task.url)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, task.url, nil)
	if err != nil {
		return err
	}
	copyHeaders(request.Header, headers)
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		request.Header.Set("If-Range", metadata.validator())
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", task.baseName, err)
	}

	appendPartial := false
	expectedTotal := int64(0)
	switch {
	case offset > 0 && response.StatusCode == http.StatusPartialContent:
		total, validRange := parseContentRange(
			response.Header.Get("Content-Range"),
			offset,
			response.ContentLength,
		)
		if !validRange || !resumeValidatorMatches(metadata, response.Header) {
			return retryDownloadFromStart(
				ctx, client, headers, tempDir, downloadDir, task, idleTimeout,
				allowResumeRetry, response, partial, metadataPath,
				fmt.Errorf("download %s: invalid partial response", task.baseName),
			)
		}
		expectedTotal = total
		appendPartial = true
	case offset > 0 && response.StatusCode == http.StatusOK:
		offset = 0
		metadata = partialMetadata{}
	case offset > 0 && response.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		return retryDownloadFromStart(
			ctx, client, headers, tempDir, downloadDir, task, idleTimeout,
			allowResumeRetry, response, partial, metadataPath,
			fmt.Errorf("download %s: upstream returned %s", task.baseName, response.Status),
		)
	case offset == 0 && response.StatusCode == http.StatusPartialContent:
		return retryDownloadFromStart(
			ctx, client, headers, tempDir, downloadDir, task, idleTimeout,
			allowResumeRetry, response, partial, metadataPath,
			fmt.Errorf("download %s: unexpected partial response", task.baseName),
		)
	case response.StatusCode != http.StatusOK:
		defer response.Body.Close()
		return fmt.Errorf("download %s: upstream returned %s", task.baseName, response.Status)
	}
	if response.StatusCode == http.StatusOK && response.ContentLength > 0 {
		expectedTotal = response.ContentLength
	}
	defer response.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if appendPartial {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partial, flags, 0o644)
	if err != nil {
		return err
	}

	if !appendPartial {
		metadata = partialMetadataFromResponse(task.url, response.Header)
		if err := persistPartialMetadata(metadataPath, metadata); err != nil {
			_ = file.Close()
			_ = resetPartialState(partial, metadataPath)
			return err
		}
	}

	body := &idleTimeoutReadCloser{ReadCloser: response.Body, timeout: idleTimeout}
	_, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if transferErr := errors.Join(copyErr, closeErr); transferErr != nil {
		keepPartial, stateErr := partialCanResume(partial, expectedTotal, metadata)
		transferErr = errors.Join(transferErr, stateErr)
		if !keepPartial {
			transferErr = errors.Join(transferErr, resetPartialState(partial, metadataPath))
		}
		return fmt.Errorf("download %s: %w", task.baseName, transferErr)
	}
	keepPartial, sizeErr := checkTransferredSize(partial, expectedTotal, metadata)
	if sizeErr != nil {
		if !keepPartial {
			sizeErr = errors.Join(sizeErr, resetPartialState(partial, metadataPath))
		}
		return fmt.Errorf("download %s: %w", task.baseName, sizeErr)
	}

	extension, err := validatedMediaExtension(partial, task.extension)
	if err != nil {
		_ = resetPartialState(partial, metadataPath)
		return fmt.Errorf("download %s: %w", task.baseName, err)
	}
	target := filepath.Join(downloadDir, task.baseName+"."+extension)
	targetExists, err := regularFileExists(target)
	if err != nil {
		return err
	}
	if targetExists {
		if _, validationErr := validatedMediaExtension(target, task.extension); validationErr == nil {
			return resetPartialState(partial, metadataPath)
		} else if !errors.Is(validationErr, errInvalidMedia) {
			return validationErr
		}
		if err := os.Remove(target); err != nil {
			return err
		}
	}
	if err := os.Rename(partial, target); err != nil {
		return fmt.Errorf("finalize %s: %w", task.baseName, err)
	}
	if err := removeIfExists(metadataPath); err != nil {
		return err
	}
	return removeIfExists(metadataPath + ".tmp")
}

func normalizedMediaRequestURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("media URL must be an http or https URL without credentials")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		parsed.Scheme = "https"
	case "https":
		parsed.Scheme = "https"
	default:
		return "", errors.New("media URL must be an http or https URL")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func retryDownloadFromStart(
	ctx context.Context,
	client *http.Client,
	headers http.Header,
	tempDir, downloadDir string,
	task downloadTask,
	idleTimeout time.Duration,
	allowRetry bool,
	response *http.Response,
	partial, metadataPath string,
	reason error,
) error {
	_ = response.Body.Close()
	if err := resetPartialState(partial, metadataPath); err != nil {
		return errors.Join(reason, err)
	}
	if !allowRetry {
		return reason
	}
	return downloadFile(ctx, client, headers, tempDir, downloadDir, task, idleTimeout, false)
}

func loadPartialState(partial, metadataPath, sourceURL string) (int64, partialMetadata, error) {
	info, err := os.Stat(partial)
	if errors.Is(err, os.ErrNotExist) {
		if err := resetPartialState(partial, metadataPath); err != nil {
			return 0, partialMetadata{}, err
		}
		return 0, partialMetadata{}, nil
	}
	if err != nil {
		return 0, partialMetadata{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		if err := resetPartialState(partial, metadataPath); err != nil {
			return 0, partialMetadata{}, err
		}
		return 0, partialMetadata{}, nil
	}

	content, err := os.ReadFile(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := resetPartialState(partial, metadataPath); err != nil {
			return 0, partialMetadata{}, err
		}
		return 0, partialMetadata{}, nil
	}
	if err != nil {
		return 0, partialMetadata{}, err
	}
	var metadata partialMetadata
	if err := json.Unmarshal(content, &metadata); err != nil ||
		metadata.URL != sourceURL ||
		metadata.validator() == "" {
		if resetErr := resetPartialState(partial, metadataPath); resetErr != nil {
			return 0, partialMetadata{}, resetErr
		}
		return 0, partialMetadata{}, nil
	}
	return info.Size(), metadata, nil
}

func partialMetadataFromResponse(sourceURL string, headers http.Header) partialMetadata {
	etag := strings.TrimSpace(headers.Get("ETag"))
	if strings.HasPrefix(strings.ToLower(etag), "w/") {
		etag = ""
	}
	return partialMetadata{
		URL:          sourceURL,
		ETag:         etag,
		LastModified: strings.TrimSpace(headers.Get("Last-Modified")),
	}
}

func persistPartialMetadata(path string, metadata partialMetadata) error {
	if metadata.validator() == "" {
		return removeIfExists(path)
	}
	content, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(content, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func resetPartialState(partial, metadataPath string) error {
	return errors.Join(
		removeIfExists(partial),
		removeIfExists(metadataPath),
		removeIfExists(metadataPath+".tmp"),
	)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func parseContentRange(value string, expectedStart, contentLength int64) (int64, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 || strings.ToLower(fields[0]) != "bytes" {
		return 0, false
	}
	span, totalValue, ok := strings.Cut(fields[1], "/")
	if !ok || totalValue == "*" {
		return 0, false
	}
	startValue, endValue, ok := strings.Cut(span, "-")
	if !ok {
		return 0, false
	}
	start, startErr := strconv.ParseInt(startValue, 10, 64)
	end, endErr := strconv.ParseInt(endValue, 10, 64)
	total, totalErr := strconv.ParseInt(totalValue, 10, 64)
	if startErr != nil || endErr != nil || totalErr != nil ||
		start != expectedStart || end < start || total <= end || end != total-1 {
		return 0, false
	}
	expectedLength := end - start + 1
	if contentLength > 0 && contentLength != expectedLength {
		return 0, false
	}
	return total, true
}

func partialCanResume(path string, expectedTotal int64, metadata partialMetadata) (bool, error) {
	if metadata.validator() == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return false, nil
	}
	if expectedTotal > 0 && info.Size() >= expectedTotal {
		return false, nil
	}
	return true, nil
}

func checkTransferredSize(path string, expectedTotal int64, metadata partialMetadata) (bool, error) {
	if expectedTotal <= 0 {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Size() == expectedTotal {
		return false, nil
	}
	sizeErr := fmt.Errorf(
		"transferred size %d does not match expected total %d",
		info.Size(),
		expectedTotal,
	)
	if info.Size() < expectedTotal {
		keepPartial, stateErr := partialCanResume(path, expectedTotal, metadata)
		return keepPartial, errors.Join(sizeErr, stateErr)
	}
	return false, sizeErr
}

func resumeValidatorMatches(metadata partialMetadata, headers http.Header) bool {
	responseMetadata := partialMetadataFromResponse(metadata.URL, headers)
	if metadata.ETag != "" {
		return responseMetadata.ETag != "" && metadata.ETag == responseMetadata.ETag
	}
	if metadata.LastModified != "" {
		return responseMetadata.LastModified != "" &&
			metadata.LastModified == responseMetadata.LastModified
	}
	return false
}

type idleTimeoutReadCloser struct {
	io.ReadCloser
	timeout time.Duration
}

func (r *idleTimeoutReadCloser) Read(buffer []byte) (int, error) {
	if r.timeout <= 0 {
		return r.ReadCloser.Read(buffer)
	}
	var timedOut atomic.Bool
	finished := make(chan struct{})
	timer := time.AfterFunc(r.timeout, func() {
		timedOut.Store(true)
		_ = r.ReadCloser.Close()
		close(finished)
	})
	length, err := r.ReadCloser.Read(buffer)
	if !timer.Stop() {
		<-finished
	}
	if timedOut.Load() {
		return length, errDownloadIdleTimeout
	}
	return length, err
}

func completedDownloadExists(downloadDir string, task downloadTask) (bool, error) {
	extensions := []string{task.extension}
	if task.extension == "jpeg" {
		extensions = append(extensions, "jpg", "png", "webp", "gif", "bmp", "avif", "heic")
	}
	for _, extension := range extensions {
		path := filepath.Join(downloadDir, task.baseName+"."+extension)
		exists, err := regularFileExists(path)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		if _, err := validatedMediaExtension(path, task.extension); err == nil {
			return true, nil
		} else if !errors.Is(err, errInvalidMedia) {
			return false, err
		}
		if err := os.Remove(path); err != nil {
			return false, err
		}
	}
	return false, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s exists but is not a regular file", path)
	}
	return true, nil
}

func validatedMediaExtension(path, expected string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", fmt.Errorf("%w: media file is empty or not regular", errInvalidMedia)
	}

	header := make([]byte, 32)
	length, readErr := io.ReadFull(file, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", readErr
	}
	header = header[:length]
	extension := detectedMediaExtension(header)
	switch expected {
	case "jpeg":
		if !isImageExtension(extension) {
			return "", fmt.Errorf("%w: expected image, detected %q", errInvalidMedia, extension)
		}
	case "mp4":
		if extension != "mp4" {
			return "", fmt.Errorf("%w: expected mp4, detected %q", errInvalidMedia, extension)
		}
	default:
		return "", fmt.Errorf("%w: unsupported expected media type %q", errInvalidMedia, expected)
	}
	if err := validateMediaStructure(file, info.Size(), extension); err != nil {
		return "", err
	}
	return extension, nil
}

func detectedMediaExtension(header []byte) string {
	switch {
	case len(header) >= 3 && string(header[:3]) == "\xff\xd8\xff":
		return "jpeg"
	case len(header) >= 8 && string(header[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP":
		return "webp"
	case len(header) >= 6 && (string(header[:6]) == "GIF87a" || string(header[:6]) == "GIF89a"):
		return "gif"
	case len(header) >= 2 && string(header[:2]) == "BM":
		return "bmp"
	case len(header) >= 12 && string(header[4:8]) == "ftyp":
		brandOffset := 8
		if binary.BigEndian.Uint32(header[:4]) == 1 {
			if len(header) < 20 {
				return ""
			}
			brandOffset = 16
		}
		return extensionForISOBrand(string(header[brandOffset : brandOffset+4]))
	default:
		return ""
	}
}

func extensionForISOBrand(brand string) string {
	switch brand {
	case "avif", "avis":
		return "avif"
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
		return "heic"
	case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "iso7", "iso8", "iso9",
		"mp41", "mp42", "avc1", "dash", "MSNV", "M4V ", "M4A ", "M4B ",
		"F4V ", "F4A ", "3gp4", "3gp5", "3g2a", "qt  ":
		return "mp4"
	default:
		return ""
	}
}

func validateMediaStructure(file *os.File, size int64, extension string) error {
	var valid bool
	switch extension {
	case "jpeg":
		valid = validateJPEG(file, size)
	case "png":
		valid = validatePNG(file, size)
	case "webp":
		valid = validateWebP(file, size)
	case "gif":
		valid = validateGIF(file, size)
	case "bmp":
		valid = validateBMP(file, size)
	case "mp4", "avif", "heic":
		valid = validateISOBaseMedia(file, size)
	}
	if !valid {
		return fmt.Errorf("%w: incomplete or malformed %s structure", errInvalidMedia, extension)
	}
	return nil
}

func validateJPEG(file *os.File, size int64) bool {
	if size < 4 {
		return false
	}
	tail := make([]byte, 2)
	return readAtExact(file, size-2, tail) && tail[0] == 0xff && tail[1] == 0xd9
}

func validatePNG(file *os.File, size int64) bool {
	if size < 20 {
		return false
	}
	const iend = "\x00\x00\x00\x00IEND\xae\x42\x60\x82"
	tail := make([]byte, len(iend))
	return readAtExact(file, size-int64(len(tail)), tail) && string(tail) == iend
}

func validateWebP(file *os.File, size int64) bool {
	if size < 12 || size-8 > int64(^uint32(0)) {
		return false
	}
	header := make([]byte, 12)
	return readAtExact(file, 0, header) &&
		string(header[:4]) == "RIFF" &&
		string(header[8:12]) == "WEBP" &&
		int64(binary.LittleEndian.Uint32(header[4:8])) == size-8
}

func validateGIF(file *os.File, size int64) bool {
	if size < 7 {
		return false
	}
	trailer := make([]byte, 1)
	return readAtExact(file, size-1, trailer) && trailer[0] == 0x3b
}

func validateBMP(file *os.File, size int64) bool {
	if size < 14 || size > int64(^uint32(0)) {
		return false
	}
	header := make([]byte, 14)
	return readAtExact(file, 0, header) &&
		string(header[:2]) == "BM" &&
		int64(binary.LittleEndian.Uint32(header[2:6])) == size
}

func validateISOBaseMedia(file *os.File, size int64) bool {
	if size < 16 {
		return false
	}
	offset := int64(0)
	boxes := 0
	hasMediaBox := false
	for offset < size {
		remaining := size - offset
		if remaining < 8 {
			return false
		}
		header := make([]byte, 8)
		if !readAtExact(file, offset, header) {
			return false
		}
		size32 := binary.BigEndian.Uint32(header[:4])
		boxType := string(header[4:8])
		headerSize := int64(8)
		var boxSize int64
		switch size32 {
		case 0:
			boxSize = remaining
		case 1:
			if remaining < 16 {
				return false
			}
			extended := make([]byte, 8)
			if !readAtExact(file, offset+8, extended) {
				return false
			}
			size64 := binary.BigEndian.Uint64(extended)
			if size64 > uint64(^uint64(0)>>1) {
				return false
			}
			boxSize = int64(size64)
			headerSize = 16
			if boxSize < headerSize {
				return false
			}
		default:
			boxSize = int64(size32)
			if boxSize < headerSize {
				return false
			}
		}
		if boxSize > remaining {
			return false
		}
		if boxes == 0 {
			if boxType != "ftyp" || boxSize < headerSize+8 {
				return false
			}
		} else if isISOMediaBox(boxType) {
			hasMediaBox = true
		}
		offset += boxSize
		boxes++
	}
	return boxes > 1 && hasMediaBox && offset == size
}

func isISOMediaBox(boxType string) bool {
	switch boxType {
	case "mdat", "moov", "moof", "meta", "idat":
		return true
	default:
		return false
	}
}

func readAtExact(file *os.File, offset int64, buffer []byte) bool {
	length, err := file.ReadAt(buffer, offset)
	return length == len(buffer) && err == nil
}

func isImageExtension(extension string) bool {
	switch extension {
	case "jpeg", "png", "webp", "gif", "bmp", "avif", "heic":
		return true
	default:
		return false
	}
}

func safeFilename(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, character := range strings.TrimSpace(value) {
		allowed := unicode.IsLetter(character) || unicode.IsNumber(character) || strings.ContainsRune("-_！？，。；：“”（）《》", character)
		if !allowed {
			if !lastUnderscore && builder.Len() > 0 {
				builder.WriteRune('_')
				lastUnderscore = true
			}
			continue
		}
		builder.WriteRune(character)
		lastUnderscore = character == '_'
	}
	result := strings.Trim(builder.String(), "_")
	runes := []rune(result)
	if len(runes) > 120 {
		result = string(runes[:120])
	}
	return result
}
