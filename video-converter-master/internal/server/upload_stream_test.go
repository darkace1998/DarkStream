package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

// countTempSpillFiles reports how many multipart spill files exist in the
// system temp directory. Go names them "multipart-*".
func countTempSpillFiles(t *testing.T) int {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	return len(matches)
}

// buildMultipart returns a multipart body containing a single file field, along
// with its boundary. The boundary comes from the writer rather than being parsed
// back out of the body, since reading from the buffer would consume the bytes the
// reader needs.
func buildMultipart(t *testing.T, field, filename string, payload []byte) (body *bytes.Buffer, boundary string) {
	t.Helper()

	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)

	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("writing payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	return body, w.Boundary()
}

// The upload path must stream. ParseMultipartForm spills anything beyond its
// in-memory limit to os.TempDir(), which is a RAM-backed tmpfs on a typical
// container host — so a large upload was held in memory at full size and leaked
// its spill file if the request was interrupted.
func TestFindFormFileStreamsWithoutSpillingToTemp(t *testing.T) {
	// Comfortably larger than the 32 MiB limit the old implementation used.
	payload := bytes.Repeat([]byte("darkstream"), 4*1024*1024) // ~40 MiB

	body, boundary := buildMultipart(t, "video", "out.mp4", payload)

	before := countTempSpillFiles(t)

	reader := multipart.NewReader(body, boundary)
	part, err := findFormFile(reader, "video")
	if err != nil {
		t.Fatalf("findFormFile() error = %v", err)
	}
	defer func() { _ = part.Close() }()

	written, err := io.Copy(io.Discard, part)
	if err != nil {
		t.Fatalf("streaming the part: %v", err)
	}
	if written != int64(len(payload)) {
		t.Errorf("streamed %d bytes, want %d", written, len(payload))
	}

	if after := countTempSpillFiles(t); after != before {
		t.Errorf("upload spilled %d temp file(s) to %s; it must stream instead",
			after-before, os.TempDir())
	}
}

func TestFindFormFileSkipsOtherFields(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	if err := w.WriteField("job_id", "abc123"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	other, err := w.CreateFormFile("thumbnail", "thumb.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := other.Write([]byte("not the video")); err != nil {
		t.Fatalf("writing thumbnail: %v", err)
	}
	video, err := w.CreateFormFile("video", "out.mp4")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := video.Write([]byte("the real payload")); err != nil {
		t.Fatalf("writing video: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	boundary := w.Boundary()
	part, err := findFormFile(multipart.NewReader(body, boundary), "video")
	if err != nil {
		t.Fatalf("findFormFile() error = %v", err)
	}
	defer func() { _ = part.Close() }()

	got, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("reading part: %v", err)
	}
	if string(got) != "the real payload" {
		t.Errorf("read %q, want the video part", got)
	}
}

func TestFindFormFileMissingField(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("job_id", "abc123"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	if _, err := findFormFile(multipart.NewReader(body, w.Boundary()), "video"); err == nil {
		t.Error("findFormFile() accepted an upload with no video field")
	}
}
