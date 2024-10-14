package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alecthomas/kong"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gebn/go-stamp/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/automaxprocs/maxprocs"

	"github.com/gebn/unifibackup/v2/cmd/unifibackup/uploader"
)

var (
	buildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "unifibackup_build_info",
			Help: "The version and commit of the software. Constant 1.",
		},
		// the runtime version is already exposed by the default Go collector
		[]string{"version", "commit"},
	)
	buildTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "unifibackup_build_time_seconds",
		Help: "When the software was built, as seconds since the Unix Epoch.",
	})
)

var CLI struct {
	Genmeta struct {
		Dir string `help:"path of the UniFi autobackup directory." type:"existingdir" default:"/var/lib/unifi/backup/autobackup"`
	} `cmd:"" help:"Generate autobackup_meta.json for the autobackup directory"`
	Version kong.VersionFlag `env:"-"`
	Backup  struct {
		Dir     string        `help:"path of the UniFi autobackup directory." type:"existingdir" default:"/var/lib/unifi/backup/autobackup"`
		Bucket  string        `help:"name of the S3 bucket to upload to." required:""`
		Prefix  string        `help:"prepended to the backup file name to form the object key." default:"unifi/"`
		Metrics string        `help:"listen spec for the web server that exposes Prometheus metrics." default:":9184"`
		Timeout time.Duration `help:"time to allow for upload and delete requests." default:"5m"`
	} `cmd:"" help:"Copies UniFi Controller backups to S3" default:"withargs"`
}

func main() {
	maxprocs.Set() // use the library this way to avoid logging when CPU quota is undefined
	buildInfo.WithLabelValues(stamp.Version, stamp.Commit).Set(1)
	buildTime.Set(float64(stamp.Time().UnixNano()) / float64(time.Second))

	ctx := kong.Parse(
		&CLI,
		kong.Name("unifibackup"),
		kong.DefaultEnvars("UNIFIBACKUP"),
		kong.Description("Copies UniFi Controller backups to S3"),
		kong.Vars{
			"version": stamp.Summary(),
		},
	)
	switch ctx.Command() {
	case "backup":
		ctx := context.Background()
		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithUseDualStackEndpoint(aws.DualStackEndpointStateEnabled))
		if err != nil {
			panic(fmt.Errorf("failed to initialise AWS SDK: %w", err))
		}
		s3client := s3.NewFromConfig(cfg)
		uploader := uploader.New(s3client, CLI.Backup.Bucket, CLI.Backup.Prefix)
		err = daemon(ctx, CLI.Backup.Metrics, CLI.Backup.Dir, uploader, CLI.Backup.Timeout)
		if err != nil {
			panic(fmt.Errorf("failed to initialize daemon: %w", err))
		}
		break
	case "genmeta":
		err := genmeta(CLI.Genmeta.Dir)
		if err != nil {
			panic(fmt.Errorf("failed to generate meta: %w", err))
		}
		break
	default:
		panic(ctx.Command())
	}
}
