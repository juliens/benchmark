package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
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
	Duration string
	Address  string
	Profils  []profil
}

type benchConfig struct {
	Name string
	Url  string
}

func main() {

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

	config := flag.String("config", "./benchconfhttp.toml", "config file for bench")
	flag.Parse()

	page := charts.NewPage()
	page.InitOpts.PageTitle = "ReverseProxy benchmark"
	if config == nil || len(*config) == 0 {
		log.Fatal("No config file")
	}

	var conf benchconfigs
	_, err = toml.DecodeFile(*config, &conf)
	if err != nil {
		log.Fatal(err)
	}
	duration := benchDuration
	if len(conf.Duration) > 0 {
		parseDuration, err := time.ParseDuration(conf.Duration)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
		} else {
			duration = parseDuration
		}
	}

	fmt.Println("Start benchmarking")
	go func() {
		for _, cfg := range conf.Profils {
			fmt.Printf("Start bench of %s\n", cfg.Name)
			getChart(cfg, duration, page)
		}
	}()

	if len(conf.Address) > 0 {
		fmt.Printf("Listening on %s", conf.Address)
		err := http.ListenAndServe(conf.Address, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			page.Render(writer)
		}))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		page.Render(os.Stdout)
	}

}

func getChart(config profil, duration time.Duration, page *charts.Page) {
	bar := charts.NewBar()
	bar.SetGlobalOptions(charts.TitleOpts{Title: fmt.Sprintf("Benchmark %s\nduring %s per proxy", config.Name, duration.String())}, charts.ToolboxOpts{Show: false})


	statusBar := charts.NewBar()
	statusBar.SetGlobalOptions(charts.TitleOpts{Title: "Status code"}, charts.ToolboxOpts{Show: false})

	bar.AddXAxis([]string{"Proxies"})


	page.Add(bar)

	var proxies []string
	proxiesStatuses := make(map[string]map[string]int)
	statuscodes := make(map[string]struct{})
	for _, conf := range config.Configs {
		fmt.Printf("Start proxy %s\n", conf.Name)
		time.Sleep(time.Second)
		reqs, statuses := vegetaCall(conf, duration)
		proxiesStatuses[conf.Name] = statuses

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

func vegetaCall(config benchConfig, duration time.Duration) (float64, map[string]int) {
	cfg := &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = cfg

	ticker := time.NewTicker(time.Second)
	start := time.Now()
	for range ticker.C {
		resp, err := http.Get(config.Url)
		if resp !=nil && resp.StatusCode == 200 {
			break
		}
		if time.Since(start) > timeoutDuration {
			fmt.Println(resp, err)
			return 0, map[string]int{"Timeout":1}
		}
	}
	ticker.Stop()
	rate := vegeta.Rate{Freq: 0, Per: 0}
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    config.Url,
	})


	attacker := vegeta.NewAttacker(vegeta.TLSConfig(cfg), vegeta.MaxWorkers(250))

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, rate, duration, "Big Bang!") {
		metrics.Add(res)
	}
	metrics.Close()
	if len(metrics.Errors) > 0 {
		fmt.Fprint(os.Stderr, metrics.Errors)
	}

	fmt.Println(metrics.Rate)
	return metrics.Rate, metrics.StatusCodes
}
