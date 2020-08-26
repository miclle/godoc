package gatefs_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/miclle/godoc/vfs"
	"github.com/miclle/godoc/vfs/gatefs"
)

func TestRootType(t *testing.T) {
	goPath := os.Getenv("GOPATH")
	var expectedType vfs.RootType
	if goPath == "" {
		expectedType = ""
	} else {
		expectedType = vfs.RootTypeGoPath
	}
	tests := []struct {
		path   string
		fsType vfs.RootType
	}{
		{runtime.GOROOT(), vfs.RootTypeGoRoot},
		{goPath, expectedType},
		{"/tmp/", ""},
	}

	for _, item := range tests {
		fs := gatefs.New(vfs.OS(item.path), make(chan bool, 1))
		if fs.RootType("path") != item.fsType {
			t.Errorf("unexpected fsType. Expected- %v, Got- %v", item.fsType, fs.RootType("path"))
		}
	}
}
