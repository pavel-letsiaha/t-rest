package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/clbanning/mxj"
	"github.com/pkg/errors"
)

// TestSuite represents file with test cases.
type TestSuite struct {
	// file name
	Name string
	// Path to a directory where suite is located
	// Relative to the suite root
	Dir string
	// test cases listed in a file
	Cases []TestCase
}

// PackageName builds name of a package based on folder where test is located
func (suite TestSuite) PackageName() string {
	if strings.HasPrefix(suite.Dir, ".") {
		return ""
	}

	return strings.Replace(filepath.ToSlash(suite.Dir), "/", ".", -1)
}

// FullName builds name of the test including package and test name
func (suite TestSuite) FullName() string {
	pkg := suite.PackageName()
	if pkg == "" {
		return suite.Name
	}

	return fmt.Sprintf("%s.%s", suite.PackageName(), suite.Name)
}

// TestCase represents single test scenario
type TestCase struct {
	Name   string                 `json:"name,omitempty"`
	Ignore *string                `json:"ignore,omitempty"`
	Args   map[string]interface{} `json:"args,omitempty"`
	Calls  []Call                 `json:"calls,omitempty"`
}

// Call defines metadata for one request-response verification within TestCase
type Call struct {
	Args     map[string]interface{} `json:"args,omitempty"`
	On       On                     `json:"on,omitempty"`
	Expect   Expect                 `json:"expect,omitempty"`
	Remember Remember               `json:"remember,omitempty"`
}

// Remember defines items from HTTP response to persist for usage in future calls
type Remember struct {
	BPath   map[string]string `json:"bodyPath,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// On is a metadata for building a HTTP request
type On struct {
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Params   map[string]string `json:"params"`
	Body     json.RawMessage   `json:"body"`
	BodyFile string            `json:"bodyFile"`
}

// BodyContent returns request body content regardless of its source
// e.g. provided inline or fetched from file
func (on On) BodyContent(suitePath string) (string, error) {
	const quote byte = '"'

	dat := []byte(on.Body)
	if len(dat) > 0 && dat[0] == quote && dat[len(dat)-1] == quote {
		dat = dat[1 : len(dat)-1]
	} // remove leading and trailing double quotes (suppress JSON string)

	if on.BodyFile != "" {
		uri, err := toAbsPath(suitePath, on.BodyFile)
		if err != nil {
			return "", err
		}

		d, err := ioutil.ReadFile(uri)
		if err != nil {
			return "", fmt.Errorf("Can't read body file: %s", err.Error())
		}

		dat = d
	}

	return string(dat), nil
}

// Expect is a metadata for HTTP response verification
type Expect struct {
	StatusCode int `json:"statusCode"`
	// shortcut for content-type header
	ContentType    string                 `json:"contentType"`
	Headers        map[string]string      `json:"headers"`
	BPath          map[string]interface{} `json:"bodyPath"`
	Body           interface{}            `json:"body"`
	ExactBody      interface{}            `json:"exactBody"`
	Absent         []string               `json:"absent"`
	Present        []string               `json:"present"`
	BodySchemaRaw  json.RawMessage        `json:"bodySchema"`
	BodySchemaFile string                 `json:"bodySchemaFile"`
	BodySchemaURI  string                 `json:"bodySchemaURI"`
}

func (e Expect) BodyPath() map[string]interface{} {
	return e.BPath
}

var jsonSchemaCache sync.Map

func (e Expect) loadSchemaFromFile(suitePath string) ([]byte, error) {

	if e.BodySchemaFile == "" {
		return nil, nil
	}

	uri, err := toAbsPath(suitePath, e.BodySchemaFile)
	if err != nil {
		return nil, err
	}

	var cached, ok = jsonSchemaCache.Load(uri)
	if ok {
		debugf("loading json schema from the cache: %s", uri)
		v, _ := cached.([]byte)
		return v, nil
	}

	debugf("loading json schema: %s", uri)

	schema, err := ioutil.ReadFile(uri)
	if err != nil {
		return nil, err
	}

	jsonSchemaCache.Store(uri, schema)

	return schema, nil
}

func (e Expect) loadSchemaFromURI() ([]byte, error) {
	uri := toAbsURL(hostFlag, e.BodySchemaURI)

	if uri == "" {
		return nil, nil
	}

	var cached, ok = jsonSchemaCache.Load(uri)
	if ok {
		debugf("loading json schema from the cache: %s", uri)
		v, _ := cached.([]byte)
		return v, nil
	}

	debugf("loading json schema: %s", uri)

	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}

	schema, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	jsonSchemaCache.Store(uri, schema)

	return schema, nil
}

func (e *Expect) populateWith(vars *Vars) error {
	tmplCtx := NewTemplateContext(vars)

	//expect.Headers        map[string]string
	for name, valueTmpl := range e.Headers {
		e.Headers[name] = tmplCtx.ApplyTo(valueTmpl)
	}

	e.Body = populateProperty(tmplCtx, e.Body)
	e.ExactBody = populateProperty(tmplCtx, e.ExactBody)
	e.BPath = populateProperty(tmplCtx, e.BodyPath()).(map[string]interface{})

	if tmplCtx.HasErrors() {
		return tmplCtx.Error()
	}

	return nil
}

func populateProperty(tmpl *TemplateContext, prop interface{}) interface{} {

	switch typedProp := prop.(type) {
	case string:
		r := tmpl.ApplyTo(typedProp)
		debugf("Populated template: %v -> %v", typedProp, r)
		return r

	case []string:
		var result = make([]string, 0)
		for _, item := range typedProp {
			result = append(result, populateProperty(tmpl, item).(string))
		}
		return result

	case map[string]interface{}:
		result := make(map[string]interface{})
		for pk, pv := range typedProp {
			result[pk] = populateProperty(tmpl, pv)
		}
		return result

	default:
		// no transformation are required
		return prop
	}

}

func toAbsPath(suitePath string, assetPath string) (string, error) {
	debug.Printf("Building absolute path using: suiteDir: %s, srcDir: %s, assetPath: %s", suitesDir, suitePath, assetPath)
	if filepath.IsAbs(assetPath) {
		// ignore srcDir
		return assetPath, nil
	}

	uri, err := filepath.Abs(filepath.Join(suitesDir, suitePath, assetPath))
	if err != nil {
		return "", errors.New("Invalid file path: " + assetPath)
	}

	return filepath.ToSlash(uri), nil
}

// toAbsURL returns absolute URL to a schema
func toAbsURL(baseHost, uri string) string {
	if uri == "" {
		return ""
	}

	isHTTP := strings.HasPrefix(uri, "http://")
	isHTTPS := strings.HasPrefix(uri, "https://")

	if isHTTP || isHTTPS {
		return uri
	}

	return strings.TrimSuffix(baseHost, "/") + "/" + strings.TrimPrefix(uri, "/")
}

// TestResult represents single test case for reporting
type TestResult struct {
	Suite      TestSuite
	Case       TestCase
	Skipped    bool
	SkippedMsg string
	Traces     []*CallTrace

	ExecFrame TimeFrame
}

func (result *TestResult) hasError() bool {
	for _, trace := range result.Traces {
		if trace.hasError() {
			return true
		}
	}
	return false
}

func (result *TestResult) Error() string {
	for _, trace := range result.Traces {
		if trace.hasError() {
			return trace.ErrorCause.Error()
		}
	}
	return ""
}

// TimeFrame describes period of time
type TimeFrame struct {
	Start time.Time
	End   time.Time
}

// Duration of TimeFrame from Start to End
func (tf TimeFrame) Duration() time.Duration {
	return tf.End.Sub(tf.Start)
}

// Extend does extension of time perod by other provided TimeFrame
// from earlier start to elder end
func (tf *TimeFrame) Extend(tf2 TimeFrame) {
	if tf.Start.After(tf2.Start) {
		tf.Start = tf2.Start
	}

	if tf.End.Before(tf2.End) {
		tf.End = tf2.End
	}
}

// CallTrace stands for test error in report
type CallTrace struct {
	Num           int
	RequestMethod string
	RequestURL    string
	RequestDump   string
	ResponseDump  string
	ErrorCause    error
	ExpDesc       map[string]bool
	ExecFrame     TimeFrame
}

func (trace *CallTrace) addExp(desc string) {
	if trace.ExpDesc == nil {
		trace.ExpDesc = make(map[string]bool)
	}
	trace.ExpDesc[desc] = false
}

func (trace *CallTrace) addFail(err error) {
	if trace.ExpDesc == nil {
		trace.ExpDesc = make(map[string]bool)
	}

	trace.ErrorCause = err
	trace.ExpDesc[err.Error()] = true
}

func (trace *CallTrace) hasError() bool {
	return trace.ErrorCause != nil
}

// Terminated returns true if request failed due to the issues with making request
// or parsing response, not due to failed expectations
func (trace *CallTrace) Terminated() bool {
	return trace.hasError() && !trace.hasFailedExp()
}

func (trace *CallTrace) hasFailedExp() bool {
	for _, failed := range trace.ExpDesc {
		if failed {
			return true
		}
	}

	return false
}

// Response wraps test call HTTP response
type Response struct {
	http       *http.Response
	body       []byte
	parsedBody interface{}
}

// Body returns parsed response (array or map) depending on provided 'Content-Type'
// supported content types are 'application/json', 'application/xml', 'text/xml'
func (resp *Response) Body() (interface{}, error) {
	if resp.parsedBody != nil {
		return resp.parsedBody, nil
	}

	var err error
	resp.parsedBody, err = resp.parseBody()

	return resp.parsedBody, err
}

func (resp Response) parseBody() (interface{}, error) {

	if len(resp.body) == 0 {
		return nil, nil
	}

	contentType, _, _ := mime.ParseMediaType(resp.http.Header.Get("content-type"))
	if contentType == "application/xml" || contentType == "text/xml" {
		m, err := mxj.NewMapXml(resp.body)
		if err == nil {
			return m.Old(), nil
		}
		return nil, err
	}

	if contentType == "application/json" {
		var (
			body interface{}
			err  error
		)
		if string(resp.body[0]) == "[" {
			body = make([]interface{}, 0)
			err = json.Unmarshal(resp.body, &body)
		} else {
			body = make(map[string]interface{})
			err = json.Unmarshal(resp.body, &body)
		}

		if err == nil {
			return body, nil
		}
		return nil, err
	}

	if contentType == "text/html" {
		m, err := mxj.NewMapXmlSeq(resp.body)
		if err == nil {
			return m.Old(), nil
		}
		return nil, err
	}

	return nil, errors.New("Cannot parse body. Unsupported content type")
}

// ToString return string representation of response data
// including status code, headers and body.
func (resp *Response) ToString() string {
	http := resp.http

	headers := "\n"
	for k, v := range http.Header {
		headers = fmt.Sprintf("%s%s: %s\n", headers, k, strings.Join(v, " "))
	}

	var body interface{}
	contentType, _, _ := mime.ParseMediaType(resp.http.Header.Get("content-type"))
	if contentType == "application/json" {
		data, _ := resp.Body()
		body, _ = json.MarshalIndent(data, "", "  ")
	}

	if contentType == "application/xml" || contentType == "text/xml" {
		resp.Body()
		mp, _ := mxj.NewMapXml(resp.body, false)
		body, _ = mp.XmlIndent("", "  ")
	}

	if contentType == "text/html" {
		resp.Body()
		body = resp.body
	}

	if body == nil {
		body = ""
	}

	details := fmt.Sprintf("%s \n %s \n%s", http.Status, headers, body)
	return details
}

const (
	envVarPrefix       = "env"
	ctxVarPrefix       = "ctx"
	varPrefixSeparator = ":"
)

// Vars defines map of test case level variables (e.g. args, remember, env)
type Vars struct {
	// variables ready to be used
	items map[string]interface{}
	used  map[string]bool
}

// NewVars create new Vars object with default set of env variables
func NewVars(baseURL string) *Vars {
	v := &Vars{
		items: make(map[string]interface{}),
		used:  make(map[string]bool),
	}

	v.addContext(baseURL)
	v.addEnv()

	return v
}

func (v *Vars) addContext(baseURL string) {
	v.items[ctxVarPrefix+varPrefixSeparator+"base_url"] = baseURL
}

func (v *Vars) addEnv() {

	for _, e := range os.Environ() {
		v.parseEnv(e)
	}
}

func (v *Vars) parseEnv(env string) {
	pair := strings.SplitN(env, "=", 2)
	v.items[envVarPrefix+varPrefixSeparator+pair[0]] = pair[1]
}

// Add is adding variable with name and value to map.
// References to other variables will be resolved upon add.
// If variable is a template, it will executed.
func (v *Vars) Add(name string, val interface{}) error {
	return v.addInScope(name, val, make(map[string]interface{}))
}

func (v *Vars) addInScope(name string, val interface{}, scope map[string]interface{}) error {
	debugf("Adding new argument: %s - %+v\n", name, val)

	if !v.isUserDefined(name) {
		if _, ok := v.items[name]; ok {
			return fmt.Errorf("%s is already defined. Overriding is not allowed", name)
		}
	}

	if str, ok := val.(string); ok {
		tmplCtx := NewTemplateContext(v)

		for in, iv := range scope {
			arg := fmt.Sprintf("{%s}", in)

			if !strings.Contains(str, arg) {
				continue
			}

			delete(scope, in)

			v.addInScope(in, iv, scope)
		}

		str = v.ApplyTo(str)

		v.items[name] = tmplCtx.ApplyTo(str)

		if tmplCtx.HasErrors() {
			debugf("Cannot add new argument: %s\n", tmplCtx.Error())
			return errors.Wrapf(tmplCtx.Error(), "Cannot evaluate `"+name+"`")
		}

		debugf("Added argument: %s - %s\n", name, v.items[name])

		return nil
	}

	v.items[name] = val
	return nil
}

// AddAll adds all passed arguments in a single scope. Means items can refer to each other.
func (v *Vars) AddAll(src map[string]interface{}) error {
	if src == nil {
		return nil
	}

	scope := make(map[string]interface{})
	for ik, iv := range src {
		scope[ik] = iv
	}

	for ik, iv := range src {
		// Scope shall contain only non-processed items.
		// By doing it before v.Add we avoid self-referencing.
		delete(scope, ik)

		err := v.addInScope(ik, iv, scope)
		if err != nil {
			return err
		}
	}

	return nil
}

// ApplyTo updates input template with values correspondent to placeholders
// according to current vars map
func (v *Vars) ApplyTo(str string) string {
	for varName, val := range v.items {
		placeholder := "{" + varName + "}"
		assembled := strings.Replace(str, placeholder, toString(val), -1)

		used := assembled != str

		if v.isUserDefined(varName) && used {
			v.used[varName] = true
		} // check used excluding ctx and env

		str = assembled
	}

	return str
}

// Unused returns the slice of var names not replaced so far in any templates
func (v *Vars) Unused() []string {

	unused := make([]string, 0, len(v.items)-len(v.used))
	for varName := range v.items {
		if v.used[varName] {
			continue
		}

		if !v.isUserDefined(varName) {
			continue
		}

		unused = append(unused, varName)
	}

	return unused
}

func (v *Vars) String() string {
	str := "Vars {"
	for varName, val := range v.items {
		if !v.isUserDefined(varName) {
			continue
		}
		str += fmt.Sprintf("%s=%s; ", varName, val)
	}

	str += "}"

	return str
}

func (v *Vars) isUserDefined(varName string) bool {
	if strings.HasPrefix(varName, ctxVarPrefix+varPrefixSeparator) {
		return false
	}

	if strings.HasPrefix(varName, envVarPrefix+varPrefixSeparator) {
		return false
	}

	return true
}

// toString returns value suitable to insert as an argument
// if value if a float where decimal part is zero - convert to int
func toString(rw interface{}) string {
	var sv = rw
	if fv, ok := rw.(float64); ok {
		_, frac := math.Modf(fv)
		if frac == 0 {
			sv = int(fv)
		}
	}

	return fmt.Sprintf("%v", sv)
}

func toJSON(v interface{}) string {
	bytes, _ := json.Marshal(v)
	return string(bytes)
}

// Throttle implements rate limiting based on sliding time window
type Throttle struct {
	limit     int
	timeFrame time.Duration
	queue     []time.Time
}

// InfiniteLimit is a constant that represents an absence of any limits.
const InfiniteLimit = 0

// NewThrottle creates Throttle with following notation: not more than X executions per time period
// e.g. not more than 300 calls per 1 minute
func NewThrottle(limit int, perTimeFrame time.Duration) *Throttle {
	return &Throttle{limit: limit, timeFrame: perTimeFrame, queue: make([]time.Time, 0)}
}

func (t *Throttle) cleanOld() {
	for _, callTime := range t.queue {

		timeSince := time.Since(callTime)
		if timeSince <= t.timeFrame {
			break
		} // queue is ordered, so no point to proceed

		t.queue = t.queue[1:]
	} // clean up top callTimes older than frame
}

// RunOrPause should be added to any throttled operation
// so it either runs without interruption or waits for next time frame if current time frame call limit is exceeded
func (t *Throttle) RunOrPause() {
	if t.limit == InfiniteLimit {
		return
	} // no limit, so exit

	t.cleanOld()

	totalCallsInFrame := len(t.queue)
	limitExceeded := totalCallsInFrame == t.limit

	if limitExceeded {
		eldestCallInFrame := t.queue[0]
		durationSinceEldest := time.Since(eldestCallInFrame)

		remaining := t.timeFrame - durationSinceEldest

		time.Sleep(remaining)

		t.queue = t.queue[1:] // free up space for new item
	}

	t.queue = append(t.queue, time.Now())
}
