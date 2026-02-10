package commands

import (
	"testing"
)

func TestValidateConfigLocally_ValidMaster(t *testing.T) {
	content := []byte(`
server:
  port: 8080
scanner:
  root_path: /videos
database:
  path: ./jobs.db
logging:
  level: info
`)
	errors := validateConfigLocally("master", content)
	if len(errors) != 0 {
		t.Errorf("validateConfigLocally() returned %d errors for valid master config: %v", len(errors), errors)
	}
}

func TestValidateConfigLocally_MissingMasterSections(t *testing.T) {
	content := []byte(`
server:
  port: 8080
`)
	errors := validateConfigLocally("master", content)
	if len(errors) == 0 {
		t.Error("validateConfigLocally() expected errors for missing scanner/database sections, got none")
	}

	// Should report missing scanner and database sections
	foundScanner := false
	foundDatabase := false
	for _, e := range errors {
		if e == "Missing required section: scanner:" {
			foundScanner = true
		}
		if e == "Missing required section: database:" {
			foundDatabase = true
		}
	}
	if !foundScanner {
		t.Error("Expected error about missing scanner section")
	}
	if !foundDatabase {
		t.Error("Expected error about missing database section")
	}
}

func TestValidateConfigLocally_MissingMasterPort(t *testing.T) {
	content := []byte(`
server:
  host: 0.0.0.0
scanner:
  root_path: /videos
database:
  path: ./jobs.db
`)
	errors := validateConfigLocally("master", content)
	foundPort := false
	for _, e := range errors {
		if e == "Missing server port configuration" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Error("Expected error about missing port configuration")
	}
}

func TestValidateConfigLocally_ValidWorker(t *testing.T) {
	content := []byte(`
worker:
  id: worker-1
  master_url: http://localhost:8080
storage:
  mount_path: /mnt/storage
ffmpeg:
  path: /usr/bin/ffmpeg
logging:
  level: info
`)
	errors := validateConfigLocally("worker", content)
	if len(errors) != 0 {
		t.Errorf("validateConfigLocally() returned %d errors for valid worker config: %v", len(errors), errors)
	}
}

func TestValidateConfigLocally_MissingWorkerSections(t *testing.T) {
	content := []byte(`
worker:
  id: worker-1
  master_url: http://localhost:8080
`)
	errors := validateConfigLocally("worker", content)
	if len(errors) == 0 {
		t.Error("validateConfigLocally() expected errors for missing storage/ffmpeg sections, got none")
	}
}

func TestValidateConfigLocally_EmptyContent(t *testing.T) {
	errors := validateConfigLocally("master", []byte(""))
	if len(errors) != 1 || errors[0] != "Empty configuration file" {
		t.Errorf("validateConfigLocally() for empty content = %v, want [\"Empty configuration file\"]", errors)
	}
}

func TestValidateConfigLocally_MissingWorkerID(t *testing.T) {
	content := []byte(`
worker:
  master_url: http://localhost:8080
storage:
  mount_path: /mnt/storage
ffmpeg:
  path: /usr/bin/ffmpeg
`)
	errors := validateConfigLocally("worker", content)
	foundID := false
	for _, e := range errors {
		if e == "Missing worker id configuration" {
			foundID = true
		}
	}
	if !foundID {
		t.Error("Expected error about missing worker id configuration")
	}
}

func TestValidateConfigLocally_MissingWorkerMasterURL(t *testing.T) {
	content := []byte(`
worker:
  id: worker-1
storage:
  mount_path: /mnt/storage
ffmpeg:
  path: /usr/bin/ffmpeg
`)
	errors := validateConfigLocally("worker", content)
	foundURL := false
	for _, e := range errors {
		if e == "Missing worker master_url configuration" {
			foundURL = true
		}
	}
	if !foundURL {
		t.Error("Expected error about missing worker master_url configuration")
	}
}
