package prometheus

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/afero"
)

// WriteConfig creates Prometheus configuration (`/etc/prometheus/prometheus.yml`) and rules (`/etc/prometheus/alert.rules`) files.
func WriteConfig(scrapes map[string]Scrape, alerts map[string]Alert) {
	FS.MkdirAll("/etc/prometheus", 0755)
	gc := GetGlobalConfig()
	sc := GetScrapeConfig(scrapes)
	rc := GetRemoteConfig()
	amc := GetAlertManagerConfig()
	ruleFiles := ""
	if len(alerts) > 0 {
		logPrintf("Writing to alert.rules")
		ruleFiles = "rule_files:\n  - 'alert.rules'"
		afero.WriteFile(FS, "/etc/prometheus/alert.rules", []byte(GetAlertConfig(alerts)), 0644)
	}

	config := ""
	for _, c := range []string{gc, sc, rc, ruleFiles, amc} {
		if len(c) > 0 {
			if len(config) == 0 {
				config = c
			} else {
				config += "\n" + c
			}
		}
	}

	logPrintf("Writing to prometheus.yml")
	afero.WriteFile(FS, "/etc/prometheus/prometheus.yml", []byte(config), 0644)
}

// GetRemoteConfig returns remote_write and remote_read configs
func GetRemoteConfig() string {
	rw := getDataFromEnvVars("REMOTE_WRITE")
	config := getConfigSectionArray("remote_write", rw)

	rr := getDataFromEnvVars("REMOTE_READ")
	if rrc := getConfigSectionArray("remote_read", rr); len(rrc) != 0 {
		if len(config) == 0 {
			config = rrc
		} else {
			config += "\n" + rrc
		}
	}

	return config
}

// GetGlobalConfig returns global section of the configuration
func GetGlobalConfig() string {
	data := getDataFromEnvVars("GLOBAL")
	return getConfigSection("global", data)
}

// GetAlertManagerConfig returns alerting section of the configuration
func GetAlertManagerConfig() string {
	alertmanagerURL := os.Getenv("ARG_ALERTMANAGER_URL")
	url, err := url.Parse(alertmanagerURL)
	if err != nil {
		return ""
	}
	templateStr := `alerting:
  alertmanagers:
  - scheme: {{ .Scheme }}
    static_configs:
    - targets:
      - {{ .Host }}`
	tmpl, _ := template.New("").Parse(templateStr)

	b := new(bytes.Buffer)
	tmpl.Execute(b, url)
	return b.String()

}

// GetScrapeConfig returns scrapes section of the configuration
func GetScrapeConfig(scrapes map[string]Scrape) string {
	config := getScrapeConfigFromMap(scrapes)

	if dirConfig := getScrapeConfigFromDir(); len(dirConfig) > 0 {
		if len(config) == 0 {
			config = dirConfig
		} else {
			config += "\n" + dirConfig
		}
	}

	if len(config) > 0 {
		config = fmt.Sprintf("scrape_configs:\n%s", config)
	}
	return config
}

func getDataFromEnvVars(prefix string) map[string]map[string]string {
	data := map[string]map[string]string{}
	for _, e := range os.Environ() {
		if key, value := getArgFromEnv(e, prefix); len(key) > 0 {
			realKey := key
			subKey := ""
			if strings.Contains(key, "-") {
				keys := strings.Split(key, "-")
				realKey = keys[0]
				subKey = keys[1]
			}
			if _, ok := data[realKey]; !ok {
				data[realKey] = map[string]string{}
			}
			subData := data[realKey]
			subData[subKey] = value
		}
	}
	return data
}

func getScrapeConfigFromDir() string {
	dir := "/run/secrets/"
	if len(os.Getenv("CONFIGS_DIR")) > 0 {
		dir = os.Getenv("CONFIGS_DIR")
	}
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}

	config := ""
	if files, err := afero.ReadDir(FS, dir); err == nil {
		for _, file := range files {
			if !strings.HasPrefix(file.Name(), "scrape_") {
				continue
			}
			if content, err := afero.ReadFile(FS, dir+file.Name()); err == nil {
				contentStr := string(content)
				if len(contentStr) > 0 {
					if len(config) > 0 {
						config += "\n" + contentStr
					} else {
						config = contentStr
					}
				}

			}
		}
	}
	return config
}

func getScrapeConfigFromMap(scrapes map[string]Scrape) string {
	if len(scrapes) != 0 {
		templateString := `
{{- range . }}
  - job_name: "{{.ServiceName}}"
    metrics_path: {{if .MetricsPath}}{{.MetricsPath}}{{else}}/metrics{{end}}
{{- if .ScrapeType }}
    {{.ScrapeType}}:
      - targets:
        - {{.ServiceName}}:{{- .ScrapePort}}
{{- else }}
    dns_sd_configs:
      - names: ["tasks.{{.ServiceName}}"]
        type: A
        port: {{.ScrapePort}}{{end}}
{{- end}}`
		tmpl, _ := template.New("").Parse(templateString)
		var b bytes.Buffer
		tmpl.Execute(&b, scrapes)
		return strings.TrimPrefix(b.String(), "\n")

	}
	return ""
}

func getConfigSection(section string, data map[string]map[string]string) string {
	if len(data) == 0 {
		return ""
	}
	config := fmt.Sprintf(`%s:`, section)
	for key, values := range data {
		if len(values[""]) > 0 {
			config += "\n  " + key + ": " + values[""]
		} else {
			config += "\n  " + key + ":"
			for subKey, value := range values {
				config += "\n    " + subKey + ": " + value
			}
		}
	}
	return config
}

func getConfigSectionArray(section string, data map[string]map[string]string) string {
	if len(data) == 0 {
		return ""
	}
	config := fmt.Sprintf(`%s:`, section)
	for key, values := range data {
		if len(values[""]) > 0 {
			if config == fmt.Sprintf(`%s:`, section) {
				config += "\n  - " + key + ": " + values[""]
			} else {
				config += "\n    " + key + ": " + values[""]
			}
		}
	}
	return config
}
