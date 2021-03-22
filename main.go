package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
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

	reqURL := url.URL{
		Scheme: "https",
		Host:   "api.fastly.com",
		Path:   fmt.Sprintf("/service/%s/version/1/logging/s3/%s", *serviceID, *loggingName),
	}

	form := url.Values{"access_key": {*awsAccessKey}, "secret_key": {awsSecretKey}}
	req, err := http.NewRequest(http.MethodPut, reqURL.String(), strings.NewReader(form.Encode()))
	check(err)

	req.Header.Add("Fastly-Key", fastlyKey)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	check(err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		check(fmt.Errorf("Update request failed: %d, %s", resp.StatusCode, string(body)))
	}

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
