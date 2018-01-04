package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
)

// Reporter is used to write down test results using particular formats and outputs
type Reporter interface {
	Init()

	Report(result []TestResult)

	Flush()
}

// ConsoleReporter is a simple reporter that outputs everything to the StdOut.
type ConsoleReporter struct {
	ExitCode   int
	Writer     io.Writer
	IntendSize int

	execFrame *TimeFrame

	// to prevent collisions while working with StdOut
	ioMutex *sync.Mutex

	total   int
	failed  int
	skipped int
}

func (r *ConsoleReporter) Init() {
	r.execFrame = &TimeFrame{Start: time.Now()}
}

const (
	DefaultIntendSize = 4
	CaretIcon         = "\u2514" // ↳
)

type Status struct {
	Icon  string
	Label string
	Color color.Attribute
}

const (
	OutputLabel int = iota
	OutputIcon
)

var (
	StatusPassed  Status = Status{Icon: "\u221A", Label: "PASSED", Color: color.FgGreen} // ✔
	StatusFailed  Status = Status{Icon: "\u00D7", Label: "FAILED", Color: color.FgRed}   // ✘
	StatusSkipped Status = Status{Icon: "", Label: "SKIPPED", Color: color.FgYellow}
)

func (r *ConsoleReporter) StartLine() {
	r.Writer.Write([]byte("\n"))
	r.Writer.Write([]byte(strings.Repeat(" ", r.IntendSize)))
}

func (r *ConsoleReporter) Intend() {
	r.IntendSize = r.IntendSize + DefaultIntendSize
}

func (r *ConsoleReporter) Unintend() {
	r.IntendSize = r.IntendSize - DefaultIntendSize
}

func (r *ConsoleReporter) Report(results []TestResult) {
	r.ioMutex.Lock()

	if len(results) == 0 {
		r.ioMutex.Unlock()
		return
	}

	// suite
	suite := results[0].Suite

	r.StartLine()
	r.Write(suite.FullName())

	for _, result := range results {

		r.total = r.total + 1

		r.Intend()

		r.StartLine()
		r.Write(CaretIcon).Write(" ")

		if result.Skipped {
			r.WriteStatus(StatusSkipped, OutputLabel).Write(" ").Write(result.Case.Name)
			r.Write(" (").Write(result.SkippedMsg).Write(")")
			r.skipped = r.skipped + 1
			r.Unintend()

			continue
		}

		if result.hasError() {
			r.WriteStatus(StatusFailed, OutputLabel)
			r.failed = r.failed + 1
		} else {
			r.WriteStatus(StatusPassed, OutputLabel)
		}

		r.Write(" ").Write(result.Case.Name)
		r.Write(" [").Write(result.ExecFrame.Duration().Round(time.Millisecond)).Write("]")

		for _, trace := range result.Traces {
			if trace.Req == nil {
				// fmt.Println("REQ IS NIL!!!") // TODO
				continue
			}

			r.Intend()

			r.StartLine()
			r.Write(trace.Req.Method).Write(" ").Write(trace.Req.URL).Write(" [").Write(trace.ExecFrame.Duration().Round(time.Millisecond)).Write("]")

			for exp, failed := range trace.ExpDesc {
				r.Intend()
				r.StartLine()

				if failed {
					r.WriteStatus(StatusFailed, OutputIcon)
				} else {
					r.WriteStatus(StatusPassed, OutputIcon)
				}

				r.Write(" ").WriteDimmed(exp)

				r.Unintend()
			}

			r.StartLine()
			r.Unintend()
		}

		r.Unintend()

	}

	r.StartLine()

	r.ioMutex.Unlock()
}

func (r ConsoleReporter) WriteDimmed(content interface{}) ConsoleReporter {
	c := color.New(color.FgHiBlack)
	c.Print(content)
	return r
}

func (r ConsoleReporter) Write(content interface{}) ConsoleReporter {
	r.Writer.Write([]byte(fmt.Sprintf("%v", content)))
	return r
}

func (r ConsoleReporter) WriteStatus(status Status, output int) ConsoleReporter {
	c := color.New(status.Color).Add(color.Bold)
	var val string

	if output == OutputIcon {
		val = status.Icon
	}

	if output == OutputLabel {
		val = status.Label
	}

	c.Print(val)
	return r
}

// func (r ConsoleReporter) reportSuccess(result TestResult) {
// 	r.WriteStatus(StatusPassed, OutputLabel)

// 	fmt.Printf("]  %s - %s \t%s\n", result.Suite.FullName(), result.Case.Name, result.ExecFrame.Duration())

// 	for _, trace := range result.Traces {
// 		fmt.Println(string(trace.CallNum) + " -----------")
// 		for _, exp := range trace.ExpDesc {
// 			fmt.Print("\t\t")
// 			c.Print("✔ ")
// 			fmt.Printf("%s\n", exp)
// 		}
// 	}
// }

// func (r ConsoleReporter) reportSkipped(result TestResult) {
// 	c := color.New(color.FgYellow).Add(color.Bold)
// 	fmt.Printf("[")
// 	c.Print("SKIPPED")
// 	fmt.Printf("] %s - %s", result.Suite.FullName(), result.Case.Name)
// 	if result.SkippedMsg != "" {
// 		reasonColor := color.New(color.FgMagenta)
// 		reasonColor.Printf("\t (%s)", result.SkippedMsg)
// 	}

// 	fmt.Printf("\n")
// }

// func (r ConsoleReporter) reportError(result TestResult) {
// 	c := color.New(color.FgRed).Add(color.Bold)
// 	fmt.Printf("[")
// 	c.Print("FAILED")
// 	fmt.Printf("]  %s - %s - on call %d \n", result.Suite.FullName(), result.Case.Name, result.Trace.CallNum+1)

// 	for _, trace := range result.Traces {
// 		fmt.Println(string(trace.CallNum) + " -----------")
// 		for _, exp := range trace.ExpDesc {
// 			fmt.Print("\t\t")
// 			c.Print("✔ ")
// 			fmt.Printf("%s\n", exp)
// 		}
// 	}

// 	lines := strings.Split(result.Error(), "\n")

// 	for _, line := range lines {
// 		fmt.Printf("\t\t✘ %s \n", line)
// 	}
// }

func (r ConsoleReporter) Flush() {
	r.ioMutex.Lock()
	r.execFrame.End = time.Now()

	overall := "PASSED"
	if r.failed != 0 {
		overall = "FAILED"
	}

	fmt.Println()
	fmt.Println("Test Run Summary")
	fmt.Println("-------------------------------")

	w := tabwriter.NewWriter(os.Stdout, 4, 2, 1, ' ', tabwriter.AlignRight)

	fmt.Fprintf(w, "Overall result:\t %s\n", overall)

	fmt.Fprintf(w, "Test count:\t %d\n", r.total)

	fmt.Fprintf(w, "Passed:\t %d \n", r.total-r.failed-r.skipped)
	fmt.Fprintf(w, "Failed:\t %d \n", r.failed)
	fmt.Fprintf(w, "Skipped:\t %d \n", r.skipped)

	start := r.execFrame.Start
	end := r.execFrame.End

	fmt.Fprintf(w, "Start time:\t %s\n", start)
	fmt.Fprintf(w, "End time:\t %s\n", end)
	fmt.Fprintf(w, "Duration:\t %s\n", end.Sub(start).Round(time.Microsecond))

	w.Flush()
	fmt.Println()
	r.ioMutex.Unlock()
}

// NewConsoleReporter returns new instance of console reporter
func NewConsoleReporter() Reporter {
	return &ConsoleReporter{ExitCode: 0, ioMutex: &sync.Mutex{}, Writer: os.Stdout}
}

// JUnitXMLReporter produces separate xml file for each test sute
type JUnitXMLReporter struct {
	// output directory
	OutPath string
}

func (r *JUnitXMLReporter) Init() {
	// nothing to do here
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
	Skipped  int `xml:"skipped,attr"`

	Properties properties `xml:"properties"`
	Cases      []tc       `xml:"testcase"`

	SystemOut string `xml:"system-out"`
	SystemErr string `xml:"system-err"`

	fullName string
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

func (r *JUnitXMLReporter) Report(results []TestResult) {

	var suiteResult *suite
	var suiteTimeFrame TimeFrame
	for _, result := range results {

		if suiteResult == nil {
			suiteResult = &suite{
				ID:          0,
				Name:        result.Suite.Name,
				PackageName: result.Suite.PackageName(),
				TimeStamp:   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
				fullName:    result.Suite.FullName(),
				HostName:    "localhost",
			}

			suiteTimeFrame = result.ExecFrame
		}

		testCase := tc{
			Name:      result.Case.Name,
			ClassName: suiteResult.fullName,
			Time:      result.ExecFrame.Duration().Seconds(),
		}

		if result.Error != nil {
			errType := "FailedExpectation"
			errMsg := result.Error()
			errDetails := fmt.Sprintf("%s\n\n%s", errMsg, "") // TODO

			testCase.Failure = &failure{
				Type:    errType,
				Message: errMsg,
				Details: errDetails,
			}

			suiteResult.Failures = suiteResult.Failures + 1
		}

		if result.Skipped {
			suiteResult.Skipped = suiteResult.Skipped + 1
			testCase.Skipped = &skipped{Message: result.SkippedMsg}
		}

		suiteResult.Tests = suiteResult.Tests + 1
		suiteResult.ID = suiteResult.ID + 1
		suiteResult.Cases = append(suiteResult.Cases, testCase)

		suiteTimeFrame.Extend(result.ExecFrame)
		suiteResult.Time = suiteTimeFrame.Duration().Seconds()
	}

	r.flushSuite(suiteResult)
}

func (r JUnitXMLReporter) flushSuite(suite *suite) {
	if suite == nil {
		return
	}

	fileName := suite.fullName + ".xml"
	fp := filepath.Join(r.OutPath, fileName)
	err := os.MkdirAll(r.OutPath, 0777)
	if err != nil {
		panic(err)
	}
	f, err := os.Create(fp)
	if err != nil {
		panic(err)
	}

	data, err := xml.Marshal(suite)
	if err != nil {
		panic(err)
	}

	f.Write(data)
}

func (r JUnitXMLReporter) Flush() {

}

func NewJUnitReporter(outdir string) Reporter {
	return &JUnitXMLReporter{OutPath: outdir}
}

// MultiReporter broadcasts events to another reporters.
type MultiReporter struct {
	Reporters []Reporter
}

func (r MultiReporter) Report(results []TestResult) {
	for _, reporter := range r.Reporters {
		reporter.Report(results)
	}
}

func (r MultiReporter) Init() {
	for _, reporter := range r.Reporters {
		reporter.Init()
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
