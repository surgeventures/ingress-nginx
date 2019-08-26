/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// FormatHeader name of the header used to extract the format
	FormatHeader = "X-Format"

	// CodeHeader name of the header used as source of the HTTP status code to return
	CodeHeader = "X-Code"

	// ContentType name of the header that defines the format of the reply
	ContentType = "Content-Type"

	// OriginalURI name of the header with the original URL from NGINX
	OriginalURI = "X-Original-URI"

	// Namespace name of the header that contains information about the Ingress namespace
	Namespace = "X-Namespace"

	// IngressName name of the header that contains the matched Ingress
	IngressName = "X-Ingress-Name"

	// ServiceName name of the header that contains the matched Service in the Ingress
	ServiceName = "X-Service-Name"

	// ServicePort name of the header that contains the matched Service port in the Ingress
	ServicePort = "X-Service-Port"

	// RequestId is a unique ID that identifies the request - same as for backend service
	RequestId = "X-Request-ID"

	// ErrFilesPathVar is the name of the environment variable indicating
	// the location on disk of files served by the handler.
	ErrFilesPathVar = "ERROR_FILES_PATH"
)

func main() {
	errFilesPath := "/www"
	if os.Getenv(ErrFilesPathVar) != "" {
		errFilesPath = os.Getenv(ErrFilesPathVar)
	}

	http.HandleFunc("/", errorHandler(errFilesPath))

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	http.ListenAndServe(fmt.Sprintf(":8080"), nil)
}

func modifyOutput(w http.ResponseWriter, ext string, path string, service string, uri string, headers map[string]string, endpoints map[string]map[string]string, ingressCode int, returnCode int) (string, *strings.Reader, int) {
	filename := ""
	var content *strings.Reader
	var customCode int

	log.Printf("Detected request to %v. Mocking response", service)
	for header, value := range headers {
		w.Header().Set(header, value)
	}
	// default file based on ingress code and file extension
	filename = fmt.Sprintf("%v/%v%v", path, ingressCode, ext)

	// Choose custom response file for if endpoint listed
	for endpoint, config := range endpoints {
		if os.Getenv(config["env"]) != "" {
			content = strings.NewReader(os.Getenv(config["env"]))
		} else if strings.Contains(uri, endpoint) {
			customCode = http.StatusOK
			filename = fmt.Sprintf("%v/%v", path, config["file"])
		}
	}

	return filename, content, customCode
}

func errorHandler(path string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		refreshHeaders := map[string]string{
			"Content-Type":                     "application/vnd.api+json; charset=utf-8",
			"Access-Control-Allow-Origin":      r.Header.Get("Origin"),
			"Access-Control-Allow-Credentials": "true",
		}
		refreshEndpoints := map[string]map[string]string{
			"/version-checks/fresha": {
				"file": "refresh-fresha.json",
				"env":  "REFRESH_FRESHA_MAINTENANCE"},
			"/version-checks/shedul": {
				"file": "refresh-shedul.json",
				"env":  "REFRESH_SHEDUL_MAINTENANCE"},
		}

		filename := ""
		var content *strings.Reader = nil
		start := time.Now()
		ext := "html"

		if os.Getenv("DEBUG") != "" {
			w.Header().Set(FormatHeader, r.Header.Get(FormatHeader))
			w.Header().Set(CodeHeader, r.Header.Get(CodeHeader))
			w.Header().Set(ContentType, r.Header.Get(ContentType))
			w.Header().Set(OriginalURI, r.Header.Get(OriginalURI))
			w.Header().Set(Namespace, r.Header.Get(Namespace))
			w.Header().Set(IngressName, r.Header.Get(IngressName))
			w.Header().Set(ServiceName, r.Header.Get(ServiceName))
			w.Header().Set(ServicePort, r.Header.Get(ServicePort))
			w.Header().Set(RequestId, r.Header.Get(RequestId))
		}

		format := r.Header.Get(FormatHeader)
		if format == "" {
			format = "text/html"
			log.Printf("format not specified. Using %v", format)
		}

		cext, err := mime.ExtensionsByType(format)
		if err != nil {
			log.Printf("unexpected error reading media type extension: %v. Using %v", err, ext)
			format = "text/html"
		} else if len(cext) == 0 {
			log.Printf("couldn't get media type extension. Using %v", ext)
		} else {
			ext = cext[0]
		}
		w.Header().Set(ContentType, format)

		errCode := r.Header.Get(CodeHeader)
		code, err := strconv.Atoi(errCode)
		if err != nil {
			code = 404
			log.Printf("unexpected error reading return code: %v. Using %v", err, code)
		}
		customCode := code
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		// Custom extension only used by refresh application
		uri := r.Header.Get(OriginalURI)
		serviceName := r.Header.Get(ServiceName)
		if serviceName == "refresh" {
			filename, content, customCode = modifyOutput(w, ext, path, serviceName, uri, refreshHeaders, refreshEndpoints, code, 200)
		} else {
			filename = fmt.Sprintf("%v/%v%v", path, code, ext)
		}
		if content != nil {
			w.WriteHeader(customCode)
			io.Copy(w, content)
		} else {
			f, err := os.Open(filename)
			if err != nil {
				log.Printf("unexpected error opening file: %v", err)
				scode := strconv.Itoa(code)
				filename = fmt.Sprintf("%v/%cxx%v", path, scode[0], ext)
				f, err := os.Open(filename)
				if err != nil {
					log.Printf("unexpected error opening file: %v", err)
					http.NotFound(w, r)
					return
				}
				defer f.Close()
				log.Printf("serving custom error response for code %v and format %v from file %v", code, format, filename)
				w.WriteHeader(code)
				io.Copy(w, f)
				return
			}
			defer f.Close()
			log.Printf("serving custom error response for code %v and format %v from file %v", code, format, filename)
			w.WriteHeader(customCode)
			io.Copy(w, f)
		}

		duration := time.Now().Sub(start).Seconds()

		proto := strconv.Itoa(r.ProtoMajor)
		proto = fmt.Sprintf("%s.%s", proto, strconv.Itoa(r.ProtoMinor))

		requestCount.WithLabelValues(proto).Inc()
		requestDuration.WithLabelValues(proto).Observe(duration)
	}
}
