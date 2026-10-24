package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxAttachment = 10 << 20

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SaveAttachment stores a base64 image under <home>/workspaces/<key>/attachments
// (or <home>/attachments when key is empty) and returns its absolute path.
func SaveAttachment(home, key, name, dataB64 string) (string, error) {
	base := unsafeName.ReplaceAllString(filepath.Base(name), "_")
	ext := strings.ToLower(filepath.Ext(base))
	if !imageExt[ext] {
		return "", fmt.Errorf("unsupported image type %q (png, jpg, gif, webp)", ext)
	}
	if len(dataB64) > maxAttachment*4/3+4 {
		return "", errors.New("image too large (max 10 MB)")
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	if len(data) > maxAttachment {
		return "", errors.New("image too large (max 10 MB)")
	}
	dir := filepath.Join(home, "attachments")
	if key != "" {
		dir = filepath.Join(WorkspacesDir(home), unsafeName.ReplaceAllString(filepath.Base(key), "_"), "attachments")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base))
	return p, os.WriteFile(p, data, 0o644)
}

// AttachmentHandler serves GET /attachments/<abs path> for image files under
// the conveyor home, so the webview can show them.
func AttachmentHandler(home string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/attachments/")
		if !ok || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		p := filepath.Clean("/" + rest)
		if !strings.HasPrefix(p, home+string(filepath.Separator)) || !imageExt[strings.ToLower(filepath.Ext(p))] {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, p)
	})
}
