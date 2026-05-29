package botdetect

import "strings"

// botPatterns is a curated list of bot user-agent substrings.
var botPatterns = []string{
	"Googlebot",
	"Bingbot",
	"bingbot",
	"YandexBot",
	"Baiduspider",
	"DuckDuckBot",
	"facebookexternalhit",
	"Twitterbot",
	"rogerbot",
	"linkedinbot",
	"embedly",
	"showyoubot",
	"outbrain",
	"pinterest",
	"slackbot",
	"vkShare",
	"W3C_Validator",
	"whatsapp",
	"Applebot",
	"SemrushBot",
	"AhrefsBot",
	"MJ12bot",
	"DotBot",
	"PetalBot",
	"Bytespider",
	"GPTBot",
	"ClaudeBot",
	"Sogou",
	"Exabot",
	"ia_archiver",
	"archive.org_bot",
	"Screaming Frog",
	"Majestic",
	"DataForSeoBot",
	"coccocbot",
	"SEOkicks",
	"BLEXBot",
	"proximic",
	"megaindex",
	"Uptimebot",
	"PaperLiBot",
	"Grapeshot",
	"GrapeshotCrawler",
	"curl",
	"wget",
	"python-requests",
	"Go-http-client",
	"Java/",
	"okhttp",
	"Apache-HttpClient",
	"libwww-perl",
	"Mechanize",
	"Scrapy",
	"Nutch",
	"CrawlerBot",
	"spider",
	"Crawler",
	"bot",
	"HeadlessChrome",
	"PhantomJS",
	"Selenium",
	"Puppeteer",
	"Lighthouse",
	"PTST",
	"GTmetrix",
	"pingdom",
	"StatusCake",
	"UptimeRobot",
	"Site24x7",
	"Datadog",
	"NewRelicPinger",
	"Zabbix",
	"monitoring",
	"nagios",
	"YisouSpider",
	"Qwantify",
	"CCBot",
	"Buck",
	"netcraft",
	"Wappalyzer",
	"BuiltWith",
	"WhatWeb",
	"Riddler",
	"Dataprovider",
	"NetcraftSurveyAgent",
	"ZoominfoBot",
	"Nimbostratus",
	"censys",
	"CensysInspect",
	"Shodan",
	"masscan",
	"Nmap",
	"zgrab",
	"mail.ru",
	"Feedfetcher",
	"AdsBot-Google",
	"Mediapartners-Google",
	"APIs-Google",
	"Google-Read-Aloud",
	"Chrome-Lighthouse",
	"Seznam",
	"Yeti",
	"NaverBot",
	"Daum",
	"360Spider",
	"Qihoo",
	"TurnitinBot",
	"Grammarly",
	"ZoomBot",
	"BitSight",
	"SecurityTrails",
}

// lowercasePatterns holds the pre-computed lowercase versions of botPatterns.
var lowercasePatterns []string

func init() {
	lowercasePatterns = make([]string, len(botPatterns))
	for i, p := range botPatterns {
		lowercasePatterns[i] = strings.ToLower(p)
	}
}

// MatchesBot returns true if the given user-agent string matches any known bot pattern.
func MatchesBot(ua string) bool {
	lower := strings.ToLower(ua)
	for _, pattern := range lowercasePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
