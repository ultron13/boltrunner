package jmx

import (
	"bytes"
	"fmt"
	"net/url"
	"text/template"
)

type Params struct {
	TargetURL       string
	VirtualUsers    int
	DurationSeconds int
}

const jmxTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0">
  <hashTree>
    <TestPlan testname="BoltRunner Generated Plan" enabled="true"/>
    <hashTree>
      <ThreadGroup testname="BoltRunner Threads" enabled="true">
        <stringProp name="ThreadGroup.num_threads">{{.VirtualUsers}}</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <stringProp name="ThreadGroup.duration">{{.DurationSeconds}}</stringProp>
        <boolProp name="ThreadGroup.scheduler">true</boolProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController">
          <boolProp name="LoopController.continue_forever">true</boolProp>
          <stringProp name="LoopController.loops">-1</stringProp>
        </elementProp>
      </ThreadGroup>
      <hashTree>
        <HTTPSamplerProxy testname="BoltRunner Request" enabled="true">
          <stringProp name="HTTPSampler.domain">{{.Host}}</stringProp>
          <stringProp name="HTTPSampler.port">{{.Port}}</stringProp>
          <stringProp name="HTTPSampler.protocol">{{.Scheme}}</stringProp>
          <stringProp name="HTTPSampler.path">{{.Path}}</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
        </HTTPSamplerProxy>
        <hashTree/>
        <ResultCollector testname="BoltRunner Results" enabled="true">
          <stringProp name="filename">results.jtl</stringProp>
        </ResultCollector>
        <hashTree/>
      </hashTree>
    </hashTree>
  </hashTree>
</jmeterTestPlan>
`

type templateData struct {
	Params
	Scheme string
	Host   string
	Port   string
	Path   string
}

func Generate(p Params) (string, error) {
	u, err := url.Parse(p.TargetURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid target URL %q", p.TargetURL)
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	path := u.Path
	if path == "" {
		path = "/"
	}

	tmpl, err := template.New("jmx").Parse(jmxTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := templateData{Params: p, Scheme: u.Scheme, Host: u.Hostname(), Port: port, Path: path}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
