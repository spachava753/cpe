package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/goyek/goyek/v2"
)

// DebugProxy starts an HTTP debug proxy
var DebugProxy = goyek.Define(goyek.Task{
	Name:  "debug-proxy",
	Usage: "HTTP debug proxy. Use -target=URL [-port=8080]",
	Action: func(a *goyek.A) {
		if *targetURL == "" {
			a.Fatal("Usage: go run ./build -target=<url> [-port=8080] debug-proxy")
		}

		target, err := url.Parse(*targetURL)
		if err != nil {
			a.Fatalf("Invalid target URL: %v", err)
		}

		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
			},
			ModifyResponse: func(resp *http.Response) error {
				fmt.Printf("\n%s=== RESPONSE ===%s\n", "\033[32m", "\033[0m")
				fmt.Printf("Status: %s\n", resp.Status)

				fmt.Printf("\n%sResponse Headers:%s\n", "\033[33m", "\033[0m")
				for k, v := range resp.Header {
					fmt.Printf("  %s: %s\n", k, strings.Join(v, ", "))
				}

				if resp.Body != nil {
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						return err
					}
					resp.Body.Close()

					if resp.Header.Get("Content-Encoding") == "gzip" {
						reader, err := gzip.NewReader(bytes.NewReader(body))
						if err == nil {
							body, _ = io.ReadAll(reader)
							reader.Close()
						}
					}

					fmt.Printf("\n%sResponse Body:%s\n", "\033[33m", "\033[0m")
					prettyPrint(body)

					resp.Body = io.NopCloser(bytes.NewReader(body))
					resp.ContentLength = int64(len(body))
					resp.Header.Del("Content-Encoding")
				}

				return nil
			},
		}

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Printf("\n%s========================================%s\n", "\033[36m", "\033[0m")
			fmt.Printf("%s[%s] %s %s%s\n", "\033[36m", time.Now().Format("15:04:05"), r.Method, r.URL.Path, "\033[0m")
			fmt.Printf("%s========================================%s\n", "\033[36m", "\033[0m")

			fmt.Printf("\n%sRequest Headers:%s\n", "\033[33m", "\033[0m")
			for k, v := range r.Header {
				val := strings.Join(v, ", ")
				if strings.ToLower(k) == "authorization" || strings.ToLower(k) == "x-api-key" {
					if len(val) > 20 {
						val = val[:10] + "..." + val[len(val)-5:]
					}
				}
				fmt.Printf("  %s: %s\n", k, val)
			}

			if r.Body != nil {
				body, err := io.ReadAll(r.Body)
				if err == nil && len(body) > 0 {
					r.Body.Close()

					fmt.Printf("\n%sRequest Body:%s\n", "\033[33m", "\033[0m")
					prettyPrint(body)

					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = int64(len(body))
				}
			}

			proxy.ServeHTTP(w, r)
		})

		fmt.Printf("\n🔍 Debug proxy listening on http://localhost:%s\n", *port)
		fmt.Printf("   Proxying to: %s\n", target.String())
		fmt.Printf("   Set your base_url to: http://localhost:%s\n\n", *port)

		if err := http.ListenAndServe(":"+*port, nil); err != nil {
			a.Fatalf("Server error: %v", err)
		}
	},
})

func prettyPrint(data []byte) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		pretty, _ := json.MarshalIndent(obj, "  ", "  ")
		s := string(pretty)
		if len(s) > 2000 {
			s = s[:2000] + "\n  ... (truncated)"
		}
		fmt.Printf("  %s\n", s)
	} else {
		s := string(data)
		if len(s) > 2000 {
			s = s[:2000] + "\n... (truncated)"
		}
		fmt.Printf("  %s\n", s)
	}
}
