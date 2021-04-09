package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

// https://developer.fastly.com/reference/api/logging/s3/
func main() {

	serviceID := flag.String("serviceID", "", "A Fastly Service ID.")
	loggingName := flag.String("loggingName", "", "Name of your service logging configuration in Fastly.")
	awsAccessKey := flag.String("awsAccessKey", "", "AWS Access Key for S3 write access to the target bucket.")

	flag.Usage = usage // customise help/error messages
	flag.Parse()

	awsSecretKey := os.Getenv("AWS_SECRET_KEY")
	fastlyKey := os.Getenv("FASTLY_KEY")

	checkArg("serviceID", *serviceID)
	checkArg("loggingName", *loggingName)
	checkArg("awsAccessKey", *awsAccessKey)
	checkArg("AWS_SECRET_KEY", awsSecretKey)
	checkArg("FASTLY_KEY", fastlyKey)

	var err error
	activeVersion, err := getActiveService(*serviceID, fastlyKey)
	check(err)
	println(activeVersion)

	newVersion, err := cloneService(*serviceID, activeVersion, fastlyKey)
	check(err)
	println(newVersion)

	err = updateLoggingCreds(*serviceID, newVersion, *loggingName, fastlyKey)
	check(err)

	err = activateService(*serviceID, newVersion, fastlyKey)
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), "Usage of fastly-logging-creds:\n")
	fmt.Fprintln(flag.CommandLine.Output())
	flag.PrintDefaults()
	fmt.Fprintln(flag.CommandLine.Output())
	fmt.Fprint(flag.CommandLine.Output(), "Note, AWS_SECRET_KEY and FASTLY_KEY must be provided as env vars.\n")
}

func checkArg(name, value string) {
	if value == "" {
		fmt.Printf("Missing required arg '%s'.\n", name)
		os.Exit(1)
	}
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

func updateLoggingCreds(serviceID string, versionID int, loggingName string, fastlyKey string) error {
	reqURL := fmt.Sprintf("/service/%s/version/%d/logging/s3/%s", serviceID, versionID, loggingName)
	_, err := fastlyHTTP(reqURL, http.MethodPut, fastlyKey, nil)
	return err
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
