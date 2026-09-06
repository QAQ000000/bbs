package api

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"testing"
)

func TestUploadFailureRemovesFile(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	original := smokeSrv.cfg.UploadDir
	smokeSrv.cfg.UploadDir = t.TempDir()
	defer func() { smokeSrv.cfg.UploadDir = original }()
	if _, err := smokePool.Exec(ctx, `ALTER TABLE uploads ADD CONSTRAINT test_upload_failure CHECK (name <> 'reject.pdf')`); err != nil {
		t.Fatal(err)
	}
	defer smokePool.Exec(ctx, `ALTER TABLE uploads DROP CONSTRAINT test_upload_failure`)
	w := smokeMultipart(t, "/api/v1/uploads", userCSRF, "file", "reject.pdf", pdfMagic,
		map[string]string{"kind": "file"}, userCookie)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected database failure, got %d: %s", w.Code, w.Body.String())
	}
	if err := filepath.WalkDir(smokeSrv.cfg.UploadDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("orphan upload: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
