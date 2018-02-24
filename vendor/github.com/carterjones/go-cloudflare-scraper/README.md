Cloudflare Challenge Solver
===========================

A port of [cloudflare-scrape](https://github.com/Anorov/cloudflare-scrape).

Usage
-----

```go
package main

import (
    "github.com/carterjones/go-cloudflare-scraper"
)

func main() {
	scraper := scraper.NewTransport(http.DefaultTransport)

	c := http.Client{Transport: scraper}

	res, err := c.Get(ts.URL)
	if err != nil {
		log.Fatal(err)
	}

	body, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
}

