package coordinator

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

func TestStartReturnsOnServerError(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	cfg := &models.MasterConfig{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Database.Path = filepath.Join(dir, "master.db")
	cfg.Scanner.RootPath = dir
	cfg.Scanner.OutputBase = filepath.Join(dir, "output")

	coord, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create coordinator: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- coord.Start()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Start to fail when the port is already in use")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after server failure")
	}
}
