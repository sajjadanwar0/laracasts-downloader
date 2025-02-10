package vimeo

type Episode struct {
	Title   string `json:"title"`
	VimeoId string `json:"vimeoId"`
	Number  int    `json:"position"`
}

type VimeoSegment struct {
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type VimeoConfig struct {
	Request struct {
		Files struct {
			Progressive []struct {
				URL     string `json:"url"`
				Quality string `json:"quality"`
			} `json:"progressive"`
			HLS struct {
				DefaultCDN string `json:"default_cdn"`
				Cdns       map[string]struct {
					URL string `json:"url"`
				} `json:"cdns"`
			} `json:"hls"`
		} `json:"files"`
	} `json:"request"`
}

type DashManifest struct {
	BaseURL     string         `json:"base_url"`
	InitSegment string         `json:"init_segment"`
	Segments    []VimeoSegment `json:"segments"`
}

type ProgressSegment struct {
	URL      string
	Size     int64
	Position int
	Total    int
}
