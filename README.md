## Laracast Downloader
This is the Go version of Laracasts downloader, ultra fast. For this be sure that go is installed.
Currently work with series URI, in future version it will download by topics, larabits, by teacher.

### Features
- Go version of Laracasts downloader
- With a stable high-speed connection and SSDs, 15 workers can download ~15 episodes in parallel at ~5–10 Mbps each, totaling ~75–150 Mbps utilization.
- Using cache for better performance and check if the new episode is available grab it. Cache works for 7 days.


### To up and run:
- Make a build (preferred) by running go build, this should place the build name laracasts-dl in cmd/laracasts-dl
- copy .env.example to .env using command `cp .env.example .env`
- Provide email, password in .env
- To run:
   1. Using build
      - ./laracasts-dl ---download all series
      - ./laracasts-dl -s css-flexbox-simplified   ---download specific series
  2. Using dev mode
     - go run main.go ---download all series
     - go run main.go -s css-flexbox-simplified ---download specific series
         
