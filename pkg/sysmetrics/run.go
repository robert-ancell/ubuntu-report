package sysmetrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/ubuntu-report/internal/metrics"
	"github.com/ubuntu/ubuntu-report/internal/sender"
	"github.com/ubuntu/ubuntu-report/internal/utils"
)

// optOutJSON is the data sent in case of Opt-Out choice
const optOutJSON = `{"OptOut": true}`

func metricsCollect(m metrics.Metrics) ([]byte, error) {
	data, err := m.Collect()
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't collect system minimal info")
	}

	log.Debug("pretty print format the collected data to the user")
	h := json.RawMessage(data)
	return json.MarshalIndent(&h, "", "  ")
}

func metricsReport(m metrics.Metrics, r ReportType, alwaysReport bool, baseURL string, reportBasePath string, in io.Reader, out io.Writer) error {
	distro, version, err := m.GetIDS()
	if err != nil {
		return errors.Wrapf(err, "couldn't get mandatory information")
	}

	reportP, err := utils.ReportPath(distro, version, reportBasePath)
	if err != nil {
		return errors.Wrapf(err, "couldn't get where to save reported metrics on disk")
	}
	if _, err := os.Stat(reportP); !os.IsNotExist(err) {
		log.Infof("previous report found in %s", reportP)
		if !alwaysReport {
			return errors.Errorf("metrics from this machine have already been reported and can be found in: %s", reportP)
		}
		log.Debug("ignore previous report requested")
	}

	var data []byte
	if r != ReportOptOut {
		if data, err = metricsCollect(m); err != nil {
			return errors.Wrapf(err, "couldn't collect system minimal info and format it")
		}
	}

	sendMetrics := true
	if r == ReportInteractive {
		fmt.Fprintln(out, "This is the result of hardware and optional installer/upgrader that we collected:")
		fmt.Fprintln(out, string(data))

		validAnswer := false
		scanner := bufio.NewScanner(in)
		for validAnswer != true {
			fmt.Fprintf(out, "Do you agree to report this? [y (send metrics)/n (send opt out message)/Q (quit)] ")
			if !scanner.Scan() {
				log.Info("programm interrupted")
				return nil
			}
			text := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if text == "n" || text == "no" {
				log.Debug("sending report was denied")
				sendMetrics = false
				validAnswer = true
			} else if text == "y" || text == "yes" {
				log.Debug("sending report was accepted")
				sendMetrics = true
				validAnswer = true
			} else if text == "q" || text == "quit" || text == "" {
				return nil
			}
			if validAnswer != true {
				log.Error("we didn't understand your answer")
			}
		}
	} else if r == ReportAuto {
		log.Debug("auto report requested")
		sendMetrics = true
	} else {
		log.Debug("opt-out report requested")
		sendMetrics = false
	}

	// erase potential collected data
	if !sendMetrics {
		data = []byte(optOutJSON)
	}

	if baseURL == "" {
		baseURL = sender.BaseURL
	}
	u, err := sender.GetURL(baseURL, distro, version)
	if err != nil {
		return errors.Wrapf(err, "report destination url is invalid")
	}
	if err := sender.Send(u, data); err != nil {
		return errors.Wrapf(err, "data were not delivered successfully to metrics server")
	}

	return saveMetrics(reportP, data)
}

func saveMetrics(p string, data []byte) error {
	log.Debugf("save sent metrics to %s", p)

	d := filepath.Dir(p)
	if err := os.MkdirAll(d, 0700); err != nil {
		return errors.Wrap(err, "couldn't create parent directory to save reported metrics")
	}

	if err := ioutil.WriteFile(p, data, 0666); err != nil {
		return errors.Wrap(err, "couldn't save reported metrics on disk")
	}

	return nil
}
