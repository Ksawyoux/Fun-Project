package ingestor

import (
	"context"
	"testing"
)

func TestTypeScriptAST_ParsesTypeScriptFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/index.ts", `
import { helper } from "./utils/helper";
import 'direct-package';

export function runMain() {
    helper();
}
`)
	writeFile(t, root, "src/utils/helper.ts", `
export const helper = () => {
    console.log("helper");
};
`)

	ts := NewTypeScriptAST(TypeScriptASTConfig{
		SourceID:  "ts-test",
		RootPath:  root,
		Namespace: "test",
	})

	batch, _, err := ts.Fetch(context.Background(), "run-1", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	indexMod := entityByName(batch, "src.index")
	helperMod := entityByName(batch, "src.utils.helper")

	if indexMod == nil || helperMod == nil {
		t.Fatalf("missing TS module entities: index=%v helper=%v", indexMod, helperMod)
	}

	// Verify function
	runFunc := entityByName(batch, "src.index.runMain")
	if runFunc == nil {
		t.Fatalf("missing TS function entity runMain")
	}

	// Verify arrow function
	helperFunc := entityByName(batch, "src.utils.helper.helper")
	if helperFunc == nil {
		t.Fatalf("missing TS arrow function entity helper")
	}

	// Verify imports relationship resolved relatively
	if !hasRelationship(batch, "IMPORTS", indexMod.ID, helperMod.ID) {
		t.Errorf("missing IMPORTS index -> helper")
	}
}
