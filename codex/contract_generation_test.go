package codex

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedFilesAreUpToDate(t *testing.T) {
	t.Parallel()

	targets := []string{
		filepath.Join("protocol", "generated.go"),
		filepath.Join("protocol", "registry_generated.go"),
	}

	before := snapshotFiles(t, targets)

	cmd := exec.Command("go", "run", "../cmd/updatesdkartifacts", "generate-types")
	cmd.Dir = "."
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "schema fetch failed") {
			t.Skipf("generate-types requires upstream schema access: %s", bytes.TrimSpace(output))
		}
		t.Fatalf("generate-types error = %v\n%s", err, output)
	}

	after := snapshotFiles(t, targets)
	if !bytes.Equal(before[targets[0]], after[targets[0]]) || !bytes.Equal(before[targets[1]], after[targets[1]]) {
		t.Fatalf("generated files drifted after regeneration")
	}
}

func snapshotFiles(t *testing.T, targets []string) map[string][]byte {
	t.Helper()

	snapshot := make(map[string][]byte, len(targets))
	for _, target := range targets {
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", target, err)
		}
		snapshot[target] = data
	}
	return snapshot
}
