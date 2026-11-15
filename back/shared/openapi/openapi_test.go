package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestGeneratedRoutesAndJSONSchemas(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/api/v1/items/{id}", func(http.ResponseWriter, *http.Request) {})
	router.Post("/internal/items", func(http.ResponseWriter, *http.Request) {})
	response := httptest.NewRecorder()
	Handler(router, "test", nil)(response, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	if len(paths) != 1 {
		t.Fatalf("routes=%d", len(paths))
	}
	if _, ok := paths["/internal/items"]; ok {
		t.Fatal("private route published")
	}
	if _, ok := spec["components"].(map[string]any)["securitySchemes"].(map[string]any)["serviceHMAC"]; ok {
		t.Fatal("service authentication published")
	}
	type input struct {
		Name     string  `json:"name"`
		Password string  `json:"-"`
		Optional *string `json:"optional,omitempty"`
	}
	definitions := &schemas{components: map[string]any{}}
	definition := definitions.object(reflect.TypeFor[input]())
	properties := definition["properties"].(map[string]any)
	if len(properties) != 2 {
		t.Fatal("ignored fields exposed")
	}
}

func TestPublicDocumentOmitsPrivateSchemasAndKeepsFullValidation(t *testing.T) {
	type publicDTO struct {
		Title string `json:"title"`
	}
	type privateDTO struct {
		Secret string `json:"secret_service_field"`
	}
	r := chi.NewRouter()
	r.Get("/api/v1/items", func(http.ResponseWriter, *http.Request) {})
	r.Post("/internal/privileged", func(http.ResponseWriter, *http.Request) {})
	models := map[string]Models{"GET /api/v1/items": {Response: publicDTO{}}, "POST /internal/privileged": {Request: privateDTO{}}}
	doc, err := PublicDocument(r, "test", models)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	for _, forbidden := range []string{"/internal/", "secret_service_field", "privateDTO", "serviceHMAC", "X-Service-Token"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("public specification contains %s", forbidden)
		}
	}
	delete(models, "POST /internal/privileged")
	if err := ValidateRoutes(r, "test", models); err == nil {
		t.Fatal("full contract validation ignored private routes")
	}
}

func TestSchemasAndOperationContracts(t *testing.T) {
	router := chi.NewRouter()
	router.Post("/files/{id}", func(http.ResponseWriter, *http.Request) {})
	router.Get("/items", func(http.ResponseWriter, *http.Request) {})
	router.Get("/refresh", func(http.ResponseWriter, *http.Request) {})
	models := map[string]Models{
		"POST /files/{id}": Upload("file"),
		"GET /items":       {Response: map[string]any{"data": Array(map[string]any{"title": "", "at": time.Time{}, "optional": (*string)(nil)})}, OptionalAuth: true, Parameters: PageParameters()},
		"GET /refresh":     {CookieAuth: true, Status: 302},
	}
	models["POST /files/{id}"] = func() Models {
		model := models["POST /files/{id}"]
		model.Response = map[string]any{"id": ""}
		return model
	}()
	if err := ValidateRoutes(router, "test", models); err != nil {
		t.Fatal(err)
	}
	document, err := Document(router, "test", models)
	if err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]map[string]any)
	file := paths["/files/{id}"]["post"].(map[string]any)
	content := file["requestBody"].(map[string]any)["content"].(map[string]any)
	schema := content["multipart/form-data"].(map[string]any)["schema"].(map[string]any)
	if schema["properties"].(map[string]any)["file"].(map[string]any)["format"] != "binary" {
		t.Fatal("upload is not binary multipart")
	}
	if _, ok := file["responses"].(map[string]any)["201"]; !ok {
		t.Fatal("created response missing")
	}
	refresh := paths["/refresh"]["get"].(map[string]any)
	if _, ok := refresh["security"].([]any)[0].(map[string]any)["refreshCookie"]; !ok {
		t.Fatal("refresh cookie missing")
	}
	if _, ok := refresh["responses"].(map[string]any)["302"].(map[string]any)["headers"]; !ok {
		t.Fatal("redirect Location missing")
	}
	delete(models, "GET /refresh")
	if err := ValidateRoutes(router, "test", models); err == nil {
		t.Fatal("undocumented route accepted")
	}
}

func TestRecursiveTypesAndJSONFieldRules(t *testing.T) {
	type node struct {
		Title   string            `json:"title"`
		Hidden  string            `json:"-"`
		Next    *node             `json:"next,omitempty"`
		Payload []byte            `json:"payload"`
		Tags    map[string]string `json:"tags"`
		Count   int               `json:"count"`
		Ratio   float64           `json:"ratio"`
		Enabled bool              `json:"enabled"`
	}
	definitions := &schemas{components: map[string]any{}}
	ref := definitions.schema(reflect.TypeFor[node]())["$ref"].(string)
	if len(definitions.components) != 1 || ref == "" {
		t.Fatal("recursive DTO expanded indefinitely")
	}
	for _, value := range definitions.components {
		properties := value.(map[string]any)["properties"].(map[string]any)
		if _, ok := properties["hidden"]; ok {
			t.Fatal("ignored property published")
		}
		if properties["payload"].(map[string]any)["format"] != "byte" {
			t.Fatal("JSON bytes are not base64")
		}
	}
}
