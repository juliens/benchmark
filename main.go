package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-echarts/go-echarts/charts"
	"github.com/rakyll/hey/requester"
)

const benchDuration = 30 * time.Second

type benchConfig struct {
	Url    string
	cmdCPU []string
}

func main() {
	config := flag.String("config", "./benchconf.toml", "config file for bench")
	output := flag.String("output", "./output.html", "output file path")
	flag.Parse()
	page := charts.NewPage()
	page.InitOpts.PageTitle = "ReverseProxy benchmark"
	bar := charts.NewBar()
	bar.SetGlobalOptions(charts.TitleOpts{Title: fmt.Sprintf("Benchmark during %s per proxy", benchDuration.String())}, charts.ToolboxOpts{Show: false})

	statusBar := charts.NewBar()
	statusBar.SetGlobalOptions(charts.TitleOpts{Title: "Status code"}, charts.ToolboxOpts{Show: false})

	var tests map[string]benchConfig
	if config == nil || len(*config)==0 {
		log.Fatal("No config file")
	}
	_, err := toml.DecodeFile(*config, &tests)
	if err != nil {
		log.Fatal(err)
	}
	// tests := map[string]benchConfig{
	// 	"Simple TLS": {
	// 		url:    "https://10.1.11.3/bench",
	// 		cmdCPU: []string{`docker-compose`, `exec`, `-T`, `simple`, `sh`, `-c`, `mpstat 1 5 | grep Average |awk '/^Average/ {print int($3)}'|tail -n 1`},
	// 	},
	// 	"Traefik TLS": {
	// 		url:    "https://10.1.11.4/bench",
	// 		cmdCPU: []string{`docker-compose`, `exec`, `-T`, `traefik`, `sh`, `-c`, `mpstat 1 5 | grep Average |awk '/^Average/ {print int($3)}'|tail -n 1`},
	// 	},
	// 	"HaProxy TLS": {
	// 		url:    "https://10.1.11.5/bench",
	// 		cmdCPU: []string{`docker-compose`, `exec`, `-T`, `haproxy`, `sh`, `-c`, `mpstat 1 5 | grep Average |awk '/^Average/ {print int($3)}'|tail -n 1`},
	// 	},
	// }
	fmt.Println(tests)
	bar.AddXAxis([]string{"Proxies"})
	var proxies []string
	proxiesStatuses := make(map[string]map[string]int)
	statuscodes := make(map[string]struct{})
	for proxy, url := range tests {
		time.Sleep(time.Second)
		fmt.Println(url)
		reqs, statuses, cpus := hey(url)
		fmt.Println(cpus)
		proxiesStatuses[proxy] = statuses

		proxies = append(proxies, proxy)
		bar.AddYAxis(proxy, []float64{math.Round(reqs)}, charts.LabelTextOpts{
			Show:      true,
			Position:  "top",
			Formatter: "{a}: {c} req/s",
		})

		for code := range statuses {
			statuscodes[code] = struct{}{}
		}

	}

	statusBar.AddXAxis(proxies)
	for code := range statuscodes {
		bar := []int{}
		for _, proxy := range proxies {
			bar = append(bar, proxiesStatuses[proxy][code])
		}
		statusBar.AddYAxis(code, bar, charts.LabelTextOpts{
			Show:      true,
			Position:  "top",
			Formatter: "{a}: {c}",
		})
	}

	create, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}

	page.Add(bar)
	page.Add(statusBar)
	page.Render(create)

}

func hey(config benchConfig) (float64, map[string]int, []float64) {
	req, err := http.NewRequest(http.MethodGet, config.Url, http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/html")

	writer := &bytes.Buffer{}
	w := &requester.Work{
		Request: req,
		N:       math.MaxInt32,
		C:       250,
		Timeout: 20,
		// DisableKeepAlives:  *disableKeepAlives,
		Output: "csv",
		Writer: writer,
	}
	w.Init()
	var cpus []float64
	go func() {
		timer := time.NewTimer(benchDuration)
		// ticker := time.NewTicker(benchDuration/100)

		for {
			select {
			case <-timer.C:
				w.Stop()
				return
				// case <-ticker.C:
				// 	cmd := exec.Command(config.cmdCPU[0], config.cmdCPU[1:]...)
				// 	var out bytes.Buffer
				// 	cmd.Stdout = &out
				// 	cmd.Stderr = &out
				// 	cmd.Run()
				// 	cpuStat, _ := strconv.ParseFloat(strings.TrimSpace(out.String()), 64)
				// 	cpus = append(cpus, cpuStat)
			}
		}

	}()

	w.Run()

	reader := csv.NewReader(writer)
	var count float64
	var last []string
	statuses := map[string]int{}
	reader.Read()
	first := true
	var firstOffset float64
	for {
		record, err := reader.Read()

		if first {
			if len(record) < 8 {
				fmt.Printf("Error %d", len(record))
				return 0, nil, nil
			}
			var err error
			firstOffset, err = strconv.ParseFloat(record[7], 64)
			if err != nil {
				log.Fatal(err)
			}
			first = false
		}
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatal(err)
		}
		statuses[record[6]]++
		last = record
		count++
	}
	offset, err := strconv.ParseFloat(last[7], 64)
	return count / (offset - firstOffset), statuses, cpus
	// break
	// fmt.Println(reader.Read())
	// fmt.Println(reader.Read())
}
