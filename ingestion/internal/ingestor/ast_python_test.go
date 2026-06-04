package ingestor

import (
	"context"
	"testing"
)

func TestPythonAST_ParsesPythonFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app/main.py", `
import app.lib
from app.helper import helper_func

class AppController:
    def init(self):
        pass

def run_app():
    pass
`)
	writeFile(t, root, "app/lib.py", `
def lib_func():
    pass
`)
	writeFile(t, root, "app/helper.py", `
def helper_func():
    pass
`)

	py := NewPythonAST(PythonASTConfig{
		SourceID:  "py-test",
		RootPath:  root,
		Namespace: "test",
	})

	batch, _, err := py.Fetch(context.Background(), "run-1", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	mainMod := entityByName(batch, "app.main")
	libMod := entityByName(batch, "app.lib")
	helperMod := entityByName(batch, "app.helper")

	if mainMod == nil || libMod == nil || helperMod == nil {
		t.Fatalf("missing python module entities: main=%v lib=%v helper=%v", mainMod, libMod, helperMod)
	}

	// Verify functions
	runFunc := entityByName(batch, "app.main.run_app")
	if runFunc == nil {
		t.Fatalf("missing python function entity run_app")
	}

	// Verify classes
	appClass := entityByName(batch, "app.main.AppController")
	if appClass == nil {
		t.Fatalf("missing python class entity AppController")
	}

	// Verify imports relationships
	if !hasRelationship(batch, "IMPORTS", mainMod.ID, libMod.ID) {
		t.Errorf("missing IMPORTS main -> lib")
	}
	if !hasRelationship(batch, "IMPORTS", mainMod.ID, helperMod.ID) {
		t.Errorf("missing IMPORTS main -> helper")
	}
}
