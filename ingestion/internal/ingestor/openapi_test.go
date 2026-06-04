package ingestor

import (
	"context"
	"testing"
)

func TestOpenAPI_ParsesJSONAndYAML(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openapi.json", `
{
  "openapi": "3.0.0",
  "paths": {
    "/v1/users": {
      "get": {
        "summary": "Get users"
      },
      "post": {}
    }
  }
}
`)
	writeFile(t, root, "api.openapi.yaml", `
openapi: 3.0.0
paths:
  /v1/checkout:
    post:
      summary: Place order
    get:
      summary: Retrieve order
`)

	o := NewOpenAPI(OpenAPIConfig{
		SourceID:  "api-test",
		RootPath:  root,
		Namespace: "test",
	})

	batch, _, err := o.Fetch(context.Background(), "run-1", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Verify endpoints from JSON
	getUsers := entityByName(batch, "GET /v1/users")
	postUsers := entityByName(batch, "POST /v1/users")
	if getUsers == nil || postUsers == nil {
		t.Errorf("missing /v1/users API endpoint entities")
	}

	// Verify endpoints from YAML
	postCheckout := entityByName(batch, "POST /v1/checkout")
	getCheckout := entityByName(batch, "GET /v1/checkout")
	if postCheckout == nil || getCheckout == nil {
		t.Errorf("missing /v1/checkout API endpoint entities")
	}
}
