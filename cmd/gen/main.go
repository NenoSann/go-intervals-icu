package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type Spec struct {
	Paths      map[string]map[string]Operation `json:"paths"`
	Components Components                      `json:"components"`
}

type Components struct {
	Schemas map[string]Schema `json:"schemas"`
}

type Operation struct {
	OperationID string              `json:"operationId"`
	Parameters  []Parameter         `json:"parameters"`
	RequestBody *RequestBody        `json:"requestBody"`
	Responses   map[string]Response `json:"responses"`
}

type Parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   Schema `json:"schema"`
}

type RequestBody struct {
	Content  map[string]MediaType `json:"content"`
	Required bool                 `json:"required"`
}

type Response struct {
	Content map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema Schema `json:"schema"`
}

type Schema struct {
	Ref                  string            `json:"$ref"`
	Type                 string            `json:"type"`
	Format               string            `json:"format"`
	Properties           map[string]Schema `json:"properties"`
	Items                *Schema           `json:"items"`
	AdditionalProperties *Schema           `json:"additionalProperties"`
	Required             []string          `json:"required"`
	Enum                 []string          `json:"enum"`
}

type Generator struct {
	spec          Spec
	usedNames     map[string]bool
	extraSchemas  map[string]Schema
	childTypeName map[string]string
}

func main() {
	spec, err := loadSpec("openapi-spec.json")
	if err != nil {
		panic(err)
	}
	g := NewGenerator(spec)
	g.collectComponentTypes()
	g.collectOperationInlineTypes()

	typesCode, err := g.generateTypesFile()
	if err != nil {
		panic(err)
	}
	clientCode, err := g.generateClientFile()
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile("types_gen.go", typesCode, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile("client_gen.go", clientCode, 0o644); err != nil {
		panic(err)
	}
}

func loadSpec(path string) (Spec, error) {
	var spec Spec
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return spec, err
	}
	defer f.Close()
	payload, err := io.ReadAll(f)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(payload, &spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func NewGenerator(spec Spec) *Generator {
	used := make(map[string]bool)
	for name := range spec.Components.Schemas {
		used[name] = true
	}
	return &Generator{
		spec:          spec,
		usedNames:     used,
		extraSchemas:  make(map[string]Schema),
		childTypeName: make(map[string]string),
	}
}

func (g *Generator) collectComponentTypes() {
	for name, schema := range g.spec.Components.Schemas {
		g.collectSchema(name, schema)
	}
}

func (g *Generator) collectOperationInlineTypes() {
	for _, op := range g.operations() {
		opName := op.GoName
		if bodySchema, ok := op.requestSchema(); ok {
			if bodySchema.Ref == "" {
				name := opName + "RequestBody"
				g.registerExtraSchema(name, bodySchema)
				g.collectSchema(name, bodySchema)
			}
		}
		if respSchema, ok := op.responseSchema(); ok {
			if respSchema.Ref == "" {
				name := opName + "ResultBody"
				g.registerExtraSchema(name, respSchema)
				g.collectSchema(name, respSchema)
			}
		}
	}
}

func (g *Generator) registerExtraSchema(name string, schema Schema) {
	if g.usedNames[name] {
		return
	}
	g.usedNames[name] = true
	g.extraSchemas[name] = schema
}

func (g *Generator) collectSchema(parentName string, schema Schema) {
	if schema.Ref != "" {
		return
	}
	if schema.Type == "array" {
		if schema.Items != nil {
			g.collectChildSchema(parentName, "Item", *schema.Items)
		}
		return
	}
	if schema.Type == "object" || len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
		for propName, propSchema := range schema.Properties {
			g.collectChildSchema(parentName, propName, propSchema)
		}
		if schema.AdditionalProperties != nil {
			g.collectChildSchema(parentName, "AdditionalProperty", *schema.AdditionalProperties)
		}
	}
}

func (g *Generator) collectChildSchema(parentName, propName string, schema Schema) {
	if schema.Ref != "" {
		return
	}
	if schema.Type == "array" {
		if schema.Items != nil {
			g.collectChildSchema(parentName+toGoName(propName), "Item", *schema.Items)
		}
		return
	}
	if schema.Type == "object" || len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
		if len(schema.Properties) == 0 && schema.AdditionalProperties == nil {
			return
		}
		name := g.childType(parentName, propName, schema)
		g.collectSchema(name, schema)
	}
}

func (g *Generator) childType(parentName, propName string, schema Schema) string {
	key := parentName + ":" + propName
	if existing, ok := g.childTypeName[key]; ok {
		return existing
	}
	base := parentName + toGoName(propName)
	name := g.uniqueTypeName(base)
	g.childTypeName[key] = name
	g.extraSchemas[name] = schema
	return name
}

func (g *Generator) uniqueTypeName(base string) string {
	name := base
	for i := 2; g.usedNames[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	g.usedNames[name] = true
	return name
}

func (g *Generator) generateTypesFile() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen; DO NOT EDIT.\n")
	buf.WriteString("\n")
	buf.WriteString("package intervalsicu\n\n")

	all := make(map[string]Schema)
	for name, schema := range g.spec.Components.Schemas {
		all[name] = schema
	}
	for name, schema := range g.extraSchemas {
		all[name] = schema
	}

	var names []string
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		schema := all[name]
		buf.WriteString(g.renderType(name, schema))
		buf.WriteString("\n")
	}

	return format.Source(buf.Bytes())
}

func (g *Generator) renderType(name string, schema Schema) string {
	if schema.Ref != "" {
		return fmt.Sprintf("type %s %s\n", name, refName(schema.Ref))
	}
	if schema.Type == "array" {
		itemType := "any"
		if schema.Items != nil {
			itemType = g.schemaType(name, "Item", *schema.Items)
		}
		return fmt.Sprintf("type %s []%s\n", name, itemType)
	}
	if schema.Type == "object" || len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
		if len(schema.Properties) == 0 && schema.AdditionalProperties != nil {
			valueType := g.schemaType(name, "AdditionalProperty", *schema.AdditionalProperties)
			return fmt.Sprintf("type %s map[string]%s\n", name, valueType)
		}
		if len(schema.Properties) == 0 {
			return fmt.Sprintf("type %s map[string]any\n", name)
		}
		return g.renderStruct(name, schema)
	}

	return fmt.Sprintf("type %s %s\n", name, primitiveType(schema))
}

func (g *Generator) renderStruct(name string, schema Schema) string {
	required := make(map[string]bool)
	for _, field := range schema.Required {
		required[field] = true
	}

	props := make([]string, 0, len(schema.Properties))
	for prop := range schema.Properties {
		props = append(props, prop)
	}
	sort.Strings(props)

	used := make(map[string]int)
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))
	for _, propName := range props {
		propSchema := schema.Properties[propName]
		fieldName := toGoName(propName)
		if fieldName == "" {
			fieldName = "Field"
		}
		used[fieldName]++
		if used[fieldName] > 1 {
			fieldName = fmt.Sprintf("%s%d", fieldName, used[fieldName])
		}
		fieldType := g.optionalType(name, propName, propSchema, required[propName])
		tag := propName
		if !required[propName] {
			tag = tag + ",omitempty"
		}
		buf.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n", fieldName, fieldType, tag))
	}
	buf.WriteString("}\n")
	return buf.String()
}

func (g *Generator) optionalType(parentName, propName string, schema Schema, required bool) string {
	typeName := g.schemaType(parentName, propName, schema)
	if required {
		return typeName
	}
	if strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "map[") {
		return typeName
	}
	if typeName == "any" || typeName == "interface{}" {
		return typeName
	}
	return "*" + typeName
}

func (g *Generator) schemaType(parentName, propName string, schema Schema) string {
	if schema.Ref != "" {
		return refName(schema.Ref)
	}
	if schema.Type == "array" {
		itemType := "any"
		if schema.Items != nil {
			itemType = g.schemaType(parentName+toGoName(propName), "Item", *schema.Items)
		}
		return "[]" + itemType
	}
	if schema.Type == "object" || len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
		if len(schema.Properties) == 0 && schema.AdditionalProperties != nil {
			valueType := g.schemaType(parentName+toGoName(propName), "Value", *schema.AdditionalProperties)
			return "map[string]" + valueType
		}
		if len(schema.Properties) == 0 {
			return "map[string]any"
		}
		return g.childType(parentName, propName, schema)
	}
	return primitiveType(schema)
}

func primitiveType(schema Schema) string {
	switch schema.Type {
	case "string":
		switch schema.Format {
		case "binary", "byte":
			return "[]byte"
		default:
			return "string"
		}
	case "integer":
		switch schema.Format {
		case "int32":
			return "int32"
		case "int64":
			return "int64"
		default:
			return "int"
		}
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "object":
		return "map[string]any"
	default:
		return "any"
	}
}

func refName(ref string) string {
	const prefix = "#/components/schemas/"
	return strings.TrimPrefix(ref, prefix)
}

func toGoName(input string) string {
	parts := splitWords(input)
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(capitalize(part))
	}
	name := b.String()
	if name == "" {
		return ""
	}
	if unicode.IsDigit(rune(name[0])) {
		return "Field" + name
	}
	return name
}

func splitWords(input string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = current[:0]
	}

	var prev rune
	for i, r := range input {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			prev = 0
			continue
		}
		if i > 0 && (unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev))) {
			flush()
		}
		current = append(current, r)
		prev = r
	}
	flush()
	return words
}

var initialisms = map[string]string{
	"id":    "ID",
	"url":   "URL",
	"http":  "HTTP",
	"https": "HTTPS",
	"api":   "API",
	"icu":   "ICU",
	"gps":   "GPS",
	"hr":    "HR",
	"ftp":   "FTP",
}

func capitalize(word string) string {
	lower := strings.ToLower(word)
	if v, ok := initialisms[lower]; ok {
		return v
	}
	runes := []rune(word)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// operations helper

type operationEntry struct {
	Path   string
	Method string
	Op     Operation
	GoName string
}

func (g *Generator) operations() []operationEntry {
	var ops []operationEntry
	for path, methods := range g.spec.Paths {
		for method, op := range methods {
			if op.OperationID == "" {
				continue
			}
			ops = append(ops, operationEntry{
				Path:   path,
				Method: strings.ToUpper(method),
				Op:     op,
				GoName: toGoName(op.OperationID),
			})
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].GoName < ops[j].GoName
	})
	return ops
}

func (op operationEntry) requestSchema() (Schema, bool) {
	if op.Op.RequestBody == nil {
		return Schema{}, false
	}
	content := op.Op.RequestBody.Content
	if len(content) == 0 {
		return Schema{}, false
	}
	if mt, ok := content["application/json"]; ok {
		return mt.Schema, true
	}
	for _, mt := range content {
		return mt.Schema, true
	}
	return Schema{}, false
}

func (op operationEntry) responseSchema() (Schema, bool) {
	if len(op.Op.Responses) == 0 {
		return Schema{}, false
	}
	if resp, ok := op.Op.Responses["200"]; ok {
		return firstSchema(resp)
	}
	var keys []string
	for k := range op.Op.Responses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return Schema{}, false
	}
	return firstSchema(op.Op.Responses[keys[0]])
}

func firstSchema(resp Response) (Schema, bool) {
	if len(resp.Content) == 0 {
		return Schema{}, false
	}
	if mt, ok := resp.Content["application/json"]; ok {
		return mt.Schema, true
	}
	for _, mt := range resp.Content {
		return mt.Schema, true
	}
	return Schema{}, false
}

func (g *Generator) generateClientFile() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen; DO NOT EDIT.\n")
	buf.WriteString("\n")
	buf.WriteString("package intervalsicu\n\n")
	buf.WriteString("import (\n\t\"context\"\n\t\"fmt\"\n\t\"net/url\"\n)\n\n")

	for _, op := range g.operations() {
		buf.WriteString(g.renderOperation(op))
		buf.WriteString("\n")
	}

	return format.Source(buf.Bytes())
}

func (g *Generator) renderOperation(op operationEntry) string {
	var buf strings.Builder

	paramsName := op.GoName + "Params"
	buf.WriteString(fmt.Sprintf("type %s struct {\n", paramsName))

	pathParams, queryParams := splitParams(op.Op.Parameters)
	for _, param := range pathParams {
		fieldName := toGoName(param.Name)
		fieldType := g.paramType(param.Schema, true)
		buf.WriteString(fmt.Sprintf("\t%s %s\n", fieldName, fieldType))
	}
	for _, param := range queryParams {
		fieldName := toGoName(param.Name)
		fieldType := g.paramType(param.Schema, param.Required)
		if !param.Required && !strings.HasPrefix(fieldType, "[]") && !strings.HasPrefix(fieldType, "map[") {
			fieldType = "*" + fieldType
		}
		buf.WriteString(fmt.Sprintf("\t%s %s\n", fieldName, fieldType))
	}
	if bodySchema, ok := op.requestSchema(); ok {
		bodyType := g.requestBodyType(op, bodySchema)
		if op.Op.RequestBody != nil && !op.Op.RequestBody.Required {
			bodyType = "*" + bodyType
		}
		buf.WriteString(fmt.Sprintf("\tBody %s\n", bodyType))
	}
	buf.WriteString("}\n\n")

	responseType := g.responseType(op)
	buf.WriteString(fmt.Sprintf("type %sResult = %s\n\n", op.GoName, responseType))

	buf.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context, params %s) (*%sResult, error) {\n", op.GoName, paramsName, op.GoName))

	buf.WriteString("\tpathParams := map[string]string{\n")
	for _, param := range pathParams {
		fieldName := toGoName(param.Name)
		if op.isAthletePath() && param.Name == "id" {
			buf.WriteString(fmt.Sprintf("\t\t\"%s\": c.athleteIDOr(params.%s),\n", param.Name, fieldName))
			continue
		}
		buf.WriteString(fmt.Sprintf("\t\t\"%s\": fmt.Sprint(params.%s),\n", param.Name, fieldName))
	}
	buf.WriteString("\t}\n")

	buf.WriteString("\tquery := url.Values{}\n")
	for _, param := range queryParams {
		fieldName := toGoName(param.Name)
		if param.Required {
			if isSliceType(g.paramType(param.Schema, param.Required)) {
				buf.WriteString(fmt.Sprintf("\tfor _, v := range params.%s {\n", fieldName))
				buf.WriteString(fmt.Sprintf("\t\tquery.Add(\"%s\", fmt.Sprint(v))\n", param.Name))
				buf.WriteString("\t}\n")
			} else {
				buf.WriteString(fmt.Sprintf("\tquery.Set(\"%s\", fmt.Sprint(params.%s))\n", param.Name, fieldName))
			}
			continue
		}
		if isSliceType(g.paramType(param.Schema, param.Required)) {
			buf.WriteString(fmt.Sprintf("\tif len(params.%s) > 0 {\n", fieldName))
			buf.WriteString(fmt.Sprintf("\t\tfor _, v := range params.%s {\n", fieldName))
			buf.WriteString(fmt.Sprintf("\t\t\tquery.Add(\"%s\", fmt.Sprint(v))\n", param.Name))
			buf.WriteString("\t\t}\n\t}\n")
		} else {
			buf.WriteString(fmt.Sprintf("\tif params.%s != nil {\n", fieldName))
			buf.WriteString(fmt.Sprintf("\t\tquery.Set(\"%s\", fmt.Sprint(*params.%s))\n", param.Name, fieldName))
			buf.WriteString("\t}\n")
		}
	}

	bodyExpr := "nil"
	if _, ok := op.requestSchema(); ok {
		bodyExpr = "params.Body"
	}

	buf.WriteString(fmt.Sprintf("\tvar out %sResult\n", op.GoName))
	buf.WriteString(fmt.Sprintf("\tif err := c.do(ctx, \"%s\", \"%s\", pathParams, query, %s, &out); err != nil {\n", op.Method, op.Path, bodyExpr))
	buf.WriteString("\t\treturn nil, err\n\t}\n")
	buf.WriteString("\treturn &out, nil\n")
	buf.WriteString("}\n")

	return buf.String()
}

func (g *Generator) requestBodyType(op operationEntry, schema Schema) string {
	if schema.Ref != "" {
		return refName(schema.Ref)
	}
	name := op.GoName + "RequestBody"
	g.registerExtraSchema(name, schema)
	g.collectSchema(name, schema)
	return name
}

func (g *Generator) responseType(op operationEntry) string {
	respSchema, ok := op.responseSchema()
	if !ok {
		return "struct{}"
	}
	if respSchema.Ref != "" {
		return refName(respSchema.Ref)
	}
	name := op.GoName + "ResultBody"
	g.registerExtraSchema(name, respSchema)
	g.collectSchema(name, respSchema)
	return name
}

func (g *Generator) paramType(schema Schema, required bool) string {
	if schema.Ref != "" {
		return refName(schema.Ref)
	}
	if schema.Type == "array" {
		itemType := "any"
		if schema.Items != nil {
			itemType = g.paramType(*schema.Items, true)
		}
		return "[]" + itemType
	}
	if schema.Type == "object" {
		return "map[string]any"
	}
	return primitiveType(schema)
}

func splitParams(params []Parameter) ([]Parameter, []Parameter) {
	var pathParams []Parameter
	var queryParams []Parameter
	for _, param := range params {
		switch param.In {
		case "path":
			pathParams = append(pathParams, param)
		case "query":
			queryParams = append(queryParams, param)
		}
	}
	sort.Slice(pathParams, func(i, j int) bool { return pathParams[i].Name < pathParams[j].Name })
	sort.Slice(queryParams, func(i, j int) bool { return queryParams[i].Name < queryParams[j].Name })
	return pathParams, queryParams
}

func (op operationEntry) isAthletePath() bool {
	return strings.HasPrefix(op.Path, "/api/v1/athlete/{id}")
}

func isSliceType(goType string) bool {
	return strings.HasPrefix(goType, "[]")
}
