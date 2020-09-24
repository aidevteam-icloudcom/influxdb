package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/influxdata/influxdb/v2"
	"github.com/influxdata/influxdb/v2/bolt"
	"github.com/influxdata/influxdb/v2/dbrp"
	"github.com/influxdata/influxdb/v2/internal/fs"
	"github.com/influxdata/influxdb/v2/kit/cli"
	"github.com/influxdata/influxdb/v2/kit/metric"
	"github.com/influxdata/influxdb/v2/kit/prom"
	"github.com/influxdata/influxdb/v2/kv"
	"github.com/influxdata/influxdb/v2/kv/migration"
	"github.com/influxdata/influxdb/v2/kv/migration/all"
	"github.com/influxdata/influxdb/v2/storage"
	"github.com/influxdata/influxdb/v2/tenant"
	"github.com/influxdata/influxdb/v2/v1/services/meta"
	"github.com/influxdata/influxdb/v2/v1/services/meta/filestore"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// Simplified 1.x config.
type configV1 struct {
	Meta struct {
		Dir string `toml:"dir"`
	} `toml:"meta"`
	Data struct {
		Dir    string `toml:"dir"`
		WALDir string `toml:"wal-dir"`
	} `toml:"data"`
}

type optionsV1 struct {
	metaDir string
	walDir  string
	dataDir string
	// cmd option
	dbDir      string
	configFile string
}

func (o *optionsV1) checkDirs() error {
	if o.metaDir == "" || o.dataDir == "" || o.walDir == "" {
		if o.dbDir == "" {
			return errors.New("source directory not specified")
		} else {
			o.metaDir = filepath.Join(o.dbDir, "meta")
			o.dataDir = filepath.Join(o.dbDir, "data")
			o.walDir = filepath.Join(o.dbDir, "wal")
		}
	}
	return nil
}

type optionsV2 struct {
	boltPath           string
	enginePath         string
	userName           string
	password           string
	orgName            string
	bucket             string
	token              string
	retention          string
	securityScriptPath string
}

var options = struct {
	// flags for source InfluxDB
	source optionsV1

	// flags for target InfluxDB
	target optionsV2

	// verbose output
	verbose bool

	logPath string
}{}

func NewCommand() *cobra.Command {

	// source flags
	v1dir, err := influxDirV1()
	if err != nil {
		panic("error fetching default InfluxDB 1.x dir: " + err.Error())
	}

	// target flags
	v2dir, err := fs.InfluxDir()
	if err != nil {
		panic("error fetching default InfluxDB 2.0 dir: " + err.Error())
	}

	// os-specific
	var defaultSsPath string
	if runtime.GOOS == "windows" {
		defaultSsPath = filepath.Join(homeOrAnyDir(), "influxd-upgrade-security.cmd")
	} else {
		defaultSsPath = filepath.Join(homeOrAnyDir(), "influxd-upgrade-security.sh")
	}

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a 1.x version of InfluxDB",
		Long: `
    Upgrades a 1.x version of InfluxDB by performing following actions:
      1. Reads 1.x config file and creates 2.x config file with matching options. Unsupported 1.x options are reported.
      2. Copies and upgrades 1.x database files.
      3. Creates a script for creating 1.x users and their permissions. This scripts needs to be revised and run manually after starting 2.x.

    If config file is not available, 1.x db folder (--v1-dir options) is taken as an input. 
    Target 2.x database dir is specified by the --engine-path option. If changed, bolt path should to be changed as well.
`,
		RunE: runUpgradeE,
	}

	opts := []cli.Opt{
		{
			DestP:   &options.source.dbDir,
			Flag:    "v1-dir",
			Default: v1dir,
			Desc:    "path to source 1.x db directory containing meta,data and wal sub-folders",
		},
		{
			DestP:   &options.verbose,
			Flag:    "verbose",
			Default: true,
			Desc:    "verbose output",
			Short:   'v',
		},
		{
			DestP:   &options.target.boltPath,
			Flag:    "bolt-path",
			Default: filepath.Join(v2dir, bolt.DefaultFilename),
			Desc:    "path for boltdb database",
			Short:   'm',
		},
		{
			DestP:   &options.target.enginePath,
			Flag:    "engine-path",
			Default: filepath.Join(v2dir, "engine"),
			Desc:    "path for persistent engine files",
			Short:   'e',
		},
		{
			DestP:    &options.target.userName,
			Flag:     "username",
			Default:  "",
			Desc:     "primary username",
			Short:    'u',
			Required: true,
		},
		{
			DestP:    &options.target.password,
			Flag:     "password",
			Default:  "",
			Desc:     "password for username",
			Short:    'p',
			Required: true,
		},
		{
			DestP:    &options.target.orgName,
			Flag:     "org",
			Default:  "",
			Desc:     "primary organization name",
			Short:    'o',
			Required: true,
		},
		{
			DestP:    &options.target.bucket,
			Flag:     "bucket",
			Default:  "",
			Desc:     "primary bucket name",
			Short:    'b',
			Required: true,
		},
		{
			DestP:   &options.target.retention,
			Flag:    "retention",
			Default: "",
			Desc:    "optional: duration bucket will retain data. 0 is infinite. Default is 0.",
			Short:   'r',
		},
		{
			DestP:   &options.target.token,
			Flag:    "token",
			Default: "",
			Desc:    "optional: token for username, else auto-generated",
			Short:   't',
		},
		{
			DestP:   &options.source.configFile,
			Flag:    "config-file",
			Default: "/etc/influxdb/influxdb.conf",
			Desc:    "optional: Custom InfluxDB 1.x config file path, else the default config file",
		},
		{
			DestP:   &options.target.securityScriptPath,
			Flag:    "security-script",
			Default: defaultSsPath,
			Desc:    "optional: generated security upgrade script path",
		},
		{
			DestP:   &options.logPath,
			Flag:    "log-path",
			Default: filepath.Join(homeOrAnyDir(), "upgrade.log"),
			Desc:    "optional: custom log file path",
		},
	}

	cli.BindOptions(cmd, opts)
	// add sub commands
	cmd.AddCommand(v1DumpMetaCommand)
	cmd.AddCommand(v2DumpMetaCommand)
	return cmd
}

type influxDBv1 struct {
	meta *meta.Client
}

type influxDBv2 struct {
	log         *zap.Logger
	boltClient  *bolt.Client
	store       *bolt.KVStore
	kvStore     kv.SchemaStore
	tenantStore *tenant.Store
	ts          *tenant.Service
	dbrpSvc     influxdb.DBRPMappingServiceV2
	bucketSvc   influxdb.BucketService
	onboardSvc  influxdb.OnboardingService
	kvService   *kv.Service
	meta        *meta.Client
}

func (i *influxDBv2) close() error {
	err := i.meta.Close()
	if err != nil {
		return err
	}
	err = i.boltClient.Close()
	if err != nil {
		return err
	}
	err = i.store.Close()
	if err != nil {
		return err
	}
	return nil
}

func runUpgradeE(*cobra.Command, []string) error {
	ctx := context.Background()
	config := zap.NewProductionConfig()
	config.OutputPaths = append(config.OutputPaths, options.logPath)
	config.ErrorOutputPaths = append(config.ErrorOutputPaths, options.logPath)
	log, err := config.Build()
	if err != nil {
		return err
	}
	log.Info("starting InfluxDB 1.x upgrade")

	checkParam := func(name, value string) error {
		if value == "" {
			return fmt.Errorf("empty or missing mandatory option %s", name)
		}
		return nil
	}

	if err := checkParam("username", options.target.userName); err != nil {
		return err
	}
	if err := checkParam("password", options.target.password); err != nil {
		return err
	}
	if err := checkParam("org", options.target.orgName); err != nil {
		return err
	}
	if err := checkParam("bucket", options.target.bucket); err != nil {
		return err
	}

	if options.source.configFile != "" {
		log.Info("upgrading config file", zap.String("file", options.source.configFile))
		if _, err := os.Stat(options.source.configFile); err != nil {
			return err
		}
		v1Config, err := upgradeConfig(options.source.configFile, options.target, log)
		if err != nil {
			return err
		}
		options.source.metaDir = v1Config.Meta.Dir
		options.source.dataDir = v1Config.Data.Dir
		options.source.walDir = v1Config.Data.WALDir
	} else {
		log.Info("no InfluxDB 1.x config file specified, skipping its upgrade")
	}

	if err := options.source.checkDirs(); err != nil {
		return err
	}

	log.Info("source paths", zap.String("meta", options.source.metaDir), zap.String("data", options.source.dataDir))
	log.Info("target paths", zap.String("bolt", options.target.boltPath), zap.String("engine", options.target.enginePath))

	if fi, err := os.Stat(options.target.enginePath); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("engine path '%s' is not directory", options.target.enginePath)
		}
		entries, err := ioutil.ReadDir(options.target.enginePath)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("target engine path '%s' must be empty", options.target.enginePath)
		}
	}

	v1, err := newInfluxDBv1(&options.source)
	if err != nil {
		return err
	}

	v2, err := newInfluxDBv2(ctx, &options.target, log)
	if err != nil {
		return err
	}

	defer func() {
		if err := v2.close(); err != nil {
			log.Error("2.x services closing error", zap.Error(err))
		}
	}()

	or, err := setupAdmin(ctx, v2)
	if err != nil {
		return err
	}
	options.target.token = or.Auth.Token

	db2BucketIds, err := upgradeDatabases(ctx, v1, v2, or.Org.ID, log)
	if err != nil {
		//remove all files
		log.Info("database upgrade error, removing data")
		if e := os.Remove(options.target.boltPath); e != nil {
			log.Error("cleaning failed", zap.Error(e))
		}

		if e := os.RemoveAll(options.target.enginePath); e != nil {
			log.Error("cleaning failed", zap.Error(e))
		}
		return err
	}

	if err = generateSecurityScript(v1, db2BucketIds, log); err != nil {
		return err
	}

	log.Info("upgrade successfully completed. Start service now")

	return nil
}

func newInfluxDBv1(opts *optionsV1) (svc *influxDBv1, err error) {
	svc = &influxDBv1{}
	svc.meta, err = openV1Meta(opts.metaDir)
	if err != nil {
		return nil, fmt.Errorf("error opening 1.x meta.db: %w", err)
	}

	return svc, nil
}

func newInfluxDBv2(ctx context.Context, opts *optionsV2, log *zap.Logger) (svc *influxDBv2, err error) {
	reg := prom.NewRegistry(log.With(zap.String("service", "prom_registry")))

	svc = &influxDBv2{}
	svc.log = log

	// *********************
	// V2 specific services
	serviceConfig := kv.ServiceConfig{}

	// Create BoltDB store and K/V service
	svc.boltClient = bolt.NewClient(log.With(zap.String("service", "bolt")))
	svc.boltClient.Path = opts.boltPath
	if err := svc.boltClient.Open(ctx); err != nil {
		log.Error("Failed opening bolt", zap.Error(err))
		return nil, err
	}

	svc.store = bolt.NewKVStore(log.With(zap.String("service", "kvstore-bolt")), opts.boltPath)
	svc.store.WithDB(svc.boltClient.DB())
	svc.kvStore = svc.store
	svc.kvService = kv.NewService(log.With(zap.String("store", "kv")), svc.store, serviceConfig)

	// ensure migrator is run
	migrator, err := migration.NewMigrator(
		log.With(zap.String("service", "migrations")),
		svc.kvStore,
		all.Migrations[:]...,
	)
	if err != nil {
		log.Error("Failed to initialize kv migrator", zap.Error(err))
		return nil, err
	}

	// apply migrations to metadata store
	if err := migrator.Up(ctx); err != nil {
		log.Error("Failed to apply migrations", zap.Error(err))
		return nil, err
	}

	// other required services
	var (
		authSvc influxdb.AuthorizationService = svc.kvService
	)

	// Create Tenant service (orgs, buckets, )
	svc.tenantStore = tenant.NewStore(svc.kvStore)
	svc.ts = tenant.NewSystem(svc.tenantStore, log.With(zap.String("store", "new")), reg, metric.WithSuffix("new"))

	svc.meta = meta.NewClient(meta.NewConfig(), svc.kvStore)
	if err := svc.meta.Open(); err != nil {
		return nil, err
	}

	// DB/RP service
	svc.dbrpSvc = dbrp.NewService(ctx, svc.ts.BucketService, svc.kvStore)
	svc.bucketSvc = svc.ts.BucketService

	engine := storage.NewEngine(
		opts.enginePath,
		storage.NewConfig(),
		storage.WithMetaClient(svc.meta),
	)

	svc.ts.BucketService = storage.NewBucketService(svc.ts.BucketService, engine)
	// on-boarding service (influx setup)
	svc.onboardSvc = tenant.NewOnboardService(svc.ts, authSvc)

	return svc, nil
}

func openV1Meta(dir string) (*meta.Client, error) {
	cfg := meta.NewConfig()
	cfg.Dir = dir
	store := filestore.New(cfg.Dir, string(meta.BucketName), "meta.db")
	c := meta.NewClient(cfg, store)
	if err := c.Open(); err != nil {
		return nil, err
	}

	return c, nil
}

// influxDirV1 retrieves the influxdb directory.
func influxDirV1() (string, error) {
	var dir string
	// By default, store meta and data files in current users home directory
	u, err := user.Current()
	if err == nil {
		dir = u.HomeDir
	} else if home := os.Getenv("HOME"); home != "" {
		dir = home
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	dir = filepath.Join(dir, ".influxdb")

	return dir, nil
}

// homeOrAnyDir retrieves user's home directory, current working one or just none.
func homeOrAnyDir() string {
	var dir string
	u, err := user.Current()
	if err == nil {
		dir = u.HomeDir
	} else if home := os.Getenv("HOME"); home != "" {
		dir = home
	} else if home := os.Getenv("USERPROFILE"); home != "" {
		dir = home
	} else {
		wd, err := os.Getwd()
		if err != nil {
			dir = ""
		} else {
			dir = wd
		}
	}

	return dir
}
