package exporter

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/go-github/v50/github"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/promhippie/github_exporter/pkg/config"
	"github.com/ryanuber/go-glob"
)

// RunnerCollector collects metrics about the runners.
type RunnerCollector struct {
	client   *github.Client
	logger   log.Logger
	failures *prometheus.CounterVec
	duration *prometheus.HistogramVec
	config   config.Target

	RepoOnline       *prometheus.Desc
	RepoBusy         *prometheus.Desc
	EnterpriseOnline *prometheus.Desc
	EnterpriseBusy   *prometheus.Desc
	OrgOnline        *prometheus.Desc
	OrgBusy          *prometheus.Desc
}

// NewRunnerCollector returns a new RunnerCollector.
func NewRunnerCollector(logger log.Logger, client *github.Client, failures *prometheus.CounterVec, duration *prometheus.HistogramVec, cfg config.Target) *RunnerCollector {
	if failures != nil {
		failures.WithLabelValues("runner").Add(0)
	}

	labels := []string{"owner", "id", "name", "os", "status"}
	return &RunnerCollector{
		client:   client,
		logger:   log.With(logger, "collector", "runner"),
		failures: failures,
		duration: duration,
		config:   cfg,

		RepoOnline: prometheus.NewDesc(
			"github_runner_repo_online",
			"Static metrics of runner is online or not",
			labels,
			nil,
		),
		RepoBusy: prometheus.NewDesc(
			"github_runner_repo_busy",
			"1 if the runner is busy, 0 otherwise",
			labels,
			nil,
		),
		EnterpriseOnline: prometheus.NewDesc(
			"github_runner_enterprise_online",
			"Static metrics of runner is online or not",
			labels,
			nil,
		),
		EnterpriseBusy: prometheus.NewDesc(
			"github_runner_enterprise_busy",
			"1 if the runner is busy, 0 otherwise",
			labels,
			nil,
		),
		OrgOnline: prometheus.NewDesc(
			"github_runner_org_online",
			"Static metrics of runner is online or not",
			labels,
			nil,
		),
		OrgBusy: prometheus.NewDesc(
			"github_runner_org_busy",
			"1 if the runner is busy, 0 otherwise",
			labels,
			nil,
		),
	}
}

// Metrics simply returns the list metric descriptors for generating a documentation.
func (c *RunnerCollector) Metrics() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.RepoOnline,
		c.RepoBusy,
		c.EnterpriseOnline,
		c.EnterpriseBusy,
		c.OrgOnline,
		c.OrgBusy,
	}
}

// Describe sends the super-set of all possible descriptors of metrics collected by this Collector.
func (c *RunnerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.RepoOnline
	ch <- c.RepoBusy
	ch <- c.EnterpriseOnline
	ch <- c.EnterpriseBusy
	ch <- c.OrgOnline
	ch <- c.OrgBusy
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *RunnerCollector) Collect(ch chan<- prometheus.Metric) {
	{
		now := time.Now()
		records := c.repoRunners()
		c.duration.WithLabelValues("runner").Observe(time.Since(now).Seconds())

		for _, record := range records {
			var (
				online float64
			)

			labels := []string{
				"TODO: repo",
				strconv.FormatInt(record.GetID(), 10),
				record.GetName(),
				record.GetOS(),
				record.GetStatus(),
			}

			if record.GetStatus() == "online" {
				online = 1.0
			}

			ch <- prometheus.MustNewConstMetric(
				c.RepoOnline,
				prometheus.GaugeValue,
				online,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.RepoBusy,
				prometheus.GaugeValue,
				boolToFloat64(*record.Busy),
				labels...,
			)
		}
	}

	{
		now := time.Now()
		records := c.enterpriseRunners()
		c.duration.WithLabelValues("runner").Observe(time.Since(now).Seconds())

		for _, record := range records {
			var (
				online float64
			)

			labels := []string{
				"TODO: enterprise",
				strconv.FormatInt(record.GetID(), 10),
				record.GetName(),
				record.GetOS(),
				record.GetStatus(),
			}

			if record.GetStatus() == "online" {
				online = 1.0
			}

			ch <- prometheus.MustNewConstMetric(
				c.EnterpriseOnline,
				prometheus.GaugeValue,
				online,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.EnterpriseBusy,
				prometheus.GaugeValue,
				boolToFloat64(*record.Busy),
				labels...,
			)
		}
	}

	{
		now := time.Now()
		records := c.orgRunners()
		c.duration.WithLabelValues("runner").Observe(time.Since(now).Seconds())

		for _, record := range records {
			var (
				online float64
			)

			labels := []string{
				"TODO: org",
				strconv.FormatInt(record.GetID(), 10),
				record.GetName(),
				record.GetOS(),
				record.GetStatus(),
			}

			if record.GetStatus() == "online" {
				online = 1.0
			}

			ch <- prometheus.MustNewConstMetric(
				c.OrgOnline,
				prometheus.GaugeValue,
				online,
				labels...,
			)

			ch <- prometheus.MustNewConstMetric(
				c.OrgBusy,
				prometheus.GaugeValue,
				boolToFloat64(*record.Busy),
				labels...,
			)
		}
	}
}

func (c *RunnerCollector) repoRunners() []*github.Runner {
	result := make([]*github.Runner, 0)

	for _, name := range c.config.Repos.Value() {
		n := strings.Split(name, "/")

		if len(n) != 2 {
			level.Error(c.logger).Log(
				"msg", "Invalid repo name",
				"name", name,
			)

			c.failures.WithLabelValues("runner").Inc()
			continue
		}

		splitOwner, splitName := n[0], n[1]

		ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
		defer cancel()

		repos, err := reposByOwnerAndName(ctx, c.client, splitOwner, splitName)

		if err != nil {
			level.Error(c.logger).Log(
				"msg", "Failed to fetch repos",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("runner").Inc()
			continue
		}

		for _, repo := range repos {
			if !glob.Glob(name, *repo.FullName) {
				continue
			}

			records, err := c.pagedRepoRunners(ctx, *repo.Owner.Login, *repo.Name)

			if err != nil {
				level.Error(c.logger).Log(
					"msg", "Failed to fetch repo runners",
					"name", name,
					"err", err,
				)

				c.failures.WithLabelValues("runner").Inc()
				continue
			}

			result = append(result, records...)
		}
	}

	return result
}

func (c *RunnerCollector) pagedRepoRunners(ctx context.Context, owner, name string) ([]*github.Runner, error) {
	opts := &github.ListOptions{
		PerPage: 200,
	}

	var (
		runners []*github.Runner
	)

	for {
		result, resp, err := c.client.Actions.ListRunners(
			ctx,
			owner,
			name,
			opts,
		)

		if err != nil {
			return nil, err
		}

		runners = append(
			runners,
			result.Runners...,
		)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return runners, nil
}

func (c *RunnerCollector) enterpriseRunners() []*github.Runner {
	result := make([]*github.Runner, 0)

	for _, name := range c.config.Enterprises.Value() {
		ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
		defer cancel()

		records, err := c.pagedEnterpriseRunners(ctx, name)

		if err != nil {
			level.Error(c.logger).Log(
				"msg", "Failed to fetch enterprise runners",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("runner").Inc()
			continue
		}

		result = append(result, records...)
	}

	return result
}

func (c *RunnerCollector) pagedEnterpriseRunners(ctx context.Context, name string) ([]*github.Runner, error) {
	opts := &github.ListOptions{
		PerPage: 50,
	}

	var (
		runners []*github.Runner
	)

	for {
		result, resp, err := c.client.Enterprise.ListRunners(
			ctx,
			name,
			opts,
		)

		if err != nil {
			return nil, err
		}

		runners = append(
			runners,
			result.Runners...,
		)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return runners, nil
}

func (c *RunnerCollector) orgRunners() []*github.Runner {
	result := make([]*github.Runner, 0)

	for _, name := range c.config.Orgs.Value() {
		ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
		defer cancel()

		records, err := c.pagedOrgRunners(ctx, name)

		if err != nil {
			level.Error(c.logger).Log(
				"msg", "Failed to fetch org runners",
				"name", name,
				"err", err,
			)

			c.failures.WithLabelValues("runner").Inc()
			continue
		}

		result = append(result, records...)
	}

	return result
}

func (c *RunnerCollector) pagedOrgRunners(ctx context.Context, name string) ([]*github.Runner, error) {
	opts := &github.ListOptions{
		PerPage: 50,
	}

	var (
		runners []*github.Runner
	)

	for {
		result, resp, err := c.client.Actions.ListOrganizationRunners(
			ctx,
			name,
			opts,
		)

		if err != nil {
			return nil, err
		}

		runners = append(
			runners,
			result.Runners...,
		)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return runners, nil
}