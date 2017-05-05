package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
)

type Reporter interface {
	Report(result TestResult)
	Flush()
}

type ConsoleReporter struct {
	ExitCode int

	total  int
	failed int
}

func (r *ConsoleReporter) Report(result TestResult) {
	r.total = r.total + 1

	if result.Skipped {
		r.reportSkipped(result)
		return
	}

	if result.Error != nil {
		r.failed = r.failed + 1
		r.reportError(result)
	} else {
		r.reportSuccess(result)
	}
}

func (r ConsoleReporter) reportSuccess(result TestResult) {
	c := color.New(color.FgGreen).Add(color.Bold)
	fmt.Printf("[")
	c.Print("PASSED")
	fmt.Printf("]  %s - %s \t%s\n", result.Suite.Name, result.Case.Name, result.Duration)
}

func (r ConsoleReporter) reportSkipped(result TestResult) {
	c := color.New(color.FgYellow).Add(color.Bold)
	fmt.Printf("[")
	c.Print("SKIPPED")
	fmt.Printf("] %s - %s", result.Suite.Name, result.Case.Name)
	if result.SkippedMsg != "" {
		reasonColor := color.New(color.FgMagenta)
		reasonColor.Printf("\t (%s)", result.SkippedMsg)
	}

	fmt.Printf("\n")
}

func (r ConsoleReporter) reportError(result TestResult) {
	c := color.New(color.FgRed).Add(color.Bold)
	fmt.Printf("[")
	c.Print("FAILED")
	fmt.Printf("]  %s - %s \n", result.Suite.Name, result.Case.Name)
	lines := strings.Split(result.Error.Cause.Error(), "\n")

	for _, line := range lines {
		fmt.Printf("\t\t%s \n", line)
	}
}

func (r ConsoleReporter) Flush() {
	fmt.Println("\nFinished")
	fmt.Println("--------------------")

	coler := color.New(color.FgGreen).Add(color.Bold)
	if r.failed != 0 {
		coler = color.New(color.FgRed).Add(color.Bold)
	}
	coler.Printf("%v tests, %v failures\n", r.total, r.failed)
}

// NewConsoleReporter returns new instance of console reporter
func NewConsoleReporter() Reporter {
	return &ConsoleReporter{ExitCode: 0}
}

// JUnitXMLReporter produces separate xml file for each test sute
type JUnitXMLReporter struct {
	// output directory
	OutPath string

	// current suite
	// when suite is being changed, flush previous one
	suite *suite
}

type suite struct {
	XMLName     string  `xml:"testsuite"`
	ID          int     `xml:"id,attr"`
	Name        string  `xml:"name,attr"`
	PackageName string  `xml:"package,attr"`
	TimeStamp   string  `xml:"timestamp,attr"`
	Time        float64 `xml:"time,attr"`
	HostName    string  `xml:"hostname,attr"`

	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Errors   int `xml:"errors,attr"`

	Properties properties `xml:"properties"`
	Cases      []tc       `xml:"testcase"`

	SystemOut string `xml:"system-out"`
	SystemErr string `xml:"system-err"`
}

type properties struct {
}

type tc struct {
	Name      string   `xml:"name,attr"`
	ClassName string   `xml:"classname,attr"`
	Time      float64  `xml:"time,attr"`
	Failure   *failure `xml:"failure,omitempty"`
	Skipped   *skipped `xml:"skipped,omitempty"`
}

type failure struct {
	// not clear what type is but it's required
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Details string `xml:",chardata"`
}

type skipped struct {
	Message string `xml:"message,attr"`
}

func (r *JUnitXMLReporter) Report(result TestResult) {
	if r.suite == nil {
		r.suite = newSuite(result)
	}

	if r.suite.Name != result.Suite.Name {
		r.flushSuite()
		r.suite = newSuite(result)
	}

	testCase := tc{Name: result.Case.Name, ClassName: r.suite.PackageName + "." + result.Suite.Name, Time: result.Duration.Seconds()}
	if result.Error != nil {
		testCase.Failure = &failure{Type: "FailedExpectation", Message: result.Error.Cause.Error()}
		testCase.Failure.Details = result.Error.Resp.ToString()
		r.suite.Failures = r.suite.Failures + 1
	}

	if result.Skipped {
		testCase.Skipped = &skipped{}
		if result.SkippedMsg != "" {
			testCase.Skipped.Message = result.SkippedMsg
		}
	}

	r.suite.Tests = r.suite.Tests + 1
	r.suite.ID = r.suite.ID + 1
	r.suite.Time = r.suite.Time + result.Duration.Seconds()
	r.suite.Cases = append(r.suite.Cases, testCase)
}

func (r JUnitXMLReporter) flushSuite() {
	if r.suite == nil {
		return
	}
	fileName := r.suite.PackageName + "." + r.suite.Name + ".xml"
	fp := filepath.Join(r.OutPath, fileName)
	err := os.MkdirAll(r.OutPath, 0777)
	if err != nil {
		panic(err)
	}
	f, err := os.Create(fp)
	if err != nil {
		panic(err)
	}

	data, err := xml.Marshal(r.suite)
	if err != nil {
		panic(err)
	}

	f.Write(data)
}

func newSuite(result TestResult) *suite {
	return &suite{
		ID:          0,
		Name:        result.Suite.Name,
		PackageName: strings.Replace(filepath.ToSlash(result.Suite.Dir), "/", ".", -1),
		TimeStamp:   time.Now().UTC().Format("2006-01-02T15:04:05"),
		HostName:    "localhost",
	}
}

func (r JUnitXMLReporter) Flush() {
	r.flushSuite()
}

func NewJUnitReporter(outdir string) Reporter {
	return &JUnitXMLReporter{OutPath: outdir}
}

// MultiReporter broadcasts events to another reporters.
type MultiReporter struct {
	Reporters []Reporter
}

func (r MultiReporter) Report(result TestResult) {
	for _, reporter := range r.Reporters {
		reporter.Report(result)
	}
}

func (r MultiReporter) Flush() {
	for _, reporter := range r.Reporters {
		reporter.Flush()
	}
}

// NewMultiReporter creates new reporter that broadcasts events to another reporters.
func NewMultiReporter(reporters ...Reporter) Reporter {
	return &MultiReporter{Reporters: reporters}
}
