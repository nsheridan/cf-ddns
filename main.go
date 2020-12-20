package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/nsheridan/randduration"
	log "github.com/sirupsen/logrus"

	"github.com/cloudflare/cloudflare-go"
	"github.com/pelletier/go-toml"
	"github.com/rdegges/go-ipify"
)

var (
	cfgFile = flag.String("cfg_file", "cfddns.toml", "Path to config file")
	verbose = flag.Bool("verbose", false, "Verbose output")

	savedIP = "none"
)

type dnsconfig struct {
	cfapi    *cloudflare.API
	hostname string
	zoneID   string
}

// ConfigFile is a TOML config file
type ConfigFile struct {
	Global     GlobalConfig
	Cloudflare CloudflareConfig
}

// CloudflareConfig contains the necessary info to communicate with the Cloudflare API
type CloudflareConfig struct {
	Email  string `toml:"email"`
	APIKey string `toml:"api_key"`
}

// GlobalConfig contains essential config
type GlobalConfig struct {
	Hostname string `toml:"hostname"`
}

func updateDNS(config *dnsconfig, ip string) error {
	recs, err := config.cfapi.DNSRecords(config.zoneID, cloudflare.DNSRecord{Name: config.hostname})
	if err != nil {
		return fmt.Errorf("error fetching zone records: %v", err)
	}
	if len(recs) == 0 {
		log.Debugf("Creating new A record for %s -> %s", config.hostname, ip)
		rec := cloudflare.DNSRecord{
			Type:    "A",
			Name:    config.hostname,
			Content: ip,
			Proxied: false,
			TTL:     1,
			ZoneID:  config.zoneID,
		}
		_, err := config.cfapi.CreateDNSRecord(config.zoneID, rec)
		return err
	}
	if len(recs) > 1 {
		return fmt.Errorf("Found %d records for %s. There should only be one", len(recs), config.hostname)
	}
	log.Debugf("Updating A record for %s -> %s", config.hostname, ip)
	rec := recs[0]
	rec.Type = "A"
	rec.Content = ip
	return config.cfapi.UpdateDNSRecord(config.zoneID, rec.ID, rec)
}

func loadConfig(path string) ConfigFile {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Error loading config %s: %v", path, err)
	}
	cfgfile := ConfigFile{}
	if err := toml.Unmarshal(contents, &cfgfile); err != nil {
		log.Fatalf("Error parsing config %s: %v", path, err)
	}
	return cfgfile
}

func run(cfg *dnsconfig, finished chan<- bool) {
	ip, err := ipify.GetIp()
	if err != nil {
		log.Errorf("Error retrieving IP: %v\n", err)
		finished <- true
		return
	}
	log.Debugf("Discovered IP: %s", ip)
	if savedIP == "none" || ip != savedIP {
		log.Infof("Saved IP is %s, current IP is %s. Update required.", savedIP, ip)
		if err := updateDNS(cfg, ip); err != nil {
			log.Errorf("Unable to update DNS: %v", err)
		} else {
			log.Info("Updated")
			savedIP = ip
		}
	} else {
		log.Infof("IP %s hasn't changed since last run. Not taking any action", ip)
	}
	finished <- true
}

func setupCloudflare(config ConfigFile) *dnsconfig {
	cfapi, err := cloudflare.New(config.Cloudflare.APIKey, config.Cloudflare.Email)
	if err != nil {
		log.Fatalf("Error creating Cloudflare API client: %v", err)
	}
	domain := strings.SplitN(config.Global.Hostname, ".", 2)[1]
	log.Debugf("Using domain %s from hostname %s", domain, config.Global.Hostname)
	log.Debugf("Querying Cloudflare Zone ID for domain %s", domain)
	id, err := cfapi.ZoneIDByName(domain)
	if err != nil {
		log.Fatalf("Error retrieving zone ID for domain %s: %v", domain, err)
	}
	log.Debugf("Got zone ID %s", id)
	return &dnsconfig{
		cfapi:    cfapi,
		hostname: config.Global.Hostname,
		zoneID:   id,
	}

}

func main() {
	flag.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	}

	log.Debugf("Loading config file %s", *cfgFile)
	cfg := loadConfig(*cfgFile)

	dc := setupCloudflare(cfg)
	finished := make(chan bool, 1)
	log.Debug("Starting server")
	for {
		go run(dc, finished)
		<-finished

		r := &randduration.RandomDuration{}
		duration := r.Generate()
		log.Infof("Sleeping for %v", duration)
		time.Sleep(duration)
	}
}
