package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// altengine static hosting limits.
const (
	MaxFiles           = 5000
	MaxTotalBytes      = 1 << 30
	DefaultMaxFileSize = 25 << 20  // per file, unless the site raises it
	HardMaxFileSize    = 100 << 20 // the most a site can raise it to
	MaxPathBytes       = 1024
	MaxMessageLen      = 200
)

// Manifest is the built site: URL path → {hash, size}, plus where each hash
// can be read from locally.
type Manifest struct {
	Files      map[string]FileEntry
	Local      map[string]string // content hash → local file
	TotalBytes int64
	Largest    string // URL path of the largest file
}

// BuildManifest walks dir and hashes every file. It enforces the limits that
// never change; the per-file limit a site can raise is checked separately
// (CheckFileSize).
func BuildManifest(dir string) (*Manifest, error) {
	m := &Manifest{Files: map[string]FileEntry{}, Local: map[string]string{}}
	var tooBig []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := os.Stat(p) // follows symlinks
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		urlPath := "/" + filepath.ToSlash(rel)
		if len(urlPath) > MaxPathBytes {
			return fmt.Errorf("%s: path is longer than %d bytes", urlPath, MaxPathBytes)
		}
		if strings.Contains(urlPath, `\`) {
			return fmt.Errorf(`%s: altengine refuses paths containing "\"`, urlPath)
		}
		if info.Size() > HardMaxFileSize {
			tooBig = append(tooBig, fmt.Sprintf("%s (%s)", urlPath, formatBytes(info.Size())))
			return nil
		}
		hash, err := hashFile(p)
		if err != nil {
			return err
		}
		m.Files[urlPath] = FileEntry{Hash: hash, Size: info.Size()}
		m.Local[hash] = p
		m.TotalBytes += info.Size()
		if m.Largest == "" || info.Size() > m.Files[m.Largest].Size {
			m.Largest = urlPath
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(tooBig) > 0 {
		sort.Strings(tooBig)
		return nil, fmt.Errorf("altengine accepts files up to %s; too large: %s", formatBytes(HardMaxFileSize), strings.Join(tooBig, ", "))
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("%s is empty; nothing to publish", dir)
	}
	if len(m.Files) > MaxFiles {
		return nil, fmt.Errorf("the site has %d files; altengine accepts up to %d per deployment", len(m.Files), MaxFiles)
	}
	if m.TotalBytes > MaxTotalBytes {
		return nil, fmt.Errorf("the site is %s; altengine accepts up to %s per deployment", formatBytes(m.TotalBytes), formatBytes(MaxTotalBytes))
	}
	return m, nil
}

// CheckFileSize reports every file over limit.
func (m *Manifest) CheckFileSize(limit int64) error {
	var tooBig []string
	for p, f := range m.Files {
		if f.Size > limit {
			tooBig = append(tooBig, fmt.Sprintf("%s (%s)", p, formatBytes(f.Size)))
		}
	}
	if len(tooBig) == 0 {
		return nil
	}
	sort.Strings(tooBig)
	msg := fmt.Sprintf("this site accepts files up to %s; too large: %s", formatBytes(limit), strings.Join(tooBig, ", "))
	if limit < HardMaxFileSize {
		msg += fmt.Sprintf(". The site's max file size can be raised to %s in altengine", formatBytes(HardMaxFileSize))
	}
	return errors.New(msg)
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GB", n>>30)
	case n >= 1<<20:
		if n%(1<<20) == 0 {
			return fmt.Sprintf("%d MB", n>>20)
		}
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

const (
	uploadWorkers = 8
	uploadRetries = 2 // after the first attempt
)

// uploadRetryDelay is the wait before retry n (1-based); tests shorten it.
var uploadRetryDelay = func(n int) time.Duration { return time.Duration(n) * time.Second }

// uploader PUTs missing files, skipping hashes it has already sent.
type uploader struct {
	HTTP  *http.Client
	Local map[string]string

	mu   sync.Mutex
	sent map[string]bool
}

// upload sends one page of uploads, uploadWorkers at a time, and returns how
// many files it sent.
func (u *uploader) upload(ctx context.Context, uploads []Upload) (int, error) {
	u.mu.Lock()
	if u.sent == nil {
		u.sent = map[string]bool{}
	}
	var todo []Upload
	for _, up := range uploads {
		if !u.sent[up.PathHash] {
			u.sent[up.PathHash] = true
			todo = append(todo, up)
		}
	}
	u.mu.Unlock()
	if len(todo) == 0 {
		return 0, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan Upload)
	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
		count    int
		countMu  sync.Mutex
	)
	for i := 0; i < min(uploadWorkers, len(todo)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for up := range jobs {
				if err := u.uploadWithRetry(ctx, up); err != nil {
					errOnce.Do(func() { firstErr = err; cancel() })
					continue
				}
				countMu.Lock()
				count++
				countMu.Unlock()
			}
		}()
	}
feed:
	for _, up := range todo {
		select {
		case jobs <- up:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	return count, firstErr
}

func (u *uploader) uploadWithRetry(ctx context.Context, up Upload) error {
	var err error
	for attempt := 0; attempt <= uploadRetries; attempt++ {
		if attempt > 0 {
			if serr := sleepCtx(ctx, uploadRetryDelay(attempt)); serr != nil {
				return serr
			}
		}
		if err = u.put(ctx, up); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
	}
	return err
}

func (u *uploader) put(ctx context.Context, up Upload) error {
	local, ok := u.Local[up.PathHash]
	if !ok {
		return fmt.Errorf("altengine asked for a file this site doesn't have (%s)", up.PathHash)
	}
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return err
	} else if info.Size() != up.Size {
		return fmt.Errorf("%s changed while publishing; run sitegen -publish again", local)
	}
	method := up.Method
	if method == "" {
		method = http.MethodPut
	}
	req, err := http.NewRequestWithContext(ctx, method, up.UploadURL, f)
	if err != nil {
		return err
	}
	for k, v := range up.RequiredHeaders {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		req.Header.Set(k, v)
	}
	// The size is signed into the URL; without this Go would send the body
	// chunked and the storage service would refuse the signature.
	req.ContentLength = up.Size
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w", filepath.Base(local), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("upload %s: %s: %s", filepath.Base(local), resp.Status, strings.TrimSpace(string(body)))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}
