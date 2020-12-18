package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/cloudflare/cloudflare-go"
	"github.com/pelletier/go-toml"
	"github.com/rdegges/go-ipify"
)

var (
	cfgFile = flag.String("cfg_file", "cfddns.toml", "Path to config file")
	verbose = flag.Bool("verbose", false, "Verbose output")
)

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

func updateDNS(config ConfigFile, ip string) error {
	log.Debug("Creating Cloudflare API client")
	cf, err := cloudflare.New(config.Cloudflare.APIKey, config.Cloudflare.Email)
	if err != nil {
		return err
	}
	// foo.domain.xyz -> domain.xyz
	split := strings.SplitN(config.Global.Hostname, ".", 2)
	domain := split[1]

	log.Debugf("Using domain %s from hostname %s", domain, config.Global.Hostname)
	log.Debugf("Querying Cloudflare Zone ID for domain %s", domain)
	id, err := cf.ZoneIDByName(domain)
	if err != nil {
		return err
	}
	log.Debugf("Got zone ID %s", id)
	recs, err := cf.DNSRecords(id, cloudflare.DNSRecord{Name: config.Global.Hostname})
	if len(recs) == 0 {
		log.Debugf("Creating new A record for %s -> %s", config.Global.Hostname, ip)
		rec := cloudflare.DNSRecord{
			Type:    "A",
			Name:    config.Global.Hostname,
			Content: ip,
			Proxied: false,
			TTL:     1,
			ZoneID:  id,
		}
		_, err := cf.CreateDNSRecord(id, rec)
		return err
	}
	if len(recs) > 1 {
		return fmt.Errorf("Found %d records for %s. There should only be one", len(recs), config.Global.Hostname)
	}
	log.Debugf("Updating A record for %s -> %s", config.Global.Hostname, ip)
	rec := recs[0]
	rec.Type = "A"
	rec.Content = ip
	return cf.UpdateDNSRecord(id, rec.ID, rec)
}

func loadConfig(path string) ConfigFile {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Error loading config %s: %v", path, err)
	}
	cfg := ConfigFile{}
	if err := toml.Unmarshal(contents, &cfg); err != nil {
		log.Fatalf("Error parsing config %s: %v", path, err)
	}
	return cfg
}

func main() {
	flag.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	}

	log.Debugf("Loading config file %s", *cfgFile)
	cfg := loadConfig(*cfgFile)
	log.Debug("Discovering IP")
	ip, err := ipify.GetIp()
	if err != nil {
		log.Fatalf("Error retrieving IP: %v\n", err)
	}
	log.Debugf("Discovered IP: %s", ip)

	if err := updateDNS(cfg, ip); err != nil {
		log.Fatalf("Unable to update DNS: %v", err)
	}
}
