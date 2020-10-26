package main

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// ResponseExpectation is an interface to any validation
// that needs to be performed on response.
type ResponseExpectation interface {
	// check does the response meet expectation
	check(resp *Response) error

	// desc returns user-friendly description of expectation
	desc() string
}

// StatusCodeExpectation validates response HTTP code.
type StatusCodeExpectation struct {
	statusCode int
}

func (e StatusCodeExpectation) check(resp *Response) error {
	if resp.http.StatusCode != e.statusCode {
		return fmt.Errorf("Unexpected Status Code. Expected: %d, Actual: %d", e.statusCode, resp.http.StatusCode)
	}
	return nil
}

func (e StatusCodeExpectation) desc() string {
	return fmt.Sprintf("Status code is %d", e.statusCode)
}

// BodySchemaExpectation validates response body against schema.
// Content-Type header is used to identify either json schema or xsd is applied.
type BodySchemaExpectation struct {
	schema      []byte
	displayName string
}

func (e BodySchemaExpectation) check(resp *Response) error {
	contentType, _, _ := mime.ParseMediaType(resp.http.Header.Get("content-type"))

	if contentType == "application/json" {
		return e.checkJSON(resp)
	}

	return fmt.Errorf("Unsupported content type: %s", contentType)
}

func (e BodySchemaExpectation) desc() string {
	tmpl := "BodyPath matches the schema"
	if e.displayName == "" {
		return tmpl
	}

	return fmt.Sprintf(tmpl+" (%s)", e.displayName)
}

func (e BodySchemaExpectation) checkJSON(resp *Response) error {
	schemaLoader := gojsonschema.NewBytesLoader(e.schema)
	documentLoader := gojsonschema.NewStringLoader(string(resp.body))

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("failed to load schema file: %s", err)
	}

	if !result.Valid() {
		msg := "Unexpected Body Schema:"
		for _, desc := range result.Errors() {
			msg = fmt.Sprintf(msg+"\n\t%s", desc)
		}
		return errors.New(msg)
	}

	return nil
}

// BodyExpectation validates that expected object is presented in the response.
// The expected body reflect required part of the response object.
type BodyExpectation struct {
	Strict       bool
	ExpectedBody interface{}
}

func (e BodyExpectation) check(resp *Response) error {

	actualBody, err := resp.Body() // cached
	if err != nil {
		str := "Can't parse response body."
		str += " " + err.Error()

		return errors.New(str)
	}

	matcher := NewBodyMatcher{Strict: e.Strict, ExpectedBody: e.ExpectedBody}
	return matcher.check(actualBody)
}

func (e BodyExpectation) desc() string {
	return fmt.Sprint("Expected body's structure / values")
}

// BodyPathExpectation validates values under a certain path in a body.
// Applies to json and xml.
type BodyPathExpectation struct {
	pathExpectations map[string]interface{}
}

func (e BodyPathExpectation) check(resp *Response) error {

	for pathStr, expectedValue := range e.pathExpectations {

		err := responseBodyPathCheck(resp, bodyExpectationItem{Path: pathStr, ExpectedValue: expectedValue}, checkExpectedPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e BodyPathExpectation) desc() string {
	return fmt.Sprintf("Expected body's structure / values (%d checks)", len(e.pathExpectations))
}

type bodyExpectationItem struct {
	Path          string
	ExpectedValue interface{}
}

func checkExpectedPath(m interface{}, pathItem interface{}) string {

	if expectationItem, ok := pathItem.(bodyExpectationItem); ok {

		err := SearchByPath(m, expectationItem.ExpectedValue, expectationItem.Path)
		if err != nil {
			return err.Error()
		}

		return ""
	}

	return fmt.Sprintf("Path Item: %v is invalid for expectation check", pathItem)
}

// HeaderExpectation validates one header in a response.
type HeaderExpectation struct {
	Name        string
	Value       string
	ValueParser func(string) string
}

func (e HeaderExpectation) check(resp *Response) error {
	value := resp.http.Header.Get(e.Name)
	if e.ValueParser != nil {
		value = e.ValueParser(value)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("Missing header. Expected \"%s: %s\"", e.Name, e.Value)
	}
	if e.Value != "" && e.Value != value {
		return fmt.Errorf("Unexpected header. Expected \"%s: %s\". Actual \"%s: %s\"", e.Name, e.Value, e.Name, value)
	}
	return nil
}

func (e HeaderExpectation) desc() string {
	return fmt.Sprintf("Header '%s' matches expected value '%s", e.Name, e.Value)
}

// ContentTypeExpectation validates media type returned in the Content-Type header.
// Encoding information is excluded from matching value.
// E.g. "application/json;charset=utf-8" header transformed to "application/json" media type.
type ContentTypeExpectation struct {
	Value string
}

func (e ContentTypeExpectation) check(resp *Response) error {
	parser := func(value string) string {
		contentType, _, _ := mime.ParseMediaType(value)
		return contentType
	}

	headerCheck := HeaderExpectation{"content-type", e.Value, parser}
	return headerCheck.check(resp)
}

func (e ContentTypeExpectation) desc() string {
	return fmt.Sprintf("Content Type is '%s'", e.Value)
}

// AbsentExpectation validates paths are absent in response body
type AbsentExpectation struct {
	paths []string
}

func (e AbsentExpectation) check(resp *Response) error {

	for _, pathStr := range e.paths {
		err := responseBodyPathCheck(resp, pathStr, checkAbsentPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e AbsentExpectation) desc() string {
	buf := bytes.NewBufferString("")

	buf.WriteString("Absent fields:")
	for _, path := range e.paths {
		buf.WriteString(fmt.Sprintf("\n  - %s", path))
	}

	return buf.String()
}

type pathCheckFunc func(m interface{}, pathItem interface{}) string

func responseBodyPathCheck(resp *Response, pathItem interface{}, checkPath pathCheckFunc) error {

	m, err := resp.Body() // cached
	if err != nil {
		str := "Can't parse response body to Map." // TODO specific message for functions
		str += " " + err.Error()

		return errors.New(str)
	}

	str := checkPath(m, pathItem)
	if str != "" {
		return errors.New(str)
	}

	return nil
}

func checkAbsentPath(m interface{}, pathItem interface{}) string {

	if pathStr, ok := pathItem.(string); ok {

		searchResult := Search(m, pathStr)
		if len(searchResult) > 0 {
			return fmt.Sprintf("Value expected to be absent was found: %v, path: %v", searchResult, pathStr)
		}

		return ""
	}

	return fmt.Sprintf("Path Item: %v is invalid for absence check", pathItem)
}

// PresentExpectation validates paths exists in response body
type PresentExpectation struct {
	paths []string
}

func (e PresentExpectation) check(resp *Response) error {

	for _, pathStr := range e.paths {
		err := responseBodyPathCheck(resp, pathStr, checkPresentPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e PresentExpectation) desc() string {
	buf := bytes.NewBufferString("")

	buf.WriteString("Present fields:")
	for _, path := range e.paths {
		buf.WriteString(fmt.Sprintf("\n  - %s", path))
	}

	return buf.String()
}

func checkPresentPath(m interface{}, pathItem interface{}) string {

	if pathStr, ok := pathItem.(string); ok {

		searchResult := Search(m, pathStr)
		if !(len(searchResult) > 0) {
			return fmt.Sprintf("Value expected to be exist was not found: %v, path: %v", searchResult, pathStr)
		}

		return ""
	}

	return fmt.Sprintf("Path Item: %v is invalid for presence check", pathItem)
}
