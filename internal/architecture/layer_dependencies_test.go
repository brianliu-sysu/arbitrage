package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const internalImportPrefix = "github.com/brianliu-sysu/uniswapv3/internal/"

var dependencyRules = map[string][]string{
	"domain": {
		"application",
		"infrastructure",
		"interfaces",
	},
	"application": {
		"infrastructure",
		"interfaces",
	},
	"infrastructure": {
		"application",
		"interfaces",
	},
	"interfaces": {
		"infrastructure",
	},
}

func TestLayerDependencies(t *testing.T) {
	repoRoot := findRepoRoot(t)
	for layer, forbiddenLayers := range dependencyRules {
		layerDir := filepath.Join(repoRoot, "internal", layer)
		if _, err := os.Stat(layerDir); err != nil {
			t.Fatalf("stat layer dir %s: %v", layerDir, err)
		}
		assertLayerDoesNotImport(t, layerDir, forbiddenLayers)
	}
}

func assertLayerDoesNotImport(t *testing.T, layerDir string, forbiddenLayers []string) {
	t.Helper()
	fileSet := token.NewFileSet()
	err := filepath.WalkDir(layerDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range file.Imports {
			importPath := strings.Trim(importSpec.Path.Value, `"`)
			for _, forbiddenLayer := range forbiddenLayers {
				forbiddenPrefix := internalImportPrefix + forbiddenLayer
				if importPath == forbiddenPrefix || strings.HasPrefix(importPath, forbiddenPrefix+"/") {
					t.Errorf("%s imports forbidden layer %q via %s", path, forbiddenLayer, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", layerDir, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
