package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nsheridan/randduration"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflare-go"
	"github.com/pelletier/go-toml"
	"github.com/rdegges/go-ipify"
)

type record struct {
	hostname, zoneID string
}

type dnsconfig struct {
	cfapi   *cloudflare.API
	records []*record
}

func (d *dnsconfig) update(ctx context.Context, record *record, ip string) error {
	recs, _, err := d.cfapi.ListDNSRecords(
		ctx, cloudflare.ZoneIdentifier(record.zoneID), cloudflare.ListDNSRecordsParams{Name: record.hostname, Type: "A"})
	if err != nil {
		return fmt.Errorf("error fetching zone for record %v: %v", record, err)
	}
	if len(recs) == 0 {
		log.Debugf("Creating new A record for %s -> %s", record.hostname, ip)
		rec := cloudflare.CreateDNSRecordParams{
			Type:    "A",
			Name:    record.hostname,
			Content: ip,
			Proxied: cloudflare.BoolPtr(false),
			TTL:     1,
			ZoneID:  record.zoneID,
		}
		_, err = d.cfapi.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(record.zoneID), rec)
		return err
	}
	if len(recs) != 1 {
		return fmt.Errorf("Found %d records for %s. There should only be one", len(recs), record.hostname)
	}
	rec := recs[0]
	if rec.Content == ip {
		log.Infof("Record %s IP %s hasn't changed, not taking action", record.hostname, ip)
		return nil
	}
	log.Debugf("Updating A record for %s -> %s", record.hostname, ip)
	new := cloudflare.UpdateDNSRecordParams{
		Content: ip,
		ID:      rec.ID,
	}
	_, err = d.cfapi.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(record.zoneID), new)
	if err == nil {
		log.Infof("Updated %s -> %s", record.hostname, ip)
	}
	return err
}

func (d *dnsconfig) run(ctx context.Context) {
	ip, err := ipify.GetIp()
	if err != nil {
		log.Errorf("Error retrieving IP: %v\n", err)
		return
	}
	log.Debugf("Discovered IP: %s", ip)
	g := errgroup.Group{}
	for _, record := range d.records {
		record := record
		g.Go(func() error {
			return d.update(ctx, record, ip)
		})
	}
	if err := g.Wait(); err != nil {
		log.Errorf("Error updating DNS: %v", err)
	}
}

// ConfigFile is a TOML config file
type ConfigFile struct {
	Global struct {
		Hostnames []string `toml:"hostnames"`
	} `toml:"global"`
	Cloudflare struct {
		Email  string `toml:"email"`
		APIKey string `toml:"api_key"`
	} `toml:"cloudflare"`
}

func loadConfig(path string) ConfigFile {
	contents, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Error loading config %s: %v", path, err)
	}
	cfgfile := ConfigFile{}
	if err := toml.Unmarshal(contents, &cfgfile); err != nil {
		log.Fatalf("Error parsing config %s: %v", path, err)
	}
	return cfgfile
}

func setupCloudflare(config ConfigFile) dnsconfig {
	cfapi, err := cloudflare.New(config.Cloudflare.APIKey, config.Cloudflare.Email)
	if err != nil {
		log.Fatalf("Error creating Cloudflare API client: %v", err)
	}
	configs := dnsconfig{
		cfapi: cfapi,
	}
	for _, hostname := range config.Global.Hostnames {
		domain := strings.SplitN(hostname, ".", 2)[1]
		log.Debugf("Using domain %s from hostname %s", domain, hostname)
		log.Debugf("Querying Cloudflare Zone ID for domain %s", domain)
		id, err := cfapi.ZoneIDByName(domain)
		if err != nil {
			log.Fatalf("Error retrieving zone ID for domain %s: %v", domain, err)
		}
		log.Debugf("Got zone ID %s", id)
		configs.records = append(configs.records, &record{
			hostname: hostname,
			zoneID:   id,
		})
	}
	return configs
}

func main() {
	var (
		cfgFile string
		verbose bool
	)
	flag.StringVar(&cfgFile, "config", "cfddns.toml", "Path to config file")
	flag.BoolVar(&verbose, "verbose", false, "Verbose output")
	flag.Parse()

	if verbose {
		log.SetLevel(log.DebugLevel)
	}

	log.Debugf("Loading config file %s", cfgFile)
	cfg := loadConfig(cfgFile)

	dnsConfig := setupCloudflare(cfg)
	log.Debug("Starting server")
	ctx := context.Background()
	r := &randduration.RandomDuration{}
	for {
		dnsConfig.run(ctx)
		duration := r.Generate()
		log.Infof("Sleeping for %v", duration)
		time.Sleep(duration)
	}
}
