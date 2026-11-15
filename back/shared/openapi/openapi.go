package openapi

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/confhub/shared/httputil"
	"github.com/go-chi/chi/v5"
)

type Models struct {
	Request, Response  any
	Status             int
	OtherResponses     map[int]Models
	ContentType        string
	RequestContentType string
	BodyOptional       bool
	Auth               bool
	OptionalAuth       bool
	CookieAuth         bool
	Parameters         []Parameter
	Description        string
}
type Parameter struct {
	Name             string
	In               string
	Required         bool
	Type             string
	Format           string
	Description      string
	Default          any
	Enum             []string
	Minimum, Maximum *int
}
type arrayExample struct{ item any }
type binarySchema struct{}

func Array(item any) any { return arrayExample{item: item} }
func Binary() any        { return binarySchema{} }
func Query(name, kind, description string) Parameter {
	return Parameter{Name: name, In: "query", Type: kind, Description: description}
}
func Upload(field string) Models {
	return Models{Request: map[string]any{field: Binary()}, RequestContentType: "multipart/form-data", Auth: true, Status: 201}
}
func PageParameters() []Parameter {
	one, hundred := 1, 100
	return []Parameter{{Name: "page", In: "query", Type: "integer", Default: 1, Minimum: &one}, {Name: "per_page", In: "query", Type: "integer", Default: 20, Minimum: &one, Maximum: &hundred}}
}

type schemas struct{ components map[string]any }

var parameter = regexp.MustCompile(`\{([^}:]+)(?::[^}]+)?\}`)
var operationCharacters = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func (s *schemas) value(value any) map[string]any {
	switch value := value.(type) {
	case arrayExample:
		return map[string]any{"type": "array", "items": s.value(value.item)}
	case binarySchema:
		return map[string]any{"type": "string", "format": "binary"}
	case map[string]any:
		properties := map[string]any{}
		required := make([]string, 0, len(value))
		for name, item := range value {
			properties[name] = s.value(item)
			required = append(required, name)
		}
		sort.Strings(required)
		return map[string]any{"type": "object", "properties": properties, "required": required}
	case []map[string]any:
		if len(value) > 0 {
			return map[string]any{"type": "array", "items": s.value(value[0])}
		}
		return map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
	}
	if value == nil {
		return map[string]any{"nullable": true}
	}
	return s.schema(reflect.TypeOf(value))
}

func (s *schemas) schema(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Pointer {
		base := s.schema(t.Elem())
		return map[string]any{"allOf": []any{base}, "nullable": true}
	}
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.PkgPath() == "github.com/google/uuid" && t.Name() == "UUID" {
		return map[string]any{"type": "string", "format": "uuid"}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": s.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": s.schema(t.Elem())}
	case reflect.Struct:
		name := t.Name()
		if name != "" {
			pkg := t.PkgPath()
			name = pkg[strings.LastIndex(pkg, "/")+1:] + "_" + name
			ref := map[string]any{"$ref": "#/components/schemas/" + name}
			if _, ok := s.components[name]; ok {
				return ref
			}
			s.components[name] = map[string]any{}
			s.components[name] = s.object(t)
			return ref
		}
		return s.object(t)
	default:
		return map[string]any{}
	}
}

func (s *schemas) object(t reflect.Type) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = field.Name
		}
		properties[name] = s.schema(field.Type)
		if properties[name].(map[string]any)["type"] == "string" {
			property := properties[name].(map[string]any)
			if strings.HasSuffix(name, "_id") || name == "id" {
				property["format"] = "uuid"
			}
			if strings.HasSuffix(name, "_date") {
				property["format"] = "date"
			}
			if strings.HasSuffix(name, "_at") || name == "registration_deadline" {
				property["format"] = "date-time"
			}
			if name == "email" {
				property["format"] = "email"
			}
			if name == "password" || strings.HasSuffix(name, "_password") {
				property["writeOnly"] = true
			}
		}
		if !strings.Contains(tag, ",omitempty") {
			required = append(required, name)
		}
	}
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func Handler(router chi.Routes, service string, models map[string]Models) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		document, err := PublicDocument(router, service, models)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		httputil.WriteJSON(w, 200, document)
	}
}

func Document(router chi.Routes, service string, models map[string]Models) (map[string]any, error) {
	return document(router, service, models, false)
}

func PublicDocument(router chi.Routes, service string, models map[string]Models) (map[string]any, error) {
	return document(router, service, models, true)
}

func document(router chi.Routes, service string, models map[string]Models, publicOnly bool) (map[string]any, error) {
	definitions := &schemas{components: map[string]any{}}
	problem := definitions.schema(reflect.TypeFor[httputil.Problem]())
	paths := map[string]map[string]any{}
	err := chi.Walk(router, func(method, path string, handler http.Handler, middleware ...func(http.Handler) http.Handler) error {
		if publicOnly && !strings.HasPrefix(path, "/api/v1/") {
			return nil
		}
		if strings.Contains(path, "*") || path == "/healthz" || path == "/readyz" || path == "/metrics" || path == "/openapi.json" {
			return nil
		}
		path = parameter.ReplaceAllString(path, "{$1}")
		responses := map[string]any{"default": map[string]any{"description": "RFC 9457 problem response", "content": map[string]any{"application/problem+json": map[string]any{"schema": problem}}}}
		operation := map[string]any{"operationId": service + "_" + method + "_" + operationCharacters.ReplaceAllString(path, "_"), "responses": responses}
		parameters := make([]any, 0)
		for _, match := range parameter.FindAllStringSubmatch(path, -1) {
			parameters = append(parameters, map[string]any{"name": match[1], "in": "path", "required": true, "schema": map[string]string{"type": "string"}})
		}
		name := ""
		if value := reflect.ValueOf(handler); value.Kind() == reflect.Func {
			if function := runtime.FuncForPC(value.Pointer()); function != nil {
				name = function.Name()
			}
		}
		if index := strings.LastIndex(name, "/"); index >= 0 {
			name = strings.TrimSuffix(name[index+1:], "-fm")
		}
		model, ok := models[method+" "+path]
		if !ok {
			model, ok = models[name]
		}
		if !ok && strings.HasPrefix(name, "handler.") {
			model, ok = models[strings.TrimPrefix(name, "handler.")]
		}
		if !ok {
			model, ok = commonModels()[method+" "+path]
		}
		if ok {
			if model.Request != nil {
				kind := model.RequestContentType
				if kind == "" {
					kind = "application/json"
				}
				operation["requestBody"] = map[string]any{"required": !model.BodyOptional, "content": map[string]any{kind: map[string]any{"schema": definitions.value(model.Request)}}}
			}
			status := model.Status
			if status == 0 {
				status = 200
			}
			responses[fmt.Sprint(status)] = responseSchema(definitions, status, model)
			for status, alternative := range model.OtherResponses {
				responses[fmt.Sprint(status)] = responseSchema(definitions, status, alternative)
			}
			for _, query := range model.Parameters {
				kind := query.Type
				if kind == "" {
					kind = "string"
				}
				schema := map[string]any{"type": kind}
				if query.Format != "" {
					schema["format"] = query.Format
				}
				if query.Default != nil {
					schema["default"] = query.Default
				}
				if len(query.Enum) > 0 {
					schema["enum"] = query.Enum
				}
				if query.Minimum != nil {
					schema["minimum"] = *query.Minimum
				}
				if query.Maximum != nil {
					schema["maximum"] = *query.Maximum
				}
				where := query.In
				if where == "" {
					where = "query"
				}
				parameters = append(parameters, map[string]any{"name": query.Name, "in": where, "required": query.Required, "description": query.Description, "schema": schema})
			}
			if model.Auth {
				operation["security"] = []any{map[string]any{"bearerAuth": []string{}}}
			}
			if model.OptionalAuth {
				operation["security"] = []any{map[string]any{}, map[string]any{"bearerAuth": []string{}}}
			}
			if model.CookieAuth {
				operation["security"] = []any{map[string]any{"refreshCookie": []string{}}}
			}
			if model.Description != "" {
				operation["description"] = model.Description
			}
		} else {
			operation["x-schema-status"] = "route-only"
			responses["2XX"] = map[string]any{"description": "Unregistered successful response"}
		}
		if len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		if strings.HasPrefix(path, "/internal/") {
			operation["security"] = []any{map[string]any{"serviceHMAC": []string{}}}
			operation["description"] = "Only the private TLS 1.3 listener accepts this route; a trusted client certificate and a request-bound HMAC token are required. " + model.Description
		}
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}
		paths[path][strings.ToLower(method)] = operation
		return nil
	})
	if err != nil {
		return nil, err
	}
	security := map[string]any{"bearerAuth": map[string]string{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}, "refreshCookie": map[string]string{"type": "apiKey", "in": "cookie", "name": "refresh_token"}}
	if !publicOnly {
		security["serviceHMAC"] = map[string]string{"type": "apiKey", "in": "header", "name": "X-Service-Token"}
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]string{"title": "ConfHub " + service, "version": "1.4.0", "description": "API for ConferenceHub accounts, events and participation. Operations and data models are generated from the current service contracts."}, "servers": []any{map[string]string{"url": "/"}}, "paths": paths, "components": map[string]any{"schemas": definitions.components, "securitySchemes": security}}, nil
}

func ValidateRoutes(router chi.Routes, service string, models map[string]Models) error {
	document, err := Document(router, service, models)
	if err != nil {
		return err
	}
	missing := []string{}
	for path, methods := range document["paths"].(map[string]map[string]any) {
		for method, value := range methods {
			if _, missingSchema := value.(map[string]any)["x-schema-status"]; missingSchema {
				missing = append(missing, method+" "+path)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("undocumented operations: %s", strings.Join(missing, ", "))
	}
	return nil
}

func responseSchema(definitions *schemas, status int, model Models) map[string]any {
	out := map[string]any{"description": http.StatusText(status)}
	if model.Response != nil {
		kind := model.ContentType
		if kind == "" {
			kind = "application/json"
		}
		out["content"] = map[string]any{kind: map[string]any{"schema": definitions.value(model.Response)}}
	}
	if status >= 300 && status < 400 {
		out["headers"] = map[string]any{"Location": map[string]any{"description": "Redirect destination", "schema": map[string]string{"type": "string", "format": "uri-reference"}}}
	}
	return out
}

func commonModels() map[string]Models {
	return map[string]Models{
		"GET /internal/operations/queues":                     {Response: map[string]any{"data": Array(map[string]any{"id": "", "queue": "", "status": "", "attempts": 0, "created_at": time.Time{}})}},
		"POST /internal/operations/queues/{queue}/{id}/retry": {Request: map[string]any{"changed_by": ""}, Status: 204, Description: "Retries only failed jobs. Requires a non-zero administrator actor UUID; the retry and audit record commit atomically. A duplicate retry returns 409."},
	}
}
