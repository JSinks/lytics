package cmds

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
	lytics "github.com/lytics/go-lytics"
	"github.com/urfave/cli"
)

/*
    [watch]
         watch the current folder for .lql, .json files to evaluate
         the .lql query against the data in .json to preview output.

         .lql file name must match the json file name.

         For Example:
            cd /tmp
            ls *.lql       # assume a temp.lql
            cat temp.json  # file of data

         -------
         example:
         -------
			  lytics watch
func (c *Cli) watch() (interface{}, error) {

	l := newLql(c)
	l.start()

	return nil, nil
}
*/
func schemaQueryWatch(c *cli.Context) error {
	if len(c.Args()) == 0 {
		return fmt.Errorf(`expected one arg ( ".")`)
	}
	l := newLql()
	l.start()
	return nil
}

type datafile struct {
	name          string
	lql           string
	data          []url.Values
	checkedRecent bool
	stream        string
}

func (d *datafile) loadJson(of string) {
	by, err := ioutil.ReadFile("./" + of)
	exitIfErr(err, fmt.Sprintf("Could not read json file %v", of))
	l := make([]map[string]interface{}, 0)
	err = json.Unmarshal(MakeJsonList(by), &l)
	exitIfErr(err, "Invalid json file")

	qsargs := make([]url.Values, 0, len(l))
	for _, row := range l {
		qs, err := lytics.FlattenJsonMap(row)
		if err == nil {
			qsargs = append(qsargs, qs)
		} else {
			log.Printf("Could not convert row to qs? %v   %v\n", row, err)
		}
	}
	d.data = qsargs
}

func (d *datafile) loadCsv(of string) {
	f, err := os.Open("./" + of)
	exitIfErr(err, fmt.Sprintf("Could not read csv file %v", of))

	csvr := csv.NewReader(f)
	csvr.TrailingComma = true // allow empty fields
	headers, err := csvr.Read()
	exitIfErr(err, fmt.Sprintf("Could not read csv headers %v", of))

	qsargs := make([]url.Values, 0, 5)
	rowCt := 0
	for {
		row, err := csvr.Read()

		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatalf("could not read csv %v", err)
			continue
		}
		if len(row) != len(headers) {
			log.Fatalf("headers/cols dont match, dropping expected:%d got:%d   vals=%v\n", len(headers), len(row), row)
			continue
		}
		qs := make(url.Values)
		for i, val := range row {
			qs.Set(headers[i], val)
		}
		qsargs = append(qsargs, qs)
		rowCt++
		if rowCt > 5 {
			break
		}
	}

	d.data = qsargs
}

type lql struct {
	files map[string]*datafile
	w     *fsnotify.Watcher
}

func newLql() *lql {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err.Error())
	}
	return &lql{
		w:     watcher,
		files: make(map[string]*datafile),
	}
}

func (l *lql) start() {
	defer l.w.Close()
	done := make(chan bool)
	l.loadFiles()
	l.watch()
	<-done
}

func (l *lql) print(d *datafile) {
	if len(d.data) == 0 {
		log.Printf("No data found for %v \n\n", d.name)
		return
	}
	if len(d.lql) == 0 {
		l.printUsingCurrentQueries(d)
		return
		log.Printf("No lql found for %v \n\n", d.name)
		return
	}

	fmt.Printf("evaluating: %s.lql \n\n", d.name)
	for i, qs := range d.data {
		ent, err := client.GetQueryTest(qs, d.lql)
		if err != nil {
			fmt.Printf("Could not evaluate query/entity: %v \n\tfor-data: %v\n\n", err, qs.Encode())
			continue
		}

		// Output the user response
		out, err := json.MarshalIndent(ent, "", "  ")
		if err == nil {
			fmt.Printf("\n%v\n\n", string(out))
		}
		if i > 1 {
			return
		}
	}

}
func (l *lql) printUsingCurrentQueries(d *datafile) {
	if len(d.data) == 0 {
		log.Printf("No data found for %v \n\n", d.name)
		return
	}

	fmt.Printf("evaluating: %s.json against current queries in your account \n\n", d.name)
	for i, qs := range d.data {

		state, err := json.MarshalIndent(qs, "", "  ")
		if err != nil {
			fmt.Printf("Could not json marshal: %v \n\tfor-data: %v\n\n", err, qs.Encode())
			continue
		}
		// if err == nil {
		// 	fmt.Printf("\n%v\n\n", string(state))
		// }
		params := url.Values{"stream": {d.name}, "state": {string(state)}}

		ent, err := client.GetEntityParams("user", "user_id", "should-never-ever-ever-match-12345", nil, params)
		if err != nil {
			fmt.Printf("Could not evaluate query/entity: %v \n\tfor-data: %v\n\n", err, qs.Encode())
			continue
		}

		// Output the user response
		out, err := json.MarshalIndent(ent, "", "  ")
		if err == nil {
			fmt.Printf("\n%v\n\n", string(out))
		}
		if i > 1 {
			return
		}
	}

}

func (l *lql) verifyLql(d *datafile) error {
	if d.lql != "" {
		ql, err := client.PostQueryValidate(d.lql)
		if err != nil {
			fmt.Printf("ERROR: invalid lql statement\n%+v\n\n%v\n", ql, err)
			return err
		}
		if len(ql) > 0 {
			q := ql[0]
			if q.From != "" {
				d.stream = q.From
			}
		}
	}
	return nil
}

func (l *lql) findRecent(d *datafile) {
	d.checkedRecent = true
	ss, err := client.GetStreams("")
	if err != nil {
		log.Printf("Could not load streams data: %v \n\n", err)
		return
	}
	for _, s := range ss {
		if s.Name == d.name || s.Name == d.stream {
			//fmt.Printf("found data %#v \n\n", s.Recent)
			d.data = s.Recent
		}
	}
}

func (l *lql) handleFile(of string, showOutput bool) {
	if strings.Index(of, ".") < 1 {
		return
	}
	f := strings.ToLower(of)
	name := strings.Split(f, ".")[0]
	df, exists := l.files[name]
	if !exists {
		df = &datafile{name: name}
		l.files[name] = df
	}
	switch {
	case strings.HasSuffix(f, ".lql"):
		//log.Println("handle lql file ", f)
		by, err := ioutil.ReadFile("./" + of)
		exitIfErr(err, fmt.Sprintf("Could not read file %v", of))
		df.lql = string(by)

		// Parse the lql to get stream name
		// and validate the lql syntax
		if err := l.verifyLql(df); err != nil {
			return
		}

		if _, err := os.Stat("./" + name + ".json"); os.IsNotExist(err) {
			// ./name.json does not exist lets use recent
			if !df.checkedRecent {
				l.findRecent(df)
			}
		}

	case strings.HasSuffix(f, ".csv"):
		//log.Println("handle csv file ", f)
		df.loadCsv(of)
	case strings.HasSuffix(f, ".json"):
		//log.Println("handle json file ", f)
		df.loadJson(of)
	default:
		return
	}
	if showOutput {
		l.print(df)
	}
}

func (l *lql) loadFiles() {
	files, _ := ioutil.ReadDir("./")
	for _, f := range files {
		l.handleFile(f.Name(), false)
	}
}

func (l *lql) watch() {

	go func() {
		for {
			select {
			case event := <-l.w.Events:
				//log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					fn := strings.ToLower(event.Name)
					fn = strings.Replace(fn, "./", "", 1)
					//log.Println("modified file:", fn)
					l.handleFile(fn, true)
				}
			case err, ok := <-l.w.Errors:
				if !ok {
					log.Fatal("What, no errors channel")
				} else {
					log.Println("watch error:", err)
				}

			}
		}
	}()

	if err := l.w.Add("./"); err != nil {
		log.Fatal(err)
	}
}

// Convert a slice of bytes into an array by ensuring it is wrapped with []
func MakeJsonList(b []byte) []byte {
	if !bytes.HasPrefix(b, []byte{'['}) {
		b = append([]byte{'['}, b...)
		b = append(b, ']')
	}
	return b
}