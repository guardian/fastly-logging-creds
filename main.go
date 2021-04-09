package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// https://developer.fastly.com/reference/api/logging/s3/
func main() {
	serviceID := flag.String("serviceID", "", "A Fastly Service ID.")
	awsAccessKey := flag.String("awsAccessKey", "", "AWS Access Key for S3 write access to the target bucket.")
	loggingName := flag.String("loggingName", "s3-logs", "Name of your service logging configuration in Fastly.")

	flag.Usage = usage
	flag.CommandLine.Parse(os.Args[2:]) // first arg is command
	checkArgs()

	awsSecretKey := os.Getenv("AWS_SECRET_KEY")
	fastlyKey := os.Getenv("FASTLY_KEY")

	switch os.Args[1] {
	case "rotate-creds":
		rotateCreds(fastlyKey, *serviceID, *loggingName, *awsAccessKey, awsSecretKey)
	case "describe":
		describeLogs(fastlyKey, *serviceID)
	case "delete":
		deleteLogs(fastlyKey, *serviceID)
	case "create":
		createLogs(fastlyKey, *serviceID, *awsAccessKey, awsSecretKey)
	default:
		check(errors.New("Must provide valid command. See -h for more info."))
	}
}

func rotateCreds(fastlyKey, serviceID, loggingName, awsAccessKey, awsSecretKey string) {
	err := updateService(fastlyKey, serviceID, func(versionID int) error {
		return updateLoggingCreds(serviceID, versionID, loggingName, fastlyKey, awsAccessKey, awsSecretKey)
	})
	check(err)
}

func describeLogs(fastlyKey, serviceID string) {
	err := updateService(fastlyKey, serviceID, func(versionID int) error {
		data, err := getLogConfiguration(serviceID, versionID, fastlyKey)
		if err != nil {
			return err
		}

		var buf bytes.Buffer
		json.Indent(&buf, data, "", "    ")
		fmt.Println(buf.String())
		return nil
	})
	check(err)
}

func deleteLogs(fastlyKey, serviceID string) {
	err := updateService(fastlyKey, serviceID, func(versionID int) error {
		return deleteLogConfiguration(serviceID, versionID, fastlyKey)
	})
	check(err)
}

func createLogs(fastlyKey, serviceID, awsAccessKey, awsSecretKey string) {
	err := updateService(fastlyKey, serviceID, func(versionID int) error {
		return createLogConfiguration(serviceID, versionID, fastlyKey, awsAccessKey, awsSecretKey)
	})
	check(err)
}

func checkArgs() {
	flag.VisitAll(func(f *flag.Flag) {
		if f.Value.String() == "" {
			fmt.Printf("Missing required arg '%s'.\n", f.Name)
			os.Exit(1)
		}
	})
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), "Usage of fastly-logging-creds:\n")
	fmt.Fprintln(flag.CommandLine.Output())
	flag.PrintDefaults()
	fmt.Fprintln(flag.CommandLine.Output())
	fmt.Fprint(flag.CommandLine.Output(), "Note, AWS_SECRET_KEY and FASTLY_KEY must be provided as env vars.\n")
}

func check(err error) {
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

type Version struct {
	Active bool `json:"active"`
	Number int  `json:"number"`
}

func updateService(fastlyKey, serviceID string, fn func(versionID int) error) error {
	var err error
	activeVersion, err := getActiveService(serviceID, fastlyKey)
	if err != nil {
		return err
	}

	newVersion, err := cloneService(serviceID, activeVersion, fastlyKey)
	if err != nil {
		return err
	}

	err = fn(newVersion)
	if err != nil {
		return err
	}

	return activateService(serviceID, newVersion, fastlyKey)
}

func getActiveService(serviceID, fastlyKey string) (int, error) {
	reqURL := fmt.Sprintf("/service/%s/version", serviceID)
	body, err := fastlyHTTP(reqURL, http.MethodGet, fastlyKey, nil)
	if err != nil {
		return 0, err
	}

	var data []Version
	err = json.Unmarshal(body, &data)
	if err != nil {
		return 0, err
	}

	for _, version := range data {
		if version.Active {
			return version.Number, nil
		}
	}

	return 0, errors.New("No active version found.")
}

func cloneService(serviceID string, versionID int, fastlyKey string) (int, error) {
	reqURL := fmt.Sprintf("/service/%s/version/%d/clone", serviceID, versionID)
	body, err := fastlyHTTP(reqURL, http.MethodPut, fastlyKey, nil)
	if err != nil {
		return 0, err
	}

	var data Version
	err = json.Unmarshal(body, &data)
	return data.Number, err
}

func updateLoggingCreds(serviceID string, versionID int, loggingName string, fastlyKey string, awsAccessKey string, awsSecretKey string) error {
	reqURL := fmt.Sprintf("/service/%s/version/%d/logging/s3/%s", serviceID, versionID, loggingName)
	form := url.Values{"access_key": {awsAccessKey}, "secret_key": {awsSecretKey}}
	body := strings.NewReader(form.Encode())
	_, err := fastlyHTTP(reqURL, http.MethodPut, fastlyKey, body)
	return err
}

func createLogConfiguration(serviceID string, versionID int, fastlyKey string, awsAccessKey string, awsSecretKey string) error {
	reqURL := fmt.Sprintf("/service/%s/version/%d/logging/s3", serviceID, versionID)
	form := url.Values{
		"name":        {"s3-logs"},
		"access_key":  {awsAccessKey},
		"secret_key":  {awsSecretKey},
		"bucket_name": {"aws-frontend-logs"},
		"path":        {"/fastly/" + serviceID},
	}

	body := strings.NewReader(form.Encode())
	_, err := fastlyHTTP(reqURL, http.MethodPost, fastlyKey, body)
	return err
}

func getLogConfiguration(serviceID string, versionID int, fastlyKey string) ([]byte, error) {
	reqURL := fmt.Sprintf("/service/%s/version/%d/logging/s3", serviceID, versionID)
	body, err := fastlyHTTP(reqURL, http.MethodGet, fastlyKey, nil)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func deleteLogConfiguration(serviceID string, versionID int, fastlyKey string) error {
	data, err := getLogConfiguration(serviceID, versionID, fastlyKey)
	if err != nil {
		return err
	}

	var confs []map[string]string
	err = json.Unmarshal(data, &confs)
	if err != nil {
		return err
	}

	for _, c := range confs {
		reqURL := fmt.Sprintf("/service/%s/version/%d/logging/s3/%s", serviceID, versionID, c["name"])
		_, err := fastlyHTTP(reqURL, http.MethodDelete, fastlyKey, nil)
		if err != nil {
			return errors.New(fmt.Sprintf("Unable to delete logging service %s: %v", c["name"], err))
		}
	}

	return nil
}

func activateService(serviceID string, versionID int, fastlyKey string) error {
	reqURL := fmt.Sprintf("/service/%s/version/%d/activate", serviceID, versionID)
	_, err := fastlyHTTP(reqURL, http.MethodPut, fastlyKey, nil)
	return err
}

func fastlyHTTP(path string, method string, fastlyKey string, reqBody io.Reader) ([]byte, error) {
	reqURL := "https://api.fastly.com" + path
	req, err := http.NewRequest(method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Fastly-Key", fastlyKey)
	req.Header.Add("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Fastly API request for '%s' failed: %d, %s", reqURL, resp.StatusCode, string(body))
	}

	return body, err
}
