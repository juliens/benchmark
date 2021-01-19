package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-echarts/go-echarts/charts"
	vegeta "github.com/tsenart/vegeta/lib"
)

const benchDuration = 30 * time.Second
const timeoutDuration = 30 * time.Second

type profil struct {
	Name    string
	Configs []benchConfig
}

type benchconfigs struct {
	MaxWorkers int
	Duration   string
	duration   time.Duration
	Address    string
	Profils    []profil
}

type benchConfig struct {
	Name string
	Url  string
	DisableHTTP2 bool
}

type bench struct {
	page   *charts.Page
	status string
	conf   benchconfigs
	when time.Time
}

func InitSystem() {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Getting Rlimit ", err)
	}
	fmt.Println(rLimit)
	rLimit.Max = 999999
	rLimit.Cur = 999999
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Setting Rlimit ", err)
	}
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Getting Rlimit ", err)
	}
	fmt.Println("Rlimit Final", rLimit)

}

func InitConfig(config string) (benchconfigs, error) {

	var conf benchconfigs
	_, err := toml.Decode(config, &conf)
	if err != nil {
		return benchconfigs{}, err
	}

	conf.duration = benchDuration
	if len(conf.Duration) > 0 {
		parseDuration, err := time.ParseDuration(conf.Duration)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
		} else {
			conf.duration = parseDuration
		}
	}
	if conf.MaxWorkers == 0 {
		conf.MaxWorkers = 250
	}
	return conf, nil
}

func main() {
	InitSystem()
	config := flag.String("config", "./benchconfhttp.toml", "config file for bench")
	flag.Parse()

	if config == nil || len(*config) == 0 {
		log.Fatal("No config file")
	}

	file, err := ioutil.ReadFile(*config)
	if err != nil {
		log.Fatal(err)
	}
	conf, err := InitConfig(string(file))
	if err != nil {
		log.Fatal(err)
	}

	var benches []*bench
	configsChan := make(chan *bench, 30)
	go func() {
		for b := range configsChan {
			b.status = "Benching..."
			getCharts(b.conf, b.page)
			b.status = "Complete"
		}
	}()

	page := charts.NewPage()
	name := fmt.Sprintf("bench-%d", time.Now().Unix())
	page.InitOpts.PageTitle = name
	b := &bench{
		page:   page,
		status: "PENDING",
		conf:   conf,
		when: time.Now(),
	}
	configsChan <- b
	benches = append(benches, b)

	if len(conf.Address) > 0 {
		fmt.Printf("Listening on %s", conf.Address)
		err := http.ListenAndServe(conf.Address, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/create":
				page := charts.NewPage()
				name := fmt.Sprintf("bench-%d", time.Now().Unix())
				page.InitOpts.PageTitle = name

				configContent := req.URL.Query().Get("config")
				conf, err := InitConfig(configContent)
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(rw, "%v", err)
					return
				}
				b := &bench{
					page:   page,
					status: "PENDING",
					conf:   conf,
					when: time.Now(),
				}
				select {
				case configsChan <- b:
					benches  = append(benches, b)
				default:
					rw.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprint(rw, "Too much bench are pending")
					return
				}
				http.Redirect(rw, req, "/list", http.StatusTemporaryRedirect)
			case "/list":
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `
<!DOCTYPE html>
<!DOCTYPE html>
<html>
<body>
`)

				fmt.Fprint(rw, "<ul>")
				for name, b := range benches {
					fmt.Fprintf(rw, "<li><a href=\"/?name=%d\">%s : %s</a></li>", name, b.when, b.status)
				}
				fmt.Fprint(rw, "</ul>")
				fmt.Fprintf(rw, `

<form action="/create">
	<textarea name="config" style="width:500px;height:500px">%s</textarea>
<input type="submit" />
</form>
				<style>
			.container {display: flex;justify-content: center;align-items: center;}
				.item {margin: auto;}
				</style>
			</body>
			</html>`, file)
			default:
				name := req.URL.Query().Get("name")
				atoi, err := strconv.Atoi(name)
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(rw, "Wrong name %v", err)
				}
				if p := benches[atoi]; p != nil {
					if p.status != "PENDING" {
						p.page.Render(rw)
					} else {
						rw.WriteHeader(http.StatusBadRequest)
						fmt.Fprint(rw, "Still pending")
					}
				} else {
					http.Redirect(rw, req, "/list", http.StatusTemporaryRedirect)
				}
			}

		}))
		if err != nil {
			log.Fatal(err)
		}
	}

}

func getCharts(conf benchconfigs, page *charts.Page) {
	for _, cfg := range conf.Profils {
		fmt.Printf("Start bench of %s\n", cfg.Name)
		getChart(cfg, conf.duration, page)
	}
}

func getChart(config profil, duration time.Duration, page *charts.Page) {
	bar := charts.NewBar()
	bar.SetGlobalOptions(charts.TitleOpts{Title: fmt.Sprintf("Benchmark %s\nduring %s per proxy", config.Name, duration.String())}, charts.ToolboxOpts{Show: true})

	statusBar := charts.NewBar()
	statusBar.SetGlobalOptions(charts.TitleOpts{Title: "Status code"}, charts.ToolboxOpts{Show: false})

	latenciesBar := charts.NewBar()
	latenciesBar.SetGlobalOptions(charts.TitleOpts{Title: fmt.Sprintf("Latencies")}, charts.ToolboxOpts{Show: true})

	bar.AddXAxis([]string{"Proxies"})
	latenciesBar.AddXAxis([]string{"Latency 95", "Latency 99", "Latency mean"})
	page.Add(bar)
	page.Add(latenciesBar)

	var proxies []string
	proxiesStatuses := make(map[string]map[string]int)
	statuscodes := make(map[string]struct{})

	for _, conf := range config.Configs {
		fmt.Printf("Start proxy %s\n", conf.Name)
		time.Sleep(time.Second)
		reqs, statuses, latencies := vegetaCall(conf, duration)
		proxiesStatuses[conf.Name] = statuses

		p95 := float64(latencies.P95) / float64(time.Millisecond)
		p99 := float64(latencies.P99) / float64(time.Millisecond)
		mean := float64(latencies.Mean) / float64(time.Millisecond)

		latenciesBar.AddYAxis(conf.Name, []float64{p95, p99, mean}, charts.LabelTextOpts{
			Show:      true,
			Position:  "top",
			Formatter: "{a}: {c}ms",
		})
		proxies = append(proxies, conf.Name)
		bar.AddYAxis(conf.Name, []float64{math.Round(reqs)}, charts.LabelTextOpts{
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
		var bar []int
		for _, proxy := range proxies {
			bar = append(bar, proxiesStatuses[proxy][code])
		}
		statusBar.AddYAxis(code, bar, charts.LabelTextOpts{
			Show:      true,
			Position:  "top",
			Formatter: "{a}: {c}",
		})
	}
	page.Add(statusBar)

}

func vegetaCall(config benchConfig, duration time.Duration) (float64, map[string]int, vegeta.LatencyMetrics) {
	cfg := &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = cfg

	ticker := time.NewTicker(time.Second)
	start := time.Now()
	for range ticker.C {
		resp, err := http.Get(config.Url)
		if resp != nil && resp.StatusCode == 200 {
			break
		}
		if time.Since(start) > timeoutDuration {
			fmt.Println(resp, err)
			return 0, map[string]int{"Timeout": 1}, vegeta.LatencyMetrics{}
		}
	}
	ticker.Stop()
	rate := vegeta.Rate{Freq: 0, Per: 0}
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    config.Url,
	})

	attacker := vegeta.NewAttacker(vegeta.TLSConfig(cfg), vegeta.MaxWorkers(500), vegeta.HTTP2(!config.DisableHTTP2))

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, rate, duration, "Big Bang!") {
		metrics.Add(res)
	}
	metrics.Close()
	if len(metrics.Errors) > 0 {
		fmt.Fprint(os.Stderr, metrics.Errors)
	}

	fmt.Println(metrics.Rate)
	return metrics.Rate, metrics.StatusCodes, metrics.Latencies
}
