package exporter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v72/github"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/promhippie/github_exporter/pkg/config"
	"github.com/promhippie/github_exporter/pkg/store"
)

// BillingCollector collects metrics about the servers.
type BillingCollector struct {
	client   *github.Client
	logger   *slog.Logger
	db       store.Store
	failures *prometheus.CounterVec
	duration *prometheus.HistogramVec
	config   config.Target

	MinutesUsed          *prometheus.Desc
	MinutesUsedBreakdown *prometheus.Desc
	PaidMinutesUsed      *prometheus.Desc
	IncludedMinutes      *prometheus.Desc

	BandwidthUsed     *prometheus.Desc
	BandwidthPaid     *prometheus.Desc
	BandwidthIncluded *prometheus.Desc

	DaysLeft              *prometheus.Desc
	EastimatedPaidStorage *prometheus.Desc
	EastimatedStorage     *prometheus.Desc
}

// NewBillingCollector returns a new BillingCollector.
func NewBillingCollector(logger *slog.Logger, client *github.Client, db store.Store, failures *prometheus.CounterVec, duration *prometheus.HistogramVec, cfg config.Target) *BillingCollector {
	if failures != nil {
		failures.WithLabelValues("billing").Add(0)
	}

	labels := []string{"type", "name"}
	return &BillingCollector{
		client:   client,
		logger:   logger.With("collector", "billing"),
		db:       db,
		failures: failures,
		duration: duration,
		config:   cfg,

		MinutesUsed: prometheus.NewDesc(
			"github_action_billing_minutes_used",
			"Total action minutes used for this type",
			labels,
			nil,
		),
		MinutesUsedBreakdown: prometheus.NewDesc(
			"github_action_billing_minutes_used_breakdown",
			"Total action minutes used for this type broken down by operating system",
			append(labels, "os"),
			nil,
		),
		PaidMinutesUsed: prometheus.NewDesc(
			"github_action_billing_paid_minutes",
			"Total paid minutes used for this type",
			labels,
			nil,
		),
		IncludedMinutes: prometheus.NewDesc(
			"github_action_billing_included_minutes",
			"Included minutes for this type",
			labels,
			nil,
		),
		BandwidthUsed: prometheus.NewDesc(
			"github_package_billing_gigabytes_bandwidth_used",
			"Total bandwidth used by this type in Gigabytes",
			labels,
			nil,
		),
		BandwidthPaid: prometheus.NewDesc(
			"github_package_billing_paid_gigabytes_bandwidth_used",
			"Total paid bandwidth used by this type in Gigabytes",
			labels,
			nil,
		),
		BandwidthIncluded: prometheus.NewDesc(
			"github_package_billing_included_gigabytes_bandwidth",
			"Included bandwidth for this type in Gigabytes",
			labels,
			nil,
		),
		DaysLeft: prometheus.NewDesc(
			"github_storage_billing_days_left_in_cycle",
			"Days left within this billing cycle for this type",
			labels,
			nil,
		),
		EastimatedPaidStorage: prometheus.NewDesc(
			"github_storage_billing_estimated_paid_storage_for_month",
			"Estimated paid storage for this month for this type",
			labels,
			nil,
		),
		EastimatedStorage: prometheus.NewDesc(
			"github_storage_billing_estimated_storage_for_month",
			"Estimated total storage for this month for this type",
			labels,
			nil,
		),
	}
}

// Metrics simply returns the list metric descriptors for generating a documentation.
func (c *BillingCollector) Metrics() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.MinutesUsed,
		c.MinutesUsedBreakdown,
		c.PaidMinutesUsed,
		c.IncludedMinutes,
		c.BandwidthUsed,
		c.BandwidthPaid,
		c.BandwidthIncluded,
		c.DaysLeft,
		c.EastimatedPaidStorage,
		c.EastimatedStorage,
	}
}

// Describe sends the super-set of all possible descriptors of metrics collected by this Collector.
func (c *BillingCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.MinutesUsed
	ch <- c.MinutesUsedBreakdown
	ch <- c.PaidMinutesUsed
	ch <- c.IncludedMinutes
	ch <- c.BandwidthUsed
	ch <- c.BandwidthPaid
	ch <- c.BandwidthIncluded
	ch <- c.DaysLeft
	ch <- c.EastimatedPaidStorage
	ch <- c.EastimatedStorage
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *BillingCollector) Collect(ch chan<- prometheus.Metric) {
	{
		collected := make([]string, 0)

		now := time.Now()
		billing := c.getActionBilling()
		c.duration.WithLabelValues("action").Observe(time.Since(now).Seconds())

		c.logger.Debug("Fetched action billing",
			"count", len(billing),
			"duration", time.Since(now),
		)

		for _, record := range billing {
			if alreadyCollected(collected, record.Name) {
				c.logger.Debug("Already collected action billing",
					"type", record.Type,
					"name", record.Name,
				)

				continue
			}

			collected = append(collected, record.Name)

			c.logger.Debug("Collecting action billing",
				"type", record.Type,
				"name", record.Name,
			)

			labels := []string{
				record.Type,
				record.Name,
			}

			ch <- prometheus.MustNewConstMetric(
				c.MinutesUsed,
				prometheus.GaugeValue,
				record.TotalMinutesUsed,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.PaidMinutesUsed,
				prometheus.GaugeValue,
				record.TotalPaidMinutesUsed,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.IncludedMinutes,
				prometheus.GaugeValue,
				record.IncludedMinutes,
				labels...,
			)

			for os, value := range record.MinutesUsedBreakdown {
				ch <- prometheus.MustNewConstMetric(
					c.MinutesUsedBreakdown,
					prometheus.GaugeValue,
					float64(value),
					append(labels, os)...,
				)
			}
		}
	}

	{
		collected := make([]string, 0)

		now := time.Now()
		billing := c.getPackageBilling()
		c.duration.WithLabelValues("action").Observe(time.Since(now).Seconds())

		c.logger.Debug("Fetched package billing",
			"count", len(billing),
			"duration", time.Since(now),
		)

		for _, record := range billing {
			if alreadyCollected(collected, record.Name) {
				c.logger.Debug("Already collected package billing",
					"type", record.Type,
					"name", record.Name,
				)

				continue
			}

			collected = append(collected, record.Name)

			c.logger.Debug("Collecting package billing",
				"type", record.Type,
				"name", record.Name,
			)

			labels := []string{
				record.Type,
				record.Name,
			}

			ch <- prometheus.MustNewConstMetric(
				c.BandwidthUsed,
				prometheus.GaugeValue,
				float64(record.TotalGigabytesBandwidthUsed),
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.BandwidthPaid,
				prometheus.GaugeValue,
				float64(record.TotalPaidGigabytesBandwidthUsed),
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.BandwidthIncluded,
				prometheus.GaugeValue,
				record.IncludedGigabytesBandwidth,
				labels...,
			)
		}
	}

	{
		collected := make([]string, 0)

		now := time.Now()
		billing := c.getStorageBilling()
		c.duration.WithLabelValues("action").Observe(time.Since(now).Seconds())

		c.logger.Debug("Fetched storage billing",
			"count", len(billing),
			"duration", time.Since(now),
		)

		for _, record := range billing {
			if alreadyCollected(collected, record.Name) {
				c.logger.Debug("Already collected storage billing",
					"type", record.Type,
					"name", record.Name,
				)

				continue
			}

			collected = append(collected, record.Name)

			c.logger.Debug("Collecting storage billing",
				"type", record.Type,
				"name", record.Name,
			)

			labels := []string{
				record.Type,
				record.Name,
			}

			ch <- prometheus.MustNewConstMetric(
				c.DaysLeft,
				prometheus.GaugeValue,
				float64(record.DaysLeftInBillingCycle),
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.EastimatedPaidStorage,
				prometheus.GaugeValue,
				record.EstimatedPaidStorageForMonth,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.EastimatedStorage,
				prometheus.GaugeValue,
				record.EstimatedStorageForMonth,
				labels...,
			)
		}
	}
}

// Enhanced billing platform structures
type UsageItem struct {
	Date             string  `json:"date"`
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Quantity         float64 `json:"quantity"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetAmount        float64 `json:"netAmount"`
	OrganizationName string  `json:"organizationName"`
	RepositoryName   string  `json:"repositoryName"`
}

type UsageResponse struct {
	UsageItems []UsageItem `json:"usageItems"`
}

type actionBilling struct {
	Type                 string
	Name                 string
	TotalMinutesUsed     float64
	TotalPaidMinutesUsed float64
	IncludedMinutes      float64
	MinutesUsedBreakdown map[string]int
}

type packageBilling struct {
	Type                            string
	Name                            string
	TotalGigabytesBandwidthUsed     float64
	TotalPaidGigabytesBandwidthUsed float64
	IncludedGigabytesBandwidth      float64
}

type storageBilling struct {
	Type                         string
	Name                         string
	DaysLeftInBillingCycle       int
	EstimatedPaidStorageForMonth float64
	EstimatedStorageForMonth     float64
}

// parseUsageResponse converts the new API response to billing structures
func parseUsageResponse(response *UsageResponse, billingType, name string) (*actionBilling, *packageBilling, *storageBilling) {
	actionBill := &actionBilling{
		Type:                 billingType,
		Name:                 name,
		MinutesUsedBreakdown: make(map[string]int),
	}

	packageBill := &packageBilling{
		Type: billingType,
		Name: name,
	}

	storageBill := &storageBilling{
		Type: billingType,
		Name: name,
	}

	if response == nil || len(response.UsageItems) == 0 {
		return actionBill, packageBill, storageBill
	}

	for _, item := range response.UsageItems {
		switch strings.ToLower(item.Product) {
		case "actions":
			if strings.ToLower(item.UnitType) == "minutes" {
				actionBill.TotalMinutesUsed += item.Quantity
				actionBill.TotalPaidMinutesUsed += item.NetAmount
				actionBill.IncludedMinutes += item.DiscountAmount

				os := extractOSFromSKU(item.SKU)
				if os != "" {
					actionBill.MinutesUsedBreakdown[os] += int(item.Quantity)
				}
			}
		case "packages":
			// Package registry billing is typically measured in GB of bandwidth
			unitType := strings.ToLower(item.UnitType)
			if unitType == "bytes" || unitType == "gigabytes" {
				quantity := item.Quantity
				if unitType == "bytes" {
					quantity = quantity / (1024 * 1024 * 1024) // Convert bytes to GB
				}
				packageBill.TotalGigabytesBandwidthUsed += quantity
				packageBill.TotalPaidGigabytesBandwidthUsed += item.NetAmount
				packageBill.IncludedGigabytesBandwidth += item.DiscountAmount
			}
		case "git_lfs":
			// Storage billing - try to extract billing cycle information
			unitType := strings.ToLower(item.UnitType)
			if unitType == "bytes" || unitType == "gigabytes" || unitType == "gigabytehours" {
				quantity := item.Quantity
				if unitType == "bytes" {
					quantity = quantity / (1024 * 1024 * 1024) // Convert bytes to GB
				}
				storageBill.EstimatedStorageForMonth += quantity
				storageBill.EstimatedPaidStorageForMonth += item.NetAmount
			}
		}
	}

	return actionBill, packageBill, storageBill
}

// extractOSFromSKU extracts operating system from SKU string
func extractOSFromSKU(sku string) string {
	skuLower := strings.ToLower(sku)
	switch {
	case strings.Contains(skuLower, "linux"):
		return "UBUNTU"
	case strings.Contains(skuLower, "windows"):
		return "WINDOWS"
	case strings.Contains(skuLower, "macos") || strings.Contains(skuLower, "mac"):
		return "MACOS"
	default:
		return ""
	}
}

func (c *BillingCollector) getActionBilling() []*actionBilling {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	result := make([]*actionBilling, 0)

	for _, name := range c.config.Enterprises {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/enterprises/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare action request",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch action billing",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		actionBill, _, _ := parseUsageResponse(record, "enterprise", name)
		result = append(result, actionBill)
	}

	for _, name := range c.config.Orgs {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/organizations/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare action request",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch action billing",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		actionBill, _, _ := parseUsageResponse(record, "org", name)
		result = append(result, actionBill)
	}

	return result
}

func (c *BillingCollector) getPackageBilling() []*packageBilling {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	result := make([]*packageBilling, 0)

	for _, name := range c.config.Enterprises {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/enterprises/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare package request",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch package billing",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		_, packageBill, _ := parseUsageResponse(record, "enterprise", name)
		result = append(result, packageBill)
	}

	for _, name := range c.config.Orgs {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/organizations/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare package request",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch package billing",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		_, packageBill, _ := parseUsageResponse(record, "org", name)
		result = append(result, packageBill)
	}

	return result
}

func (c *BillingCollector) getStorageBilling() []*storageBilling {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	result := make([]*storageBilling, 0)

	for _, name := range c.config.Enterprises {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/enterprises/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare storage request",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch storage billing",
				"type", "enterprise",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		_, _, storageBill := parseUsageResponse(record, "enterprise", name)
		result = append(result, storageBill)
	}

	for _, name := range c.config.Orgs {
		req, err := c.client.NewRequest(
			"GET",
			fmt.Sprintf("/organizations/%s/settings/billing/usage", name),
			nil,
		)

		if err != nil {
			c.logger.Error("Failed to prepare storage request",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		record := &UsageResponse{}
		resp, err := c.client.Do(ctx, req, record)

		if err != nil {
			c.logger.Error("Failed to fetch storage billing",
				"type", "org",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("action").Inc()
			continue
		}

		defer closeBody(resp)

		_, _, storageBill := parseUsageResponse(record, "org", name)
		result = append(result, storageBill)
	}

	return result
}
